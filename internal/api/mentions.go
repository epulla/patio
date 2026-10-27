package api

import (
	"strings"
	"unicode"
)

type mention struct{ Bot, Text string }

func parseMentions(text string, bots []string) []mention {
	known := make(map[string]string, len(bots))
	for _, bot := range bots {
		known[strings.ToLower(bot)] = bot
	}
	runes := []rune(text)
	var found []struct {
		start, end int
		bot        string
	}
	// Mentions inside fenced code blocks or inline code (a backtick run closed by one of the same length) never fire.
	code := make([]bool, len(runes))
	fenced := false
	for start, end := 0, 0; start < len(runes); start = end + 1 {
		for end = start; end < len(runes) && runes[end] != '\n'; end++ {
		}
		fence := strings.HasPrefix(strings.TrimLeft(string(runes[start:end]), " \t"), "```")
		if fence || fenced {
			for k := start; k < end; k++ {
				code[k] = true
			}
			fenced = fenced != fence
			continue
		}
		for k := start; k < end; {
			if runes[k] != '`' {
				k++
				continue
			}
			open := k
			for k < end && runes[k] == '`' {
				k++
			}
			for m := k; m < end; {
				if runes[m] != '`' {
					m++
					continue
				}
				run := m
				for m < end && runes[m] == '`' {
					m++
				}
				if m-run == k-open {
					for x := open; x < m; x++ {
						code[x] = true
					}
					k = m
					break
				}
			}
		}
	}
	word := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }
	for i := 0; i < len(runes); i++ {
		if runes[i] != '@' || code[i] || (i > 0 && word(runes[i-1])) {
			continue
		}
		j := i + 1
		for j < len(runes) && word(runes[j]) {
			j++
		}
		if j == i+1 {
			continue
		}
		if bot, ok := known[strings.ToLower(string(runes[i+1:j]))]; ok {
			found = append(found, struct {
				start, end int
				bot        string
			}{i, j, bot})
			i = j - 1
		}
	}
	var out []mention
	for i, m := range found {
		end := len(runes)
		for k := m.end; k+1 < end; k++ {
			if runes[k] == '\n' {
				j := k + 1
				for j < end && (runes[j] == ' ' || runes[j] == '\t') {
					j++
				}
				if j < end && runes[j] == '\n' {
					end = k
					break
				}
			}
		}
		if i+1 < len(found) && found[i+1].start < end {
			end = found[i+1].start
		}
		instruction := strings.Trim(string(runes[m.end:end]), " \t\r\n,;:")
		if instruction != "" {
			out = append(out, mention{m.bot, instruction})
		}
	}
	return out
}
