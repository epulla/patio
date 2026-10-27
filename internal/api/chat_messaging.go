package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
)

type agentMessageRequest struct {
	FromChatID string `json:"from_chat_id"`
	Text       string `json:"text"`
	ReportBack bool   `json:"report_back"`
}

func (s *Server) agentMessage(w http.ResponseWriter, r *http.Request) {
	var in agentMessageRequest
	if decode(r, &in) != nil || !required(in.FromChatID) || !required(in.Text) {
		errw(w, 400, "from_chat_id and text required")
		return
	}
	id := idParam(r, "id")
	lock := s.chats.queueLock(id)
	lock.Lock()
	defer lock.Unlock()
	var hops int
	if err := s.db.QueryRow(`SELECT agent_hops FROM chat WHERE id=?`, id).Scan(&hops); err != nil {
		errw(w, 404, "chat not found")
		return
	}
	var senderExists int
	if err := s.db.QueryRow(`SELECT 1 FROM chat WHERE id=?`, in.FromChatID).Scan(&senderExists); err != nil {
		errw(w, 400, "sender chat not found")
		return
	}
	if hops >= 10 {
		if err := s.queueAgentMessage(id, in); err != nil {
			log.Printf("queue held agent message for %s: %v", id, err)
			errw(w, 500, err.Error())
			return
		}
		if err := s.notifyAgentHopCap(id); err != nil {
			log.Printf("notify agent hop cap for %s: %v", id, err)
		}
		write(w, 200, map[string]string{"status": "held"})
		return
	}
	if err := s.queueAgentMessage(id, in); err != nil {
		log.Printf("queue agent message for %s: %v", id, err)
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, map[string]string{"status": "queued"})
}

func (s *Server) notifyAgentHopCap(chatID string) error {
	result, err := s.db.Exec(`UPDATE chat SET agent_hops_notified=1 WHERE id=? AND agent_hops>=10 AND agent_hops_notified=0 AND EXISTS(SELECT 1 FROM chat_queue WHERE chat_id=? AND from_chat_id IS NOT NULL)`, chatID, chatID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return nil
	}
	var title string
	if err := s.db.QueryRow(`SELECT title FROM chat WHERE id=?`, chatID).Scan(&title); err != nil {
		return err
	}
	return s.Notify("chat", "Chat paused: too many agent messages", title+" received 10 agent messages in a row. Open it to continue.", "/ai/chats/"+chatID, true)
}

func reportTarget(report bool, id string) any {
	if report {
		return id
	}
	return nil
}
func agentMessageLabel(fromID, title string, reportBack bool) string {
	label := `[Message from chat_id ` + fromID + `, title ` + strconv.Quote(title) + `. Reply with send_to_chat if needed.]`
	if reportBack {
		label = `[Message from chat_id ` + fromID + `, title ` + strconv.Quote(title) + `. Final reply will be sent back automatically; do not call send_to_chat to answer.]`
	}
	return label + "\n\n"
}

func (s *Server) queueAgentMessage(id string, in agentMessageRequest) error {
	if _, err := s.db.Exec(`INSERT INTO chat_queue(id,chat_id,text,attachments,created_at,from_chat_id,report_to_chat_id) VALUES(?,?,?,'[]',?,?,?)`, uid(), id, in.Text, now(), in.FromChatID, reportTarget(in.ReportBack, in.FromChatID)); err != nil {
		return err
	}
	s.chats.emit(id, map[string]any{"type": "queue"})
	return nil
}

func (s *Server) reportChatResult(target, from, text string) error {
	data, err := json.Marshal(agentMessageRequest{FromChatID: from, Text: text})
	if err != nil {
		return err
	}
	req := httptest.NewRequest(http.MethodPost, "/api/chats/"+target+"/agent-messages", bytes.NewReader(data))
	req.SetPathValue("id", target)
	rec := httptest.NewRecorder()
	s.agentMessage(rec, req)
	if rec.Code < 200 || rec.Code >= 300 {
		deliveryErr := errors.New(strings.TrimSpace(rec.Body.String()))
		title := target
		if err := s.db.QueryRow(`SELECT title FROM chat WHERE id=?`, target).Scan(&title); err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.Printf("lookup report target %s title: %v", target, err)
		}
		if err := s.Notify("chat", "Couldn't report back to "+title, deliveryErr.Error(), "/ai/chats/"+from, true); err != nil {
			log.Printf("notify report-back failure to %s: %v", target, err)
		}
		return deliveryErr
	}
	if s.chatWindow != "" || s.userID != "local" {
		return nil
	}
	if err := s.DeliverAgentMessages(context.Background()); err != nil {
		return err
	}
	return nil
}

