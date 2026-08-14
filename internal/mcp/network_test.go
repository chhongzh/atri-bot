// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package mcp

import (
	"testing"

	"github.com/chhongzh/atri-bot/internal/security"
)

func TestIsInternalHost(t *testing.T) {
	internal := []string{
		"",
		"localhost",
		"foo.localhost",
		"foo.internal",
		"foo.local",
		"foo.lan",
		"foo.home.arpa",
		"127.0.0.1",
		"10.0.0.5",
		"172.16.0.1",
		"192.168.1.1",
		"169.254.1.1",
		"::1",
		"fe80::1",
	}
	for _, host := range internal {
		if !security.IsInternalHost(host) {
			t.Errorf("isInternalHost(%q) = false, want true", host)
		}
	}

	if security.IsInternalHost("example.com") {
		t.Error("isInternalHost(example.com) = true, want false")
	}
}

func TestValidateProviderURLShape(t *testing.T) {
	tests := []string{
		"",
		"not-a-url",
		"ftp://example.com/sse",
		"https://user:password@example.com/sse",
		"https://example.com/sse#fragment",
	}
	for _, raw := range tests {
		err := validateProviderURL(raw, true)
		if err == nil {
			t.Errorf("validateProviderURL(%q) = nil, want error", raw)
		}
	}
}
