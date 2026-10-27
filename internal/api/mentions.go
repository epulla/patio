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
	word := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }
	for i := 0; i < len(runes); i++ {
		if runes[i] != '@' || (i > 0 && word(runes[i-1])) {
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
