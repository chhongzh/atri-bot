// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
)

func TestSafeDialerBlocksInternalHost(t *testing.T) {
	dialer := NewSafeDialer(&net.Dialer{}, false)
	_, err := dialer.DialContext(context.Background(), "tcp", "127.0.0.1:80")
	if !errors.Is(err, ErrPrivateAddressBlocked) {
		t.Fatalf("DialContext() error = %v, want ErrPrivateAddressBlocked", err)
	}
}

func TestIsInternalHost(t *testing.T) {
	for _, host := range []string{"", "localhost", "service.internal", "192.168.1.1", "::1"} {
		if !IsInternalHost(host) {
			t.Errorf("IsInternalHost(%q) = false, want true", host)
		}
	}
	if IsInternalHost("example.com") {
		t.Error("IsInternalHost(example.com) = true, want false")
	}
}

func TestSafeHTTPTransportChecksTargetBeforeProxy(t *testing.T) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	client := &http.Client{Transport: NewSafeHTTPTransport(transport, false)}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:8080", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Do(request)
	if !errors.Is(err, ErrPrivateAddressBlocked) {
		t.Fatalf("client.Do() error = %v, want ErrPrivateAddressBlocked", err)
	}
}
