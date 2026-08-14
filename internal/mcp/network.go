// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package mcp

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/security"
)

// validateProviderURL checks that rawURL is an absolute http(s) URL.
func validateProviderURL(rawURL string, allowPrivateIP bool) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return errors.New("mcp url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid mcp url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errs.ErrInvalidScheme
	}
	if parsed.Hostname() == "" {
		return errors.New("mcp url host is required")
	}
	if parsed.User != nil {
		return errors.New("mcp url must not contain user info")
	}
	if parsed.Fragment != "" {
		return errors.New("mcp url must not contain a fragment")
	}
	if allowPrivateIP {
		return nil
	}
	if security.IsInternalHost(parsed.Hostname()) {
		return errs.ErrInternalHostBlocked
	}
	return nil
}

func newMCPHTTPClient(allowPrivateIP bool) (*http.Client, func()) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &http.Client{Transport: security.NewSafeHTTPTransport(transport, allowPrivateIP)}, transport.CloseIdleConnections
}
