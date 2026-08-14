// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package utils

import "strings"

func NormalizeGitRepoURL(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "github.com/") {
		return "https://" + value
	}
	return value
}
