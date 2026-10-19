package api

import (
	"fmt"
	"strings"
	"testing"
)

func TestChatSystemPromptSkillIndex(t *testing.T) {
	long := "One two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen seventeen eighteen nineteen twenty twentyone. Second sentence."
	prompt := ChatSystemPrompt(true, "chat-id", []Skill{
		{Name: "pirate-voice", Description: "Use when the user asks for a pirate greeting. Never otherwise."},
		{Name: "long-one", Description: long},
	})
	for _, want := range []string{
		"\n- pirate-voice: Use when the user asks for a pirate greeting",
		"\n- long-one: One two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen seventeen eighteen nineteen twenty…",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "Never otherwise") || strings.Contains(prompt, "twentyone") {
		t.Errorf("prompt kept text past the trigger:\n%s", prompt)
	}
	if !strings.HasSuffix(prompt, " This chat's ID is chat-id.") {
		t.Errorf("chat ID is not last:\n%s", prompt)
	}
	if strings.Contains(ChatSystemPrompt(true, "chat-id", nil), "Patio skills") {
		t.Error("prompt lists skills when none are enabled")
	}
}

func TestChatSystemPromptMemoryIndex(t *testing.T) {
	skills := []Skill{{Name: "pirate-voice", Description: "Use for pirate greetings.", Kind: "skill"}}
	for i := range maxIndexedMemories + 1 {
		skills = append(skills, Skill{Name: fmt.Sprintf("memory-%02d", i), Description: "A fact.", Kind: "memory", UpdatedAt: fmt.Sprintf("2026-10-%02d", i+1)})
	}
	prompt := ChatSystemPrompt(true, "chat-id", skills)
	skillsAt, memoriesAt := strings.Index(prompt, "Patio skills"), strings.Index(prompt, "Patio memories")
	if skillsAt < 0 || memoriesAt < skillsAt || strings.Index(prompt, "- pirate-voice") > memoriesAt {
		t.Fatalf("skills and memories are not in separate sections:\n%s", prompt)
	}
	if strings.Contains(prompt, "- memory-00:") || !strings.Contains(prompt, "- memory-30:") || !strings.Contains(prompt, "- memory-01:") {
		t.Errorf("memory cap did not drop only the oldest memory:\n%s", prompt)
	}
}
