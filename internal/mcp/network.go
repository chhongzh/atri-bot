// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package mcp

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/security"
	"github.com/pkg/errors"
)

// validateProviderURL checks that rawURL is an absolute http(s) URL.
func validateProviderURL(rawURL string, allowPrivateIP bool) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return errs.ErrMCPURLEmpty
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return errors.Wrap(err, "invalid mcp url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errs.ErrInvalidScheme
	}
	if parsed.Hostname() == "" {
		return errs.ErrMCPURLHostRequired
	}
	if parsed.User != nil {
		return errs.ErrMCPURLUserInfoForbidden
	}
	if parsed.Fragment != "" {
		return errs.ErrMCPURLFragmentForbidden
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
