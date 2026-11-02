package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode"
)

type bot struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	PersonaPrompt string  `json:"persona_prompt"`
	Color         string  `json:"color"`
	ConnectorID   *string `json:"connector_id"`
	Model         string  `json:"model"`
}

const botColumns = `id,name,description,persona_prompt,color,connector_id,model`

func scanBot(row interface{ Scan(...any) error }) (bot, error) {
	var b bot
	err := row.Scan(&b.ID, &b.Name, &b.Description, &b.PersonaPrompt, &b.Color, &b.ConnectorID, &b.Model)
	return b, err
}
func (s *Server) listBots(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query(`SELECT ` + botColumns + ` FROM "user" WHERE is_ai=1 AND deleted_at IS NULL ORDER BY name COLLATE NOCASE,rowid`)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	defer rows.Close()
	out := []bot{}
	for rows.Next() {
		b, e := scanBot(rows)
		if e != nil {
			errw(w, 500, e.Error())
			return
		}
		out = append(out, b)
	}
	if err = rows.Err(); err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, out)
}
func (s *Server) saveBot(w http.ResponseWriter, r *http.Request, create bool) {
	var fields struct {
		Name          *string         `json:"name"`
		Description   *string         `json:"description"`
		PersonaPrompt *string         `json:"persona_prompt"`
		Color         *string         `json:"color"`
		ConnectorID   json.RawMessage `json:"connector_id"`
		Model         *string         `json:"model"`
	}
	if decode(r, &fields) != nil {
		errw(w, 400, "invalid bot JSON")
		return
	}
	in := bot{ConnectorID: nil}
	if !create {
		var err error
		in, err = scanBot(s.db.QueryRow(`SELECT `+botColumns+` FROM "user" WHERE id=? AND is_ai=1 AND deleted_at IS NULL`, idParam(r, "id")))
		if errors.Is(err, sql.ErrNoRows) {
			errw(w, 404, "bot not found")
			return
		}
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
	}
	if fields.Name != nil {
		in.Name = *fields.Name
	}
	if fields.Description != nil {
		in.Description = *fields.Description
	}
	if fields.PersonaPrompt != nil {
		in.PersonaPrompt = *fields.PersonaPrompt
	}
	if fields.Color != nil {
		in.Color = *fields.Color
	}
	if fields.Model != nil {
		in.Model = *fields.Model
	}
	if fields.ConnectorID != nil {
		if string(fields.ConnectorID) == "null" {
			in.ConnectorID = nil
		} else {
			var id string
			if json.Unmarshal(fields.ConnectorID, &id) != nil || !required(id) {
				errw(w, 400, "connector_id must be a connector UUID or null")
				return
			}
			in.ConnectorID = &id
		}
	}
	// Same characters parseMentions matches, so every bot can be @mentioned.
	if !required(in.Name) || strings.IndexFunc(in.Name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' }) >= 0 {
		errw(w, 400, "name required; use only letters, digits and _")
		return
	}
	if in.ConnectorID == nil && create {
		d, e := s.chatDefaults()
		if e != nil {
			errw(w, 500, e.Error())
			return
		}
		in.ConnectorID = d.ConnectorID
		if in.Model == "" {
			in.Model = d.Model
		}
	}
	var exists int
	if in.ConnectorID == nil && create {
		errw(w, 400, "connector_id required")
		return
	}
	if in.ConnectorID != nil {
		if e := s.db.QueryRow(`SELECT 1 FROM connector WHERE id=?`, *in.ConnectorID).Scan(&exists); errors.Is(e, sql.ErrNoRows) {
			errw(w, 400, "connector not found")
			return
		} else if e != nil {
			errw(w, 500, e.Error())
			return
		}
	}
	id := idParam(r, "id")
	var err error
	if create {
		id = uid()
		_, err = s.db.Exec(`INSERT INTO "user"(id,name,email,email_verified,is_ai,description,persona_prompt,color,connector_id,model) VALUES(?,?,?,1,1,?,?,?,?,?)`, id, in.Name, id+"@bot.local", in.Description, in.PersonaPrompt, in.Color, in.ConnectorID, in.Model)
	} else {
		var res sql.Result
		res, err = s.db.Exec(`UPDATE "user" SET name=?,description=?,persona_prompt=?,color=?,connector_id=?,model=?,updated_at=? WHERE id=? AND is_ai=1 AND deleted_at IS NULL`, in.Name, in.Description, in.PersonaPrompt, in.Color, in.ConnectorID, in.Model, now(), id)
		if err == nil {
			n, _ := res.RowsAffected()
			if n == 0 {
				errw(w, 404, "bot not found")
				return
			}
		}
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			errw(w, 400, "bot name already exists")
		} else {
			errw(w, 500, err.Error())
		}
		return
	}
	b, e := scanBot(s.db.QueryRow(`SELECT `+botColumns+` FROM "user" WHERE id=?`, id))
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	code := 200
	if create {
		code = 201
	}
	write(w, code, b)
}
func (s *Server) createBot(w http.ResponseWriter, r *http.Request) { s.saveBot(w, r, true) }
func (s *Server) updateBot(w http.ResponseWriter, r *http.Request) { s.saveBot(w, r, false) }
func (s *Server) deleteBot(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	if id == "ai" {
		errw(w, 400, "Martin cannot be deleted")
		return
	}
	tx, e := s.db.Begin()
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	defer tx.Rollback()
	rows, e := tx.Query(`SELECT s.id FROM skill s JOIN skill_bot sb ON sb.skill_id=s.id WHERE sb.bot_id=? AND (SELECT count(*) FROM skill_bot x WHERE x.skill_id=s.id)=1`, id)
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	var only []string
	for rows.Next() {
		var skillID string
		if e = rows.Scan(&skillID); e != nil {
			rows.Close()
			errw(w, 500, e.Error())
			return
		}
		only = append(only, skillID)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		errw(w, 500, e.Error())
		return
	}
	rows.Close()
	for _, skillID := range only {
		sk, err := s.loadSkill(skillID)
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
		snapshot, err := json.Marshal(historyMap(sk))
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
		changes, err := json.Marshal([]historyChange{})
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
		if _, err = tx.Exec(`INSERT INTO item_history(id,item_type,item_id,item_name,action,changes_json,snapshot_json,user_id,created_at) VALUES(?, 'skill', ?, ?, 'deleted', ?, ?, ?, ?)`, uid(), sk.ID, sk.Name, string(changes), string(snapshot), s.userID, now()); err != nil {
			errw(w, 500, err.Error())
			return
		}
		if _, err = tx.Exec(`DELETE FROM skill WHERE id=?`, skillID); err != nil {
			errw(w, 500, err.Error())
			return
		}
	}
	if _, e = tx.Exec(`DELETE FROM skill_bot WHERE bot_id=?`, id); e != nil {
		errw(w, 500, e.Error())
		return
	}
	res, e := tx.Exec(`UPDATE "user" SET deleted_at=?,updated_at=? WHERE id=? AND is_ai=1 AND deleted_at IS NULL`, now(), now(), id)
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	n, e := res.RowsAffected()
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	if n == 0 {
		errw(w, 404, "bot not found")
		return
	}
	if e = tx.Commit(); e != nil {
		errw(w, 500, e.Error())
		return
	}
	noContent(w)
}
