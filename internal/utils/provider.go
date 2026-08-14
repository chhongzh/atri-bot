package utils

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
)

func GetProviderPath(cwd, id, url, branch string) string {
	digest := sha256.Sum256([]byte(NormalizeGitRepoURL(url) + "\x00" + strings.TrimSpace(branch)))
	directory := fmt.Sprintf("%s-%x", id, digest[:6])
	return filepath.Join(cwd, "data", "character-providers", directory)
}
