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
	"reflect"
	"slices"
)

// historyItem describes a tracked item type: how to load its public (secret-free) view, and which
// request fields carry secrets, which are recorded by name only.
type historyItem struct {
	load    func(s *Server, id string) (any, string, error)
	secrets []string
}

var historyItems = map[string]historyItem{
	"skill": {load: func(s *Server, id string) (any, string, error) {
		skill, err := s.loadSkill(id)
		return skill, skill.Name, err
	}},
	"connector": {secrets: []string{"secret"}, load: func(s *Server, id string) (any, string, error) {
		c, err := s.readConnector(s.db.QueryRow(`SELECT id,kind,name,config_dir,base_url,model,quota_json,created_at FROM connector WHERE id=?`, id))
		return c, c.Name, err
	}},
	"mcp_server": {secrets: []string{"env", "bearer_token", "oauth_client_secret"}, load: func(s *Server, id string) (any, string, error) {
		server, err := readMCPServer(s.db.QueryRow(mcpServerSelect+` WHERE id=?`, id))
		return server, server.Name, err
	}},
	"scheduled_task": {load: func(s *Server, id string) (any, string, error) {
		task, err := scanScheduledTask(s.db.QueryRow(`SELECT `+scheduledTaskColumns+` FROM scheduled_task WHERE id=?`, id))
		return task, task.Title, err
	}},
}

// Bookkeeping fields that change without a user edit.
var historyIgnored = map[string]bool{"id": true, "created_at": true, "updated_at": true, "next_run_at": true, "quota_json": true, "has_oauth_token": true}

