package api

import "regexp"

var effortToken = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,23}$`)

func effortFor(level string, supported []string) string {
	if level == "default" {
		return ""
	}
	ranks := map[string]int{"none": 0, "minimal": 1, "low": 2, "small": 2, "medium": 3, "high": 4, "xhigh": 5, "max": 6}
	target, ok := ranks[level]
	if !ok {
		return ""
	}
	best, distance := "", int(^uint(0)>>1)
	for _, value := range supported {
		rank, exists := ranks[value]
		if !exists {
			continue
		}
		delta := rank - target
		if delta < 0 {
			delta = -delta
		}
		if delta < distance || (delta == distance && rank < ranks[best]) {
			best, distance = value, delta
		}
	}
	return best
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
