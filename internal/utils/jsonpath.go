package utils

import (
	"fmt"
	"regexp"
	"strings"
)

var jsonPathPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*(?:\.(?:[A-Za-z_][A-Za-z0-9_-]*|[0-9]+))*$`)

// ValidateJSONPath trims and validates a dotted JSON path for gjson/sjson.
// subject is used in error messages (e.g. "tool config path"); maxBytes
// enforces an optional byte-length limit (0 disables the limit).
func ValidateJSONPath(path, subject string, maxBytes int) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%s is required", subject)
	}
	if !jsonPathPattern.MatchString(path) {
		return "", fmt.Errorf("invalid %s %q", subject, path)
	}
	if maxBytes > 0 && len(path) > maxBytes {
		return "", fmt.Errorf("%s exceeds %d bytes", subject, maxBytes)
	}
	return path, nil
}
