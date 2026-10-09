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
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type skillInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Body        *string `json:"body"`
	Enabled     *bool   `json:"enabled"`
}

type skillConfig struct {
	Name        string
	Description string
	Body        string
	Enabled     bool
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
	description := strings.TrimSpace(config.Description)
	if n := utf8.RuneCountInString(description); n < 1 || n > 1024 {
		return errors.New("description must be 1 to 1024 characters")
	}
	if len(config.Body) > maxSkillBody {
		return errors.New("body must be at most 64 KB")
	}
	return nil
}

const skillSelect = `SELECT id,name,description,body,enabled,created_at,updated_at FROM skill`

func scanSkill(row interface{ Scan(...any) error }) (Skill, error) {
	var skill Skill
	var enabled int
	err := row.Scan(&skill.ID, &skill.Name, &skill.Description, &skill.Body, &enabled, &skill.CreatedAt, &skill.UpdatedAt)
	skill.Enabled = enabled != 0
	return skill, err
}

func SkillByName(db *sql.DB, name string) (Skill, error) {
	return scanSkill(db.QueryRow(skillSelect+` WHERE name=?`, name))
}

func EnabledSkills(db *sql.DB) ([]Skill, error) {
	rows, err := db.Query(skillSelect + ` WHERE enabled=1 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	skills := []Skill{}
	for rows.Next() {
		skill, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		skills = append(skills, skill)
	}
	return skills, rows.Err()
}

func (s *Server) loadSkill(id string) (Skill, error) {
	return scanSkill(s.db.QueryRow(skillSelect+` WHERE id=?`, id))
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
	config := skillConfig{Enabled: true}
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

func (s *Server) insertSkill(config skillConfig) (Skill, error) {
	timestamp := now()
	id := uid()
	_, err := s.db.Exec(`INSERT INTO skill(id,name,description,body,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, id, config.Name, config.Description, config.Body, config.Enabled, timestamp, timestamp)
	if err != nil {
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
	config := skillConfig{Name: old.Name, Description: old.Description, Body: old.Body, Enabled: old.Enabled}
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
	if err := validateSkill(config); err != nil {
		errw(w, http.StatusBadRequest, err.Error())
		return
	}
	_, err = s.db.Exec(`UPDATE skill SET name=?,description=?,body=?,enabled=?,updated_at=? WHERE id=?`, config.Name, config.Description, config.Body, config.Enabled, now(), id)
	if err != nil {
		if isSkillDuplicate(err) {
			errw(w, http.StatusConflict, "skill name already exists")
		} else {
			errw(w, http.StatusInternalServerError, err.Error())
		}
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
	config := skillConfig{Enabled: true}
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
