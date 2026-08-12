package utils

import "strings"

func GitNormalizeRepoURL(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "github.com/") {
		return "https://" + value
	}
	return value
}
