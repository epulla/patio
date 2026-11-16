package api

func validEffort(s string) bool {
	switch s {
	case "default", "small", "medium", "high", "xhigh":
		return true
	default:
		return false
	}
}

func effortFromThinking(b bool) string {
	if b {
		return "default"
	}
	return "small"
}
