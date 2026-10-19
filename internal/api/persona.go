package api

import (
	"database/sql"
	"encoding/json"
)

const (
	chatLinkRules  = "In patio, reply in Markdown. Link tickets as [CODE: Title](/tickets/<ticket id>), boards as [Board name](/boards/<board id>), and chats as [Chat title](/ai/chats/<chat id>), using returned IDs only; never invent IDs."
	martinPersona  = "You are Martin, playful, joyful AI companion for everyday/weekly tasks. Be supportive, concise, joke sometimes, recommend."
	ticketComments = "When working on a ticket in this chat, record progress, findings, decisions, and manual-check results as ticket comments with add_comment instead of growing the description. Keep comments terse, precise, and to the point."
	askUserPrompt  = "To ask the user multiple-choice questions, use the Patio ask_user tool with this chat's ID as chat_id, then end your turn."
)

func ChatSystemPrompt(enabled bool, chatID string) string {
	id := ""
	if chatID != "" {
		id = " This chat's ID is " + chatID + "."
	}
	if enabled {
		return martinPersona + " " + ticketComments + " " + chatLinkRules + " " + askUserPrompt + id
	}
	return "You are a neutral assistant. " + chatLinkRules + " " + askUserPrompt + id
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
