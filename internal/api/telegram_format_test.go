package api

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTelegramFormat(t *testing.T) {
	cases := []struct{ in, want string }{
		{"**bold**", "<b>bold</b>"}, {"__bold__", "<b>bold</b>"}, {"*italic*", "<i>italic</i>"}, {"_italic_", "<i>italic</i>"}, {"~~strike~~", "<s>strike</s>"},
		{"`a < b`", "<code>a &lt; b</code>"}, {"```go\nx < y\n```", "<pre><code class=\"language-go\">x &lt; y</code></pre>"},
		{"[site](https://example.com?a=1&b=2)", `<a href="https://example.com?a=1&amp;b=2">site</a>`}, {"[bad](javascript:x)", "bad"}, {"[ticket](/tickets/abc)", "ticket"},
		{"# Heading", "<b>Heading</b>"}, {"> one\n> **two**", "<blockquote>one\n<b>two</b></blockquote>"}, {"- one\n  - two", "• one\n  • two"}, {"1. one", "1. one"},
		{"|a|bb|\n|---|---|\n|x|y|", "<pre>a | bb\nx | y</pre>"}, {"---", ""}, {"a < b & c", "a &lt; b &amp; c"}, {"snake_case", "snake_case"}, {"**x", "**x"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := telegramHTML(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestTelegramPlain(t *testing.T) {
	cases := []struct{ in, want string }{{"**bold** and *it*", "bold and it"}, {"[x](https://e.test)", "x (https://e.test)"}, {"[x](/tickets/abc)", "x"}, {"# Head\n> quote\n- item", "Head\nquote\n• item"}, {"`code`", "code"}}
	for _, tc := range cases {
		if got := telegramPlain(tc.in); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestTelegramChunks(t *testing.T) {
	md := strings.Repeat("paragraph words ", 180) + "\n\n```txt\n" + strings.Repeat("inside fence line\n", 180) + "```\n\n" + strings.Repeat("last block ", 180)
	chunks := telegramChunks(md)
	if len(chunks) < 2 {
		t.Fatal("expected multiple chunks")
	}
	for _, c := range chunks {
		if n := utf8.RuneCountInString(telegramHTML(c)); n > 4096 {
			t.Fatalf("chunk has %d runes", n)
		}
	}
	for _, c := range chunks {
		if strings.Count(c, "```")%2 != 0 {
			t.Fatalf("chunk splits a fence: %q", c[:min(len(c), 40)])
		}
	}
}

func TestTelegramChunksOversizeCode(t *testing.T) {
	md := "```go\n" + strings.Repeat("line content\n", 500) + "```"
	chunks := telegramChunks(md)
	if len(chunks) < 2 {
		t.Fatal("expected split code chunks")
	}
	for _, c := range chunks {
		if !strings.HasPrefix(c, "```go\n") || !strings.HasSuffix(c, "\n```") {
			t.Fatalf("invalid fenced chunk: %q", c[:min(len(c), 30)])
		}
		if utf8.RuneCountInString(telegramHTML(c)) > 4096 {
			t.Fatal("oversize chunk")
		}
	}
}
