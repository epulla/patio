package api

import (
	"database/sql"
	"encoding/json"
	"errors"
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
	if db != nil {
		rows, err := db.Query(`SELECT name,COALESCE(description,'') FROM "user" WHERE is_ai=1 AND deleted_at IS NULL ORDER BY name COLLATE NOCASE,rowid`)
		if err != nil {
			return "", err
		}
		var bots strings.Builder
		for rows.Next() {
			var name, description string
			if err := rows.Scan(&name, &description); err != nil {
				rows.Close()
				return "", err
			}
			if bots.Len() == 0 {
				bots.WriteString(" Available bots (to hand off, call create_chat with bot set to the bot's name, then write \"@Name <task> → [chat](/ai/chats/<id>)\" in your reply; @Name text is display only and never opens a chat):")
			}
			fmt.Fprintf(&bots, "\n- %s", name)
			if description != "" {
				bots.WriteString(": " + description)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return "", err
		}
		rows.Close()
		prompt += bots.String()
		section, err := workflowStepNote(db, chatID)
		if err != nil {
			return "", err
		}
		prompt += section
		var inWorkflow int
		if chatID != "" {
			if err := db.QueryRow(`SELECT count(*) FROM chat WHERE id=? AND workflow_run_id IS NOT NULL`, chatID).Scan(&inWorkflow); err != nil {
				return "", err
			}
		}
		if inWorkflow == 0 {
			rows, err := db.Query(`SELECT name,COALESCE(description,''),graph,id FROM workflow WHERE deleted_at IS NULL ORDER BY name COLLATE NOCASE,rowid`)
			if err != nil {
				return "", err
			}
			var saved strings.Builder
			for rows.Next() {
				var name, description, raw, id string
				if err := rows.Scan(&name, &description, &raw, &id); err != nil {
					rows.Close()
					return "", err
				}
				var graph workflowGraph
				if err := json.Unmarshal([]byte(raw), &graph); err != nil {
					rows.Close()
					return "", err
				}
				if saved.Len() == 0 {
					saved.WriteString(" Saved workflows (to start one in this chat, call run_workflow with its id and this chat's chat_id; it waits for the user before handing off):")
				}
				detail := description
				if detail == "" {
					var names []string
					for _, n := range graph.Nodes {
						var botName string
						if err := db.QueryRow(`SELECT name FROM "user" WHERE id=?`, n.BotID).Scan(&botName); err != nil {
							rows.Close()
							return "", err
						}
						names = append(names, botName)
					}
					detail = strings.Join(names, " → ")
				}
				fmt.Fprintf(&saved, "\n- %s (id %s): %s", name, id, detail)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return "", err
			}
			rows.Close()
			prompt += saved.String()
		}
	}
	prompt += skillIndex(skills)
	if chatID != "" {
		prompt += " This chat's ID is " + chatID + "."
	}
	return prompt, nil
}

// workflowStepNote tells a workflow chat what its step is and how to hand off. It goes in
// the system prompt and in each step's first message, where models follow it more reliably.
// Chats outside a workflow, or whose workflow was deleted, get "".
func workflowStepNote(db *sql.DB, chatID string) (string, error) {
	var node, bname sql.NullString
	var wait int
	var w workflow
	var graph string
	err := db.QueryRow(`SELECT c.workflow_node,u.name,wf.name,wf.graph,c.workflow_wait FROM chat c JOIN workflow wf ON wf.id=c.workflow_id AND wf.deleted_at IS NULL LEFT JOIN "user" u ON u.id=c.bot_id WHERE c.id=?`, chatID).Scan(&node, &bname, &w.Name, &graph, &wait)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !node.Valid {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal([]byte(graph), &w.Graph); err != nil {
		return "", err
	}
	note := fmt.Sprintf(` Workflow %q: you are the step run by %s.`, w.Name, bname.String)
	var edgeLines strings.Builder
	for _, e := range w.Graph.Edges {
		if e.From != node.String {
			continue
		}
		dest := ""
		for _, step := range w.Graph.Nodes {
			if step.ID == e.To {
				if err := db.QueryRow(`SELECT name FROM "user" WHERE id=?`, step.BotID).Scan(&dest); err != nil {
					return "", err
				}
				break
			}
		}
		join := ""
		if countWorkflowIncoming(w.Graph, e.To) >= 2 {
			join = " (waits for all its inputs)"
		}
		instruction := e.Instruction
		if instruction == "" {
			instruction = "continue the task"
		}
		fmt.Fprintf(&edgeLines, "\n- workflow_edge %q → %s%s: %s", e.ID, dest, join, instruction)
	}
	if edgeLines.Len() == 0 {
		return note + " This is the last step: do it and finish with your result.", nil
	}
	if wait == 1 {
		return note + fmt.Sprintf(" Work with the user on your part (clarify, plan, agree on the task) and keep talking as long as they want, but do only your part: the work of later steps belongs to the next bots, so never do it yourself. Do not hand off until the user tells you to continue (for example \"go\" or \"hand off\"); if they already said so, hand off as soon as the task is clear. Then call create_chat with chat_id %q, the workflow_edge below and a prompt that carries everything agreed so far (Patio picks the bot):", chatID) + edgeLines.String(), nil
	}
	return note + fmt.Sprintf(" Do only your part; later steps belong to other bots. Before ending your turn, hand off by calling create_chat with chat_id %q, the workflow_edge below and a prompt for the next bot (Patio picks the bot):", chatID) + edgeLines.String(), nil
}

// withStepNote appends a workflow chat's step note to its first message.
func withStepNote(db *sql.DB, chatID, text string) (string, error) {
	note, err := workflowStepNote(db, chatID)
	if err != nil || note == "" {
		return text, err
	}
	return text + "\n\n---\n" + strings.TrimSpace(note), nil
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
