package api

import (
	"database/sql"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var mentionChatID = regexp.MustCompile(`\]\(/ai/chats/([^\)\s]+)\)`)

func (s *Server) ticketBotNames() []string {
	rows, err := s.db.Query(`SELECT name FROM "user" WHERE is_ai=1 AND deleted_at IS NULL ORDER BY name COLLATE NOCASE,rowid`)
	if err != nil {
		log.Printf("load ticket mention bots: %v", err)
		return nil
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) != nil {
			return nil
		}
		names = append(names, name)
	}
	return names
}

func (s *Server) openTicketMentionChats(ticketID, source string, mentions []mention) []string {
	ids := make([]string, len(mentions))
	if len(mentions) == 0 {
		return ids
	}
	var isAI bool
	if err := s.db.QueryRow(`SELECT is_ai FROM "user" WHERE id=?`, s.userID).Scan(&isAI); err != nil || isAI {
		return ids
	}
	var code, title string
	if err := s.db.QueryRow(`SELECT code,title FROM ticket WHERE id=?`, ticketID).Scan(&code, &title); err != nil {
		log.Printf("load ticket for mentions %s: %v", ticketID, err)
		return ids
	}
	defaults, err := s.chatDefaults()
	if err != nil {
		log.Printf("load chat defaults for ticket mentions: %v", err)
		return ids
	}
	for i, m := range mentions {
		b, err := scanBot(s.db.QueryRow(`SELECT `+botColumns+` FROM "user" WHERE name=? COLLATE NOCASE AND is_ai=1 AND deleted_at IS NULL`, m.Bot))
		if err != nil {
			log.Printf("load mentioned bot %q: %v", m.Bot, err)
			continue
		}
		connector := b.ConnectorID
		model := b.Model
		if connector == nil || *connector == "" {
			connector = defaults.ConnectorID
			model = defaults.Model
		}
		if connector == nil || *connector == "" {
			log.Printf("mentioned bot %q has no connector", m.Bot)
			continue
		}
		kind, dir, err := s.chats.connectorConfig(*connector)
		if err != nil {
			log.Printf("resolve connector for bot %q: %v", m.Bot, err)
			continue
		}
		id := uid()
		chatTitle := m.Text
		if len([]rune(chatTitle)) > 60 {
			chatTitle = string([]rune(chatTitle)[:60])
		}
		c := chat{ID: id, Title: chatTitle, ConnectorID: connector, Model: model, BotID: &b.ID}
		workdir := s.chatWorkdir(c)
		if err = os.MkdirAll(workdir, 0o700); err == nil {
			err = os.Chmod(workdir, 0o700)
		}
		if err != nil {
			log.Printf("create ticket mention workdir: %v", err)
			continue
		}
		created := now()
		_, err = s.db.Exec(`INSERT INTO chat(id,connector_id,title,model,folder,created_at,updated_at,thinking,show_thinking,auto_allow,parent_chat_id,bot_id) VALUES(?,?,?,?,NULL,?,?,?,?,?,NULL,?)`, id, *connector, chatTitle, model, created, created, defaults.Thinking, defaults.ShowThinking, defaults.AutoAllow, b.ID)
		if err != nil {
			log.Printf("create ticket mention chat: %v", cleanupConnectorDir(workdir, err))
			continue
		}
		text := "[Ticket " + code + ": " + title + "](/tickets/" + ticketID + ") — @" + b.Name + " was mentioned in the ticket " + source + ".\n\nTask: " + m.Text + "\n\nWhen done, post your result as a comment on the ticket with add_comment (ticket_id " + ticketID + ", chat_id " + id + ")."
		c.Folder = nil
		if _, err = s.chats.Send(c, kind, dir, ChatSendRequest{Text: text}); err != nil {
			log.Printf("send ticket mention to %q: %v", b.Name, cleanupConnectorDir(workdir, err))
			_, _ = s.db.Exec(`DELETE FROM chat WHERE id=?`, id)
		} else {
			ids[i] = id
		}
	}
	return ids
}

func (s *Server) linkTicketMentionChats(ticketID, expected string, mentions []mention, ids []string) string {
	linked := expected
	if len(mentions) == 0 {
		return linked
	}
	err := txrun(s.db, func(tx *sql.Tx) error {
		var current string
		if err := tx.QueryRow(`SELECT description FROM ticket WHERE id=?`, ticketID).Scan(&current); err != nil {
			return err
		}
		if current != expected {
			log.Printf("skip linking ticket mention chats for %s: description changed", ticketID)
			return nil
		}
		runes := []rune(current)
		for i := len(mentions) - 1; i >= 0; i-- {
			if i >= len(ids) || ids[i] == "" {
				continue
			}
			m := mentions[i]
			var userID string
			if err := tx.QueryRow(`SELECT id FROM "user" WHERE name=? COLLATE NOCASE AND is_ai=1 AND deleted_at IS NULL`, m.Bot).Scan(&userID); err != nil {
				return err
			}
			insert := " ([chat](/ai/chats/" + ids[i] + "))"
			if m.group != m.name {
				segment := string(runes[m.name:m.group])
				targets := mentionChatID.FindAllStringSubmatch(segment, -1)
				links := make([]string, 0, len(targets)+1)
				for _, target := range targets {
					links = append(links, target[1])
				}
				links = append(links, ids[i])
				var items []string
				for n, target := range links {
					label := "chat"
					if len(links) > 1 {
						label += " " + strconv.Itoa(n+1)
					}
					items = append(items, "["+label+"](/ai/chats/"+target+")")
				}
				insert = " (" + strings.Join(items, ", ") + ")"
			}
			runes = append(runes[:m.name], append([]rune(insert), runes[m.group:]...)...)
			if _, err := tx.Exec(`INSERT INTO ticket_history(id,content,created_at,ticket_id,user_id) VALUES(?,?,?,?,?)`, uid(), "Linked @"+m.Bot+" chat", now(), ticketID, userID); err != nil {
				return err
			}
		}
		if string(runes) == current {
			return nil
		}
		linked = string(runes)
		_, err := tx.Exec(`UPDATE ticket SET description=?,updated_at=? WHERE id=?`, linked, now(), ticketID)
		return err
	})
	if err != nil {
		log.Printf("link ticket mention chats %s: %v", ticketID, err)
		return expected
	}
	return linked
}

func (s *Server) addTriggeredMentionComments(ticketID string, mentions []mention, ids []string) {
	for i, m := range mentions {
		if i >= len(ids) || ids[i] == "" {
			continue
		}
		var botID string
		if err := s.db.QueryRow(`SELECT id FROM "user" WHERE name=? COLLATE NOCASE AND is_ai=1 AND deleted_at IS NULL`, m.Bot).Scan(&botID); err != nil {
			log.Printf("lookup triggered mention bot %q: %v", m.Bot, err)
			continue
		}
		text := "Triggered by @" + m.Bot + " in the previous comment → [chat](/ai/chats/" + ids[i] + ")"
		if _, err := s.db.Exec(`INSERT INTO ticket_comment(id,content,created_at,updated_at,ticket_id,user_id) VALUES(?,?,?,?,?,?)`, uid(), text, now(), now(), ticketID, botID); err != nil {
			log.Printf("insert triggered mention comment: %v", err)
		}
	}
}
