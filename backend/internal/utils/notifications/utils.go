package notifications

import "strings"

// SanitizeForEmail sanitizes text for safe use in email subjects
func SanitizeForEmail(text string) string {
	// Remove control characters and newlines
	text = strings.ReplaceAll(text, "\r", "")
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\t", " ")

	// Trim whitespace
	return strings.TrimSpace(text)
}
