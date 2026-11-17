package api

import "regexp"

var effortToken = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,23}$`)

func hasEffort(level string, supported []string) bool {
	if level == "" {
		return false
	}
	for _, value := range supported {
		if value == level {
			return true
		}
	}
	return false
}

func validEffort(s string) bool {
	return s == "default" || s == "" || effortToken.MatchString(s)
}

func effortFromThinking(b bool) string {
	if b {
		return ""
	}
	return "low"
}

func normalizeEffort(s string) string {
	if s == "default" {
		return ""
	}
	return s
}
