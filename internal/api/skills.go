package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
)

const maxSkillBody = 64 * 1024

type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Body        string   `json:"body"`
	Enabled     bool     `json:"enabled"`
	Kind        string   `json:"kind"`
	BotIDs      []string `json:"bot_ids"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

type skillInput struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Body        *string   `json:"body"`
	Enabled     *bool     `json:"enabled"`
	Kind        *string   `json:"kind"`
	BotIDs      *[]string `json:"bot_ids"`
}

type skillConfig struct {
	Name        string
	Description string
	Body        string
	Enabled     bool
	Kind        string
}

func validateSkill(config skillConfig) error {
	if len(config.Name) < 1 || len(config.Name) > 64 {
		return errors.New("name must be 1 to 64 characters and use lowercase letters, digits, and hyphens")
	}
	segment := false
	for i, char := range config.Name {
		valid := char >= 'a' && char <= 'z' || char >= '0' && char <= '9'
		if valid {
			segment = true
			continue
		}
		if char != '-' || !segment || i == len(config.Name)-1 {
			return errors.New("name must match [a-z0-9]+(-[a-z0-9]+)* and be at most 64 characters")
		}
		segment = false
	}
	if !segment {
		return errors.New("name must match [a-z0-9]+(-[a-z0-9]+)* and be at most 64 characters")
	}
	if n := utf8.RuneCountInString(config.Description); n < 1 || n > 1024 || strings.TrimSpace(config.Description) == "" {
		return errors.New("description must be 1 to 1024 characters")
	}
	if len(config.Body) > maxSkillBody {
		return errors.New("body must be at most 64 KB")
	}
	if config.Kind != "skill" && config.Kind != "memory" {
		return errors.New("kind must be skill or memory")
	}
	return nil
}

const skillSelect = `SELECT id,name,description,body,enabled,kind,created_at,updated_at FROM skill`

func scanSkill(row interface{ Scan(...any) error }) (Skill, error) {
	var skill Skill
	var enabled int
	err := row.Scan(&skill.ID, &skill.Name, &skill.Description, &skill.Body, &enabled, &skill.Kind, &skill.CreatedAt, &skill.UpdatedAt)
	skill.Enabled = enabled != 0
	return skill, err
}

func SkillByName(db *sql.DB, name string) (Skill, error) {
	skill, err := scanSkill(db.QueryRow(skillSelect+` WHERE name=?`, name))
	if err == nil {
		err = loadSkillBotIDs(db, &skill)
	}
	return skill, err
}

func loadSkillBotIDs(db *sql.DB, skill *Skill) error {
	skill.BotIDs = []string{}
	rows, err := db.Query(`SELECT bot_id FROM skill_bot WHERE skill_id=? ORDER BY bot_id`, skill.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		skill.BotIDs = append(skill.BotIDs, id)
	}
	return rows.Err()
}

func SkillsForBot(db *sql.DB, botID string) ([]Skill, error) {
	q := skillSelect + ` WHERE enabled=1 AND NOT EXISTS (SELECT 1 FROM skill_bot sb WHERE sb.skill_id=skill.id)`
	if botID != "" {
		q += ` OR enabled=1 AND EXISTS (SELECT 1 FROM skill_bot sb WHERE sb.skill_id=skill.id AND sb.bot_id=?)`
	}
	q += ` ORDER BY name`
	var rows *sql.Rows
	var err error
	if botID == "" {
		rows, err = db.Query(q)
	} else {
		rows, err = db.Query(q, botID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Skill{}
	for rows.Next() {
		sk, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		if err = loadSkillBotIDs(db, &sk); err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

func SkillVisible(db *sql.DB, name, botID string) (Skill, error) {
	sk, err := SkillByName(db, name)
	if err != nil {
		return sk, err
	}
	if len(sk.BotIDs) == 0 {
		return sk, nil
	}
	for _, id := range sk.BotIDs {
		if botID != "" && id == botID {
			return sk, nil
		}
	}
	return Skill{}, sql.ErrNoRows
}

func (s *Server) loadSkill(id string) (Skill, error) {
	sk, err := scanSkill(s.db.QueryRow(skillSelect+` WHERE id=?`, id))
	if err == nil {
		err = loadSkillBotIDs(s.db, &sk)
	}
	return sk, err
}

func (s *Server) listSkills(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query(skillSelect + ` ORDER BY name`)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	skills := []Skill{}
	for rows.Next() {
		skill, err := scanSkill(rows)
		if err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := loadSkillBotIDs(s.db, &skill); err != nil {
			errw(w, 500, err.Error())
			return
		}
		skills = append(skills, skill)
	}
	if err := rows.Err(); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, skills)
}

func (s *Server) getSkill(w http.ResponseWriter, r *http.Request) {
	skill, err := s.loadSkill(idParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "skill not found")
		return
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, skill)
}

func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) {
	var in skillInput
	if decode(r, &in) != nil {
		errw(w, http.StatusBadRequest, "invalid skill JSON")
		return
	}
	config := skillConfig{Enabled: true, Kind: "skill"}
	if in.Name != nil {
		config.Name = *in.Name
	}
	if in.Description != nil {
		config.Description = *in.Description
	}
	if in.Body != nil {
		config.Body = *in.Body
	}
	if in.Enabled != nil {
		config.Enabled = *in.Enabled
	}
	if in.Kind != nil {
		config.Kind = *in.Kind
	}
	if err := validateSkill(config); err != nil {
		errw(w, http.StatusBadRequest, err.Error())
		return
	}
	botIDs := []string{}
	if in.BotIDs != nil {
		botIDs = *in.BotIDs
	}
	botIDs, err := s.validateSkillBots(botIDs)
	if err != nil {
		errw(w, 400, err.Error())
		return
	}
	skill, err := s.insertSkillScoped(config, botIDs)
	if err != nil {
		if isSkillDuplicate(err) {
			errw(w, http.StatusConflict, "skill name already exists")
		} else {
			errw(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	write(w, http.StatusCreated, skill)
}

func (s *Server) insertSkill(config skillConfig) (Skill, error) {
	return s.insertSkillScoped(config, nil)
}

func (s *Server) validateSkillBots(ids []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		var valid int
		err := s.db.QueryRow(`SELECT 1 FROM "user" WHERE id=? AND is_ai=1 AND deleted_at IS NULL`, id).Scan(&valid)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("bot %s not found", id)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func (s *Server) insertSkillScoped(config skillConfig, botIDs []string) (Skill, error) {
	timestamp := now()
	id := uid()
	tx, err := s.db.Begin()
	if err != nil {
		return Skill{}, err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO skill(id,name,description,body,enabled,kind,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, id, config.Name, config.Description, config.Body, config.Enabled, config.Kind, timestamp, timestamp)
	if err != nil {
		return Skill{}, err
	}
	for _, botID := range botIDs {
		if _, err = tx.Exec(`INSERT INTO skill_bot(skill_id,bot_id) VALUES(?,?)`, id, botID); err != nil {
			return Skill{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Skill{}, err
	}
	return s.loadSkill(id)
}

func isSkillDuplicate(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}

func (s *Server) updateSkill(w http.ResponseWriter, r *http.Request) {
	var in skillInput
	if decode(r, &in) != nil {
		errw(w, http.StatusBadRequest, "invalid skill JSON")
		return
	}
	id := idParam(r, "id")
	old, err := s.loadSkill(id)
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "skill not found")
		return
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	config := skillConfig{Name: old.Name, Description: old.Description, Body: old.Body, Enabled: old.Enabled, Kind: old.Kind}
	if in.Name != nil {
		config.Name = *in.Name
	}
	if in.Description != nil {
		config.Description = *in.Description
	}
	if in.Body != nil {
		config.Body = *in.Body
	}
	if in.Enabled != nil {
		config.Enabled = *in.Enabled
	}
	if in.Kind != nil {
		config.Kind = *in.Kind
	}
	if err := validateSkill(config); err != nil {
		errw(w, http.StatusBadRequest, err.Error())
		return
	}
	var botIDs []string
	if in.BotIDs != nil {
		botIDs, err = s.validateSkillBots(*in.BotIDs)
		if err != nil {
			errw(w, 400, err.Error())
			return
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	_, err = tx.Exec(`UPDATE skill SET name=?,description=?,body=?,enabled=?,kind=?,updated_at=? WHERE id=?`, config.Name, config.Description, config.Body, config.Enabled, config.Kind, now(), id)
	if err != nil {
		if isSkillDuplicate(err) {
			errw(w, http.StatusConflict, "skill name already exists")
		} else {
			errw(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if in.BotIDs != nil {
		if _, err = tx.Exec(`DELETE FROM skill_bot WHERE skill_id=?`, id); err == nil {
			for _, botID := range botIDs {
				if _, err = tx.Exec(`INSERT INTO skill_bot(skill_id,bot_id) VALUES(?,?)`, id, botID); err != nil {
					break
				}
			}
		}
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
	}
	if err = tx.Commit(); err != nil {
		errw(w, 500, err.Error())
		return
	}
	skill, err := s.loadSkill(id)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, skill)
}

func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	result, err := s.db.Exec(`DELETE FROM skill WHERE id=?`, idParam(r, "id"))
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count == 0 {
		errw(w, http.StatusNotFound, "skill not found")
		return
	}
	noContent(w)
}

func (s *Server) importSkill(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if decode(r, &in) != nil {
		errw(w, http.StatusBadRequest, "invalid skill import JSON")
		return
	}
	config, err := parseSkill(in.Text)
	if err != nil {
		errw(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateSkill(config); err != nil {
		errw(w, http.StatusBadRequest, err.Error())
		return
	}
	skill, err := s.insertSkill(config)
	if err != nil {
		if isSkillDuplicate(err) {
			errw(w, http.StatusConflict, "skill name already exists")
		} else {
			errw(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	write(w, http.StatusCreated, skill)
}

func parseSkill(text string) (skillConfig, error) {
	text = strings.TrimPrefix(text, "\xef\xbb\xbf")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return skillConfig{}, errors.New("SKILL.md must start with front matter (---)")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return skillConfig{}, errors.New("SKILL.md front matter is missing closing ---")
	}
	config := skillConfig{Enabled: true, Kind: "skill"}
	nameSeen, descriptionSeen := false, false
	for i := 1; i < end; i++ {
		line := lines[i]
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "name":
			parsed, err := parseSkillScalar(value)
			if err != nil {
				return skillConfig{}, fmt.Errorf("invalid name front matter: %w", err)
			}
			config.Name, nameSeen = parsed, true
		case "description":
			descriptionSeen = true
			if value == ">" || value == "|" {
				var block []string
				for i+1 < end {
					next := lines[i+1]
					if next != "" && !strings.HasPrefix(next, " ") && !strings.HasPrefix(next, "\t") {
						break
					}
					i++
					block = append(block, strings.TrimLeft(next, " \t"))
				}
				config.Description = strings.TrimSpace(parseSkillBlock(block, value == ">"))
			} else {
				parsed, err := parseSkillScalar(value)
				if err != nil {
					return skillConfig{}, fmt.Errorf("invalid description front matter: %w", err)
				}
				config.Description = parsed
			}
		}
	}
	if !nameSeen {
		return skillConfig{}, errors.New("SKILL.md front matter is missing name")
	}
	if !descriptionSeen {
		return skillConfig{}, errors.New("SKILL.md front matter is missing description")
	}
	bodyLines := lines[end+1:]
	if len(bodyLines) > 0 && bodyLines[0] == "" {
		bodyLines = bodyLines[1:]
	}
	config.Body = strings.Join(bodyLines, "\n")
	config.Enabled = true
	return config, nil
}

func parseSkillScalar(value string) (string, error) {
	if strings.HasPrefix(value, "\"") {
		var parsed string
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return "", err
		}
		return parsed, nil
	}
	if strings.HasPrefix(value, "'") {
		if len(value) < 2 || value[len(value)-1] != '\'' {
			return "", errors.New("unclosed single-quoted value")
		}
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'"), nil
	}
	return value, nil
}

func parseSkillBlock(lines []string, folded bool) string {
	var out strings.Builder
	for i, line := range lines {
		if i > 0 {
			if folded && line != "" && lines[i-1] != "" {
				out.WriteByte(' ')
			} else {
				out.WriteByte('\n')
			}
		}
		out.WriteString(line)
	}
	return out.String()
}

func (s *Server) exportSkill(w http.ResponseWriter, r *http.Request) {
	skill, err := s.loadSkill(idParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "skill not found")
		return
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	description := skill.Description
	if strings.ContainsAny(description, ":#\n\r") || strings.TrimSpace(description) != description {
		encoded, err := json.Marshal(description)
		if err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		description = string(encoded)
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="SKILL.md"`)
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "---\nname: %s\ndescription: %s\n---\n\n%s", skill.Name, description, skill.Body)
}
