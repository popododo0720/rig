package main

func truncateOutput(s string, max int) string {
	return truncateWithSuffix(s, max, "...")
}

func truncate(s string, max int) string {
	return truncateWithSuffix(s, max, "..")
}

func truncateWithSuffix(s string, max int, suffix string) string {
	if len(s) <= max {
		return s
	}
	if max <= len(suffix) {
		return suffix[:max]
	}
	return s[:max-len(suffix)] + suffix
}