func (s *Server) createChatHandoff(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ConnectorID string `json:"connector_id"`
		Model       string `json:"model"`
		Bot         string `json:"bot"`
		Prompt      string `json:"prompt"`
		ReportBack  bool   `json:"report_back"`
	}
	if decode(r, &in) != nil || !required(in.Prompt) {
		errw(w, 400, "prompt required")
		return
	}
	caller, err := s.scanChat(s.db.QueryRow(`SELECT `+chatColumns+` FROM chat WHERE id=?`, idParam(r, "id")))
	if err != nil {
		errw(w, 404, "caller chat not found")
		return
	}
	defaults, err := s.chatDefaults()
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	var selectedBot bot
	if in.Bot != "" {
		selectedBot, err = scanBot(s.db.QueryRow(`SELECT `+botColumns+` FROM "user" WHERE name=? COLLATE NOCASE AND is_ai=1 AND deleted_at IS NULL`, in.Bot))
		if errors.Is(err, sql.ErrNoRows) {
			rows, queryErr := s.db.Query(`SELECT name FROM "user" WHERE is_ai=1 AND deleted_at IS NULL ORDER BY name COLLATE NOCASE,rowid`)
			if queryErr != nil {
				errw(w, 500, queryErr.Error())
				return
			}
			var names []string
			for rows.Next() {
				var name string
				if queryErr = rows.Scan(&name); queryErr != nil {
					break
				}
				names = append(names, name)
			}
			queryErr = errors.Join(queryErr, rows.Err(), rows.Close())
			if queryErr != nil {
				errw(w, 500, queryErr.Error())
				return
			}
			errw(w, 400, `unknown bot `+strconv.Quote(in.Bot)+`; available bots: `+strings.Join(names, ", "))
			return
		}
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
	}
	connectorID := in.ConnectorID
	if connectorID == "" && selectedBot.ConnectorID != nil {
		connectorID = *selectedBot.ConnectorID
	}
	if connectorID == "" && caller.ConnectorID != nil {
		connectorID = *caller.ConnectorID
	}
	if connectorID == "" && defaults.ConnectorID != nil {
		connectorID = *defaults.ConnectorID
	}
	if connectorID == "" {
		errw(w, 400, "connector_id required")
		return
	}
	model := in.Model
	if model == "" && selectedBot.ConnectorID != nil && connectorID == *selectedBot.ConnectorID {
		model = selectedBot.Model
	}
	if model == "" && caller.ConnectorID != nil && connectorID == *caller.ConnectorID {
		model = caller.Model
	}
	if model == "" && defaults.ConnectorID != nil && connectorID == *defaults.ConnectorID {
		model = defaults.Model
	}
	depth := 0
	parent := caller.ID
	for parent != "" {
		var p sql.NullString
		if err := s.db.QueryRow(`SELECT parent_chat_id FROM chat WHERE id=?`, parent).Scan(&p); err != nil {
			break
		}
		if !p.Valid {
			break
		}
		depth++
		parent = p.String
	}
	if depth >= 5 {
		errw(w, 400, "chat handoff depth limit reached")
		return
	}
	title := strings.TrimSpace(in.Prompt)
	if len(title) > 60 {
		title = title[:60]
	}
	if title == "" {
		title = "Handoff from " + caller.Title
	}
	id := uid()
	botID := caller.BotID
	if in.Bot != "" {
		botID = &selectedBot.ID
	}
	if botID == nil && defaults.DefaultBotID != "none" {
		botID = &defaults.DefaultBotID
	}
	folder := caller.Folder
	created := now()
	child := chat{ID: id, Folder: folder}
	createdWorkdir := false
	if folder == nil {
		dir := s.chatWorkdir(child)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			errw(w, 500, err.Error())
			return
		}
		createdWorkdir = true
		if err := os.Chmod(dir, 0o700); err != nil {
			errw(w, 500, cleanupConnectorDir(dir, err).Error())
			return
		}
	}
	if _, err := s.db.Exec(`INSERT INTO chat(id,connector_id,title,model,folder,created_at,updated_at,thinking,show_thinking,auto_allow,parent_chat_id,bot_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, connectorID, title, model, folder, created, created, caller.Thinking, caller.ShowThinking, caller.AutoAllow, caller.ID, botID); err != nil {
		if createdWorkdir {
			err = cleanupConnectorDir(s.chatWorkdir(child), err)
		}
		errw(w, 500, err.Error())
		return
	}
	var outcome struct {
		Status string `json:"status"`
	}
	body := agentMessageRequest{FromChatID: caller.ID, Text: in.Prompt, ReportBack: in.ReportBack}
	raw, _ := json.Marshal(body)
	req := r.Clone(r.Context())
	req.SetPathValue("id", id)
	req.Body = io.NopCloser(bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	s.agentMessage(rec, req)
	if rec.Code >= 400 {
		cause := errors.New(strings.TrimSpace(rec.Body.String()))
		_, deleteErr := s.db.Exec(`DELETE FROM chat WHERE id=?`, id)
		if deleteErr != nil {
			cause = errors.Join(cause, deleteErr)
		}
		if createdWorkdir {
			cause = cleanupConnectorDir(s.chatWorkdir(child), cause)
		}
		errw(w, rec.Code, cause.Error())
		return
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &outcome)
	write(w, 200, map[string]string{"chat_id": id, "status": outcome.Status})
}
