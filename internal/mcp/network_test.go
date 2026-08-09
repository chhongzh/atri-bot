package mcp

import "testing"

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
		got, err := isInternalHost(host)
		if err != nil {
			t.Errorf("isInternalHost(%q) error = %v", host, err)
			continue
		}
		if !got {
			t.Errorf("isInternalHost(%q) = false, want true", host)
		}
	}

	if got, err := isInternalHost("example.com"); err != nil || got {
		t.Errorf("isInternalHost(example.com) = %v, %v; want false, nil", got, err)
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
