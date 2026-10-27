package api

import (
	"log"
	"os"
)

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

func (s *Server) openTicketMentionChats(ticketID, source string, mentions []mention) {
	if len(mentions) == 0 {
		return
	}
	var isAI bool
	if err := s.db.QueryRow(`SELECT is_ai FROM "user" WHERE id=?`, s.userID).Scan(&isAI); err != nil || isAI {
		return
	}
	var code, title string
	if err := s.db.QueryRow(`SELECT code,title FROM ticket WHERE id=?`, ticketID).Scan(&code, &title); err != nil {
		log.Printf("load ticket for mentions %s: %v", ticketID, err)
		return
	}
	defaults, err := s.chatDefaults()
	if err != nil {
		log.Printf("load chat defaults for ticket mentions: %v", err)
		return
	}
	for _, m := range mentions {
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
		}
	}
}
