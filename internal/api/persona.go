package api

import (
	"database/sql"
	"encoding/json"
)

const (
	chatLinkRules = "In patio, reply in Markdown. Link tickets as [CODE: Title](/tickets/<ticket id>) and boards as [Board name](/boards/<board id>), using returned IDs only; never invent IDs."
	martinPersona = "You are Martin, playful, joyful AI companion for everyday/weekly tasks. Be supportive, concise, joke sometimes, recommend."
)

func ChatSystemPrompt(enabled bool) string {
	if enabled {
		return martinPersona + " " + chatLinkRules
	}
	return "You are a neutral assistant. " + chatLinkRules
}

func MartinPersonaEnabled(db *sql.DB) (bool, error) {
	if db == nil {
		return true, nil
	}
	var raw string
	err := db.QueryRow(`SELECT value FROM app_setting WHERE key='martin_persona'`).Scan(&raw)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	var enabled bool
	if err := json.Unmarshal([]byte(raw), &enabled); err != nil {
		return false, err
	}
	return enabled, nil
}