type historyChange struct {
	Field  string `json:"field"`
	Old    any    `json:"old,omitempty"`
	New    any    `json:"new,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
}

type itemHistory struct {
	ID       string          `json:"id"`
	ItemType string          `json:"item_type"`
	ItemID   string          `json:"item_id"`
	ItemName string          `json:"item_name"`
	Action   string          `json:"action"`
	Changes  []historyChange `json:"changes"`
	Snapshot json.RawMessage `json:"snapshot"`
	UserID   string          `json:"user_id"`
	UserName string          `json:"user_name"`
	UserIsAI bool            `json:"user_is_ai"`
	Created  string          `json:"created_at"`
}

type historyActionKey struct{}

type historyRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *historyRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *historyRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	r.body.Write(data)
	return r.ResponseWriter.Write(data)
}

// tracked records an item_history entry after a successful create, update, or delete.
func (s *Server) tracked(itemType string, next http.HandlerFunc) http.HandlerFunc {
	item := historyItems[itemType]
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var before map[string]any
		name := ""
		if id != "" {
			if value, loadedName, err := item.load(s, id); err == nil {
				before, name = historyMap(value), loadedName
			}
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			errw(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		rec := &historyRecorder{ResponseWriter: w}
		next(rec, r)
		if rec.status < 200 || rec.status >= 300 {
			return
		}
		action := map[string]string{http.MethodPost: "created", http.MethodPut: "updated", http.MethodDelete: "deleted"}[r.Method]
		if override, ok := r.Context().Value(historyActionKey{}).(string); ok {
			action = override
		}
		if id == "" {
			var created struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(rec.body.Bytes(), &created) != nil || created.ID == "" {
				return
			}
			id = created.ID
		}
		var after map[string]any
		if action != "deleted" {
			value, loadedName, err := item.load(s, id)
			if err != nil {
				log.Printf("record %s history %s: %v", itemType, id, err)
				return
			}
			after, name = historyMap(value), loadedName
		} else if before == nil {
			return
		}
		changes := historyDiff(before, after)
		var input map[string]json.RawMessage
		_ = json.Unmarshal(raw, &input)
		for _, field := range item.secrets {
			if value, ok := input[field]; ok && string(value) != "null" {
				changes = append(changes, historyChange{Field: field, Hidden: true})
			}
		}
		if (action == "updated" || action == "restored") && len(changes) == 0 {
			return
		}
		snapshot := after
		if action == "deleted" {
			snapshot = before
		}
		if err := s.recordHistory(itemType, id, name, action, changes, snapshot); err != nil {
			log.Printf("record %s history %s: %v", itemType, id, err)
		}
	}
}

func (s *Server) recordHistory(itemType, id, name, action string, changes []historyChange, snapshot map[string]any) error {
	changesJSON, err := json.Marshal(changes)
	if err != nil {
		return err
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO item_history(id,item_type,item_id,item_name,action,changes_json,snapshot_json,user_id,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		uid(), itemType, id, name, action, string(changesJSON), string(snapshotJSON), s.userID, now())
	return err
}

func historyMap(value any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out map[string]any
	if json.Unmarshal(data, &out) != nil {
		return nil
	}
	for field := range historyIgnored {
		delete(out, field)
	}
	return out
}

// historyDiff lists fields that differ, sorted by name. A create diffs against nothing; a delete records no changes.
func historyDiff(before, after map[string]any) []historyChange {
	changes := []historyChange{}
	if after == nil {
		return changes
	}
	fields := map[string]bool{}
	for field := range before {
		fields[field] = true
	}
	for field := range after {
		fields[field] = true
	}
	ordered := make([]string, 0, len(fields))
	for field := range fields {
		ordered = append(ordered, field)
	}
	slices.Sort(ordered)
	for _, field := range ordered {
		if !reflect.DeepEqual(before[field], after[field]) {
			changes = append(changes, historyChange{Field: field, Old: before[field], New: after[field]})
		}
	}
	return changes
}

func (s *Server) listItemHistory(w http.ResponseWriter, r *http.Request) {
	itemType, itemID := r.URL.Query().Get("item_type"), r.URL.Query().Get("item_id")
	if _, ok := historyItems[itemType]; !ok || itemID == "" {
		errw(w, http.StatusBadRequest, "item_type must be skill, connector, mcp_server or scheduled_task, and item_id is required")
		return
	}
	rows, err := s.db.Query(`SELECT h.id,h.item_type,h.item_id,h.item_name,h.action,h.changes_json,h.snapshot_json,h.user_id,u.name,u.is_ai,h.created_at FROM item_history h JOIN "user" u ON u.id=h.user_id WHERE h.item_type=? AND h.item_id=? ORDER BY h.created_at DESC,h.rowid DESC LIMIT 200`, itemType, itemID)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []itemHistory{}
	for rows.Next() {
		var entry itemHistory
		var changes, snapshot string
		if err := rows.Scan(&entry.ID, &entry.ItemType, &entry.ItemID, &entry.ItemName, &entry.Action, &changes, &snapshot, &entry.UserID, &entry.UserName, &entry.UserIsAI, &entry.Created); err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := json.Unmarshal([]byte(changes), &entry.Changes); err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		entry.Snapshot = json.RawMessage(snapshot)
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, out)
}

// restoreItemHistory puts a skill or memory's description and body back to a past version, as a new tracked update.
func (s *Server) restoreItemHistory(w http.ResponseWriter, r *http.Request) {
	var itemType, itemID, snapshot string
	err := s.db.QueryRow(`SELECT item_type,item_id,snapshot_json FROM item_history WHERE id=?`, idParam(r, "id")).Scan(&itemType, &itemID, &snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "history entry not found")
		return
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if itemType != "skill" {
		errw(w, http.StatusBadRequest, "only skills and memories can be restored")
		return
	}
	var version struct {
		Description *string `json:"description"`
		Body        *string `json:"body"`
	}
	if json.Unmarshal([]byte(snapshot), &version) != nil || version.Description == nil || version.Body == nil {
		errw(w, http.StatusBadRequest, "history entry has no skill version")
		return
	}
	if _, err := s.loadSkill(itemID); errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "skill was deleted")
		return
	}
	body, err := json.Marshal(map[string]string{"description": *version.Description, "body": *version.Body})
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	update, err := http.NewRequestWithContext(context.WithValue(r.Context(), historyActionKey{}, "restored"), http.MethodPut, "/api/skills/"+itemID, bytes.NewReader(body))
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	update.Header.Set("Content-Type", "application/json")
	s.ServeHTTP(w, update)
}
