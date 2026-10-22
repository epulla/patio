package api

import (
	"bytes"
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
	var title, senderTitle string
	var hops int
	if err := s.db.QueryRow(`SELECT title,agent_hops FROM chat WHERE id=?`, id).Scan(&title, &hops); err != nil {
		errw(w, 404, "chat not found")
		return
	}
	if err := s.db.QueryRow(`SELECT title FROM chat WHERE id=?`, in.FromChatID).Scan(&senderTitle); err != nil {
		errw(w, 400, "sender chat not found")
		return
	}
	prefix := `[Message from chat_id ` + in.FromChatID + `, title ` + strconv.Quote(senderTitle) + `. Reply with send_to_chat if needed.]` + "\n\n"
	if hops >= 10 {
		if err := s.queueAgentMessage(id, in, prefix); err != nil {
			errw(w, 500, err.Error())
			return
		}
		if err := s.Notify("chat", "Chat paused: too many agent messages", title+" received 10 agent messages in a row. Open it to continue.", "/ai/chats/"+id, true); err != nil { /* notification is best effort */
		}
		write(w, 200, map[string]string{"status": "held"})
		return
	}
	c, err := s.scanChat(s.db.QueryRow(`SELECT `+chatColumns+` FROM chat WHERE id=?`, id))
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	if c.ConnectorID == nil {
		errw(w, 400, errChatConnectorMissing.Error())
		return
	}
	kind, dir, err := s.chats.connectorConfig(*c.ConnectorID)
	if err != nil {
		errw(w, 400, err.Error())
		return
	}
	fromID := in.FromChatID
	var reportTo *string
	if in.ReportBack {
		reportTo = &fromID
	}
	_, err = s.chats.Send(c, kind, dir, ChatSendRequest{Text: prefix + in.Text, FromChatID: &fromID, ReportToChatID: reportTo})
	if errors.Is(err, errChatTurnRunning) || errors.Is(err, errChatTurnOtherWindow) {
		if err := s.queueAgentMessage(id, in, prefix); err != nil {
			errw(w, 500, err.Error())
			return
		}
		write(w, 200, map[string]string{"status": "queued"})
		return
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	_, err = s.db.Exec(`UPDATE chat SET agent_hops=agent_hops+1 WHERE id=?`, id)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, map[string]string{"status": "sent"})
}

func reportTarget(report bool, id string) any {
	if report {
		return id
	}
	return nil
}
func (s *Server) queueAgentMessage(id string, in agentMessageRequest, prefix string) error {
	if _, err := s.db.Exec(`INSERT INTO chat_queue(id,chat_id,text,attachments,created_at,from_chat_id,report_to_chat_id) VALUES(?,?,?,'[]',?,?,?)`, uid(), id, prefix+in.Text, now(), in.FromChatID, reportTarget(in.ReportBack, in.FromChatID)); err != nil {
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
	return nil
}

func (s *Server) createChatHandoff(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ConnectorID string `json:"connector_id"`
		Model       string `json:"model"`
		Prompt      string `json:"prompt"`
		ReportBack  bool   `json:"report_back"`
	}
	if decode(r, &in) != nil || !required(in.ConnectorID) || !required(in.Prompt) {
		errw(w, 400, "connector_id and prompt required")
		return
	}
	caller, err := s.scanChat(s.db.QueryRow(`SELECT `+chatColumns+` FROM chat WHERE id=?`, idParam(r, "id")))
	if err != nil {
		errw(w, 404, "caller chat not found")
		return
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
	if _, err := s.db.Exec(`INSERT INTO chat(id,connector_id,title,model,folder,created_at,updated_at,thinking,show_thinking,auto_allow,parent_chat_id) VALUES(?,?,?,?,?,?,?,0,0,0,?)`, id, in.ConnectorID, title, in.Model, folder, created, created, caller.ID); err != nil {
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
