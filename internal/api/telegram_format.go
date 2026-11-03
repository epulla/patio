package api

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	telegramFenceRE    = regexp.MustCompile("(?s)^```([^\\n`]*)\\n?(.*?)```$")
	telegramTableSepRE = regexp.MustCompile(`^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$`)
	telegramLinkRE     = regexp.MustCompile(`^\[([^\]]+)\]\(([^)]+)\)`)
	telegramHeadingRE  = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	telegramBulletRE   = regexp.MustCompile(`^(\s*)([-*+])\s+(.*)$`)
	telegramNumberRE   = regexp.MustCompile(`^\s*(\d+)\.\s+(.*)$`)
)

func telegramHTML(md string) string  { return renderTelegram(md, true) }
func telegramPlain(md string) string { return renderTelegram(md, false) }

func renderTelegram(md string, markup bool) string {
	lines := strings.Split(strings.TrimSpace(md), "\n")
	var out []string
	for i := 0; i < len(lines); {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			out = append(out, "")
			i++
			continue
		}
		if m := telegramFenceRE.FindStringSubmatch(line); m != nil {
			content := m[2]
			if markup {
				tag := "<pre><code>"
				if m[1] != "" {
					tag = `<pre><code class="language-` + telegramEscapeAttr(m[1]) + `">`
				}
				out = append(out, tag+telegramEscape(content)+"</code></pre>")
			} else {
				out = append(out, content)
			}
			i++
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			lang := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "```"))
			var code []string
			i++
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
				code = append(code, lines[i])
				i++
			}
			if i < len(lines) {
				i++
			}
			body := strings.Join(code, "\n")
			if markup {
				tag := "<pre><code>"
				if lang != "" {
					tag = `<pre><code class="language-` + telegramEscapeAttr(lang) + `">`
				}
				out = append(out, tag+telegramEscape(body)+"</code></pre>")
			} else {
				out = append(out, body)
			}
			continue
		}
		if i+1 < len(lines) && strings.Contains(line, "|") && telegramTableSepRE.MatchString(lines[i+1]) {
			rows := [][]string{telegramTableCells(line)}
			i += 2
			for i < len(lines) && strings.Contains(lines[i], "|") && strings.TrimSpace(lines[i]) != "" {
				rows = append(rows, telegramTableCells(lines[i]))
				i++
			}
			widths := make([]int, 0)
			for _, r := range rows {
				for j, c := range r {
					for len(widths) <= j {
						widths = append(widths, 0)
					}
					n := utf8.RuneCountInString(c)
					if n > widths[j] {
						widths[j] = n
					}
				}
			}
			var table []string
			for _, r := range rows {
				var cells []string
				for j, w := range widths {
					c := ""
					if j < len(r) {
						c = telegramPlain(r[j])
					}
					cells = append(cells, c+strings.Repeat(" ", w-utf8.RuneCountInString(c)))
				}
				table = append(table, strings.TrimRight(strings.Join(cells, " | "), " "))
			}
			if markup {
				out = append(out, "<pre>"+telegramEscape(strings.Join(table, "\n"))+"</pre>")
			} else {
				out = append(out, table...)
			}
			continue
		}
		trim := strings.TrimSpace(line)
		if trim == "---" || trim == "***" || trim == "___" {
			out = append(out, "")
			i++
			continue
		}
		if strings.HasPrefix(trim, ">") {
			var q []string
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), ">") {
				q = append(q, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), ">")))
				i++
			}
			text := telegramInline(strings.Join(q, "\n"), markup)
			if markup {
				text = "<blockquote>" + text + "</blockquote>"
			}
			out = append(out, text)
			continue
		}
		if h := telegramHeadingRE.FindStringSubmatch(trim); h != nil {
			s := telegramInline(h[1], markup)
			if markup {
				s = "<b>" + s + "</b>"
			}
			out = append(out, s)
			i++
			continue
		}
		if m := telegramBulletRE.FindStringSubmatch(line); m != nil {
			n := utf8.RuneCountInString(m[1]) / 2
			out = append(out, strings.Repeat("  ", n)+"• "+telegramInline(m[3], markup))
			i++
			continue
		}
		if m := telegramNumberRE.FindStringSubmatch(line); m != nil {
			out = append(out, m[1]+". "+telegramInline(m[2], markup))
			i++
			continue
		}
		out = append(out, telegramInline(line, markup))
		i++
	}
	return strings.TrimRight(strings.Join(out, "\n"), " \t\n")
}

func telegramTableCells(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "|")
	a := strings.Split(s, "|")
	for i := range a {
		a[i] = strings.TrimSpace(a[i])
	}
	return a
}

var telegramEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func telegramEscape(s string) string     { return telegramEscaper.Replace(s) }
func telegramEscapeAttr(s string) string { return strings.ReplaceAll(telegramEscape(s), `"`, "&quot;") }
func telegramSafeURL(s string) bool {
	s = strings.ToLower(s)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "tg:")
}
func telegramInline(s string, markup bool) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '`' {
			if j := strings.IndexByte(s[i+1:], '`'); j >= 0 {
				code := s[i+1 : i+1+j]
				if markup {
					b.WriteString("<code>" + telegramEscape(code) + "</code>")
				} else {
					b.WriteString(code)
				}
				i += j + 2
				continue
			}
		}
		if s[i] == '[' {
			if m := telegramLinkRE.FindStringSubmatch(s[i:]); m != nil {
				if markup && telegramSafeURL(m[2]) {
					b.WriteString(`<a href="` + telegramEscapeAttr(m[2]) + `">` + telegramInline(m[1], true) + "</a>")
				} else if !markup && telegramSafeURL(m[2]) {
					b.WriteString(m[1] + " (" + m[2] + ")")
				} else {
					b.WriteString(telegramInline(m[1], markup))
				}
				i += len(m[0])
				continue
			}
		}
		matched := false
		for _, p := range []struct{ m, t string }{{"**", "b"}, {"__", "b"}, {"~~", "s"}, {"*", "i"}, {"_", "i"}} {
			if strings.HasPrefix(s[i:], p.m) {
				j := strings.Index(s[i+len(p.m):], p.m)
				if j > 0 {
					c := s[i+len(p.m) : i+len(p.m)+j]
					if (p.m != "_" || i == 0 || !telegramWordByte(s[i-1])) && (p.m != "_" || i+len(p.m)+j+len(p.m) >= len(s) || !telegramWordByte(s[i+len(p.m)+j+len(p.m)])) && (p.m != "*" || c[0] != ' ' && c[len(c)-1] != ' ') {
						if markup {
							b.WriteString("<" + p.t + ">" + telegramInline(c, true) + "</" + p.t + ">")
						} else {
							b.WriteString(c)
						}
						i += len(p.m)*2 + j
						matched = true
						break
					}
				}
			}
		}
		if matched {
			continue
		}
		if markup {
			b.WriteString(telegramEscape(s[i : i+1]))
		} else {
			b.WriteByte(s[i])
		}
		i++
	}
	return b.String()
}
func telegramWordByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// telegramChunks splits Markdown at blank lines outside code fences into pieces that fit Telegram's
// 4096-character limit, measured on the rendered text (telegramPlain) with some margin. Oversize
// blocks are split: code fences and tables by lines (long code lines are cut, table headers
// repeated), other text at whitespace.
func telegramChunks(md string) []string {
	md = strings.TrimSpace(md)
	if md == "" {
		return nil
	}
	var blocks []string
	var block []string
	inFence := false
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if !inFence && strings.TrimSpace(line) == "" {
			if len(block) > 0 {
				blocks = append(blocks, strings.Join(block, "\n"))
				block = nil
			}
			continue
		}
		block = append(block, line)
	}
	if len(block) > 0 {
		blocks = append(blocks, strings.Join(block, "\n"))
	}
	var chunks []string
	cur := ""
	for _, block := range blocks {
		for _, piece := range telegramSplitBlock(block) {
			if cur != "" && telegramFits(cur+"\n\n"+piece) {
				cur += "\n\n" + piece
				continue
			}
			if cur != "" {
				chunks = append(chunks, cur)
			}
			cur = piece
		}
	}
	if cur != "" {
		chunks = append(chunks, cur)
	}
	return chunks
}

func telegramFits(md string) bool { return utf8.RuneCountInString(telegramPlain(md)) <= 4000 }

func telegramSplitBlock(block string) []string {
	if telegramFits(block) {
		return []string{block}
	}
	lines := strings.Split(block, "\n")
	// pack groups lines into pieces that fit once wrapped with head/tail.
	pack := func(head []string, body []string, tail []string) []string {
		var pieces, part []string
		wrap := func(p []string) string {
			return strings.Join(append(append(append([]string{}, head...), p...), tail...), "\n")
		}
		for _, l := range body {
			if len(part) > 0 && !telegramFits(wrap(append(part, l))) {
				pieces = append(pieces, wrap(part))
				part = nil
			}
			part = append(part, l)
		}
		if len(part) > 0 {
			pieces = append(pieces, wrap(part))
		}
		return pieces
	}
	if strings.HasPrefix(lines[0], "```") {
		var body []string
		for _, l := range lines[1:] {
			if strings.TrimSpace(l) == "```" {
				continue
			}
			for r := []rune(l); ; r = r[3000:] {
				if len(r) <= 3000 {
					body = append(body, string(r))
					break
				}
				body = append(body, string(r[:3000]))
			}
		}
		return pack(lines[:1], body, []string{"```"})
	}
	if len(lines) > 2 && telegramTableSepRE.MatchString(lines[1]) {
		var pieces []string
		for _, piece := range pack(lines[:2], lines[2:], nil) {
			if telegramFits(piece) {
				pieces = append(pieces, piece)
			} else {
				// A row too wide even alone: send it as plain text, cut to fit.
				cells := telegramTableCells(strings.Split(piece, "\n")[2])
				for i, c := range cells {
					cells[i] = telegramPlain(c)
				}
				pieces = append(pieces, telegramCutText(strings.Join(cells, " | "))...)
			}
		}
		return pieces
	}
	return telegramCutText(block)
}

// telegramCutText cuts text at whitespace into pieces of at most 3000 runes.
func telegramCutText(block string) []string {
	var pieces []string
	runes := []rune(block)
	for len(runes) > 3000 {
		cut := 3000
		if at := strings.LastIndexAny(string(runes[:cut]), " \n"); at > 0 {
			cut = utf8.RuneCountInString(string(runes[:cut])[:at])
		}
		pieces = append(pieces, strings.TrimSpace(string(runes[:cut])))
		runes = runes[cut:]
	}
	return append(pieces, strings.TrimSpace(string(runes)))
}
