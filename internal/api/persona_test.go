package api

import (
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
