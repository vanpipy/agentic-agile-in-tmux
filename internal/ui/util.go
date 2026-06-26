// Package ui — util.go: shared utility functions.
package ui

// truncate shortens s to max characters, adding " ..." if truncated.
// Used for status bar / interception modal text.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max < 4 {
		return s[:max]
	}
	return s[:max-4] + " ..."
}
