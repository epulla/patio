package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const maxIndexedMemories = 30

const (
	chatLinkRules     = "In patio, reply in Markdown. Link tickets as [CODE: Title](/tickets/<ticket id>), boards as [Board name](/boards/<board id>), and chats as [Chat title](/ai/chats/<chat id>), using returned IDs only; never invent IDs."
	martinPersona     = "You are Martin, playful, joyful AI companion for everyday/weekly tasks. Be supportive, concise, joke sometimes, recommend."
	ticketComments    = "When working on a ticket in this chat, record progress, findings, decisions, and manual-check results as ticket comments with add_comment instead of growing the description. Keep comments terse, precise, and to the point."
	askUserPrompt     = "To ask the user multiple-choice questions, use the Patio ask_user tool with this chat's ID as chat_id; set multiple true when more than one option can apply, then end your turn."
	chatHandoffPrompt = "To message another chat use send_to_chat; to delegate into a new child chat use create_chat. Messages are queued and delivered within seconds. Set report_back when you need a result returned automatically; do not call send_to_chat to answer a message with report_back enabled. Give short results inline, and long results as a ticket comment plus a pointer."
)

func ChatSystemPrompt(enabled bool, chatID string, skills []Skill) string {
	prompt := "You are a neutral assistant. " + chatLinkRules + " " + askUserPrompt + " " + chatHandoffPrompt
	if enabled {
		prompt = martinPersona + " " + ticketComments + " " + chatLinkRules + " " + askUserPrompt + " " + chatHandoffPrompt
	}
	prompt += skillIndex(skills)
	// The chat ID goes last so the text before it is the same for every chat and stays cacheable.
	if chatID != "" {
		prompt += " This chat's ID is " + chatID + "."
	}
	return prompt
}

// ChatPrompt builds ChatSystemPrompt from the stored persona setting and enabled skills.
func ChatPrompt(db *sql.DB, chatID string) (string, error) {
	persona := ""
	if db != nil && chatID != "" {
		var name, prompt string
		err := db.QueryRow(`SELECT u.name,u.persona_prompt FROM chat c JOIN "user" u ON u.id=c.bot_id WHERE c.id=? AND u.is_ai=1 AND u.deleted_at IS NULL`, chatID).Scan(&name, &prompt)
		if err != nil && err != sql.ErrNoRows {
			return "", err
		}
		if err == nil {
			persona = "You are " + name + ". " + prompt
		}
	}
	var skills []Skill
	var err error
	if db != nil {
		if skills, err = EnabledSkills(db); err != nil {
			return "", err
		}
	}
	prompt := "You are a neutral assistant. " + chatLinkRules + " " + askUserPrompt + " " + chatHandoffPrompt
	if persona != "" {
		prompt = persona + " " + ticketComments + " " + chatLinkRules + " " + askUserPrompt + " " + chatHandoffPrompt
	}
	prompt += skillIndex(skills)
	if chatID != "" {
		prompt += " This chat's ID is " + chatID + "."
	}
	return prompt, nil
}

// MartinPersonaEnabled remains for the local connector's legacy prompt builder.
func MartinPersonaEnabled(db *sql.DB) (bool, error) {
	if db == nil {
		return true, nil
	}
	var raw string
	err := db.QueryRow(`SELECT value FROM app_setting WHERE key='default_bot_id'`).Scan(&raw)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	var id string
	if err = json.Unmarshal([]byte(raw), &id); err != nil {
		return false, err
	}
	return id != "none", nil
}

// skillIndex lists each skill and memory with a short trigger; full text stays behind load_skill.
func skillIndex(skills []Skill) string {
	var tasks, memories []Skill
	for _, skill := range skills {
		if skill.Kind == "memory" {
			memories = append(memories, skill)
		} else {
			tasks = append(tasks, skill)
		}
	}
	// Cap memories to bound the prompt; the most recently updated ones win.
	slices.SortFunc(memories, func(a, b Skill) int { return strings.Compare(b.UpdatedAt, a.UpdatedAt) })
	memories = memories[:min(len(memories), maxIndexedMemories)]
	var index strings.Builder
	if len(tasks) > 0 {
		index.WriteString(" Patio skills (call load_skill with the name before a task one covers):")
		for _, skill := range tasks {
			fmt.Fprintf(&index, "\n- %s: %s", skill.Name, skillTrigger(skill.Description))
		}
	}
	if len(memories) > 0 {
		index.WriteString("\n Patio memories (the user's saved preferences and facts; follow them, and call load_skill with the name for details):")
		for _, memory := range memories {
			fmt.Fprintf(&index, "\n- %s: %s", memory.Name, skillTrigger(memory.Description))
		}
	}
	return index.String()
}

// skillTrigger keeps the description's first sentence, capped at 20 words.
func skillTrigger(description string) string {
	sentence, _, _ := strings.Cut(description, ". ")
	words := strings.Fields(sentence)
	if len(words) > 20 {
		return strings.Join(words[:20], " ") + "…"
	}
	return strings.TrimSuffix(strings.Join(words, " "), ".")
}
