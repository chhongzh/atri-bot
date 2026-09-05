package character

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
)

func normalizeGitRepoURL(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "github.com/") {
		return "https://" + value
	}
	return value
}

func providerPath(cwd, id, url, branch string) string {
	digest := sha256.Sum256([]byte(normalizeGitRepoURL(url) + "\x00" + strings.TrimSpace(branch)))
	directory := fmt.Sprintf("%s-%x", id, digest[:6])
	return filepath.Join(cwd, "data", "character-providers", directory)
}
