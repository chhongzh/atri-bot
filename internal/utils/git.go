package utils

import "strings"

func NormalizeGitRepoURL(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "github.com/") {
		return "https://" + value
	}
	return value
}
