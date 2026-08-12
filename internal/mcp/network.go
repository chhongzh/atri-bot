package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chhongzh/atri-bot/internal/errs"
)

// validateProviderURL checks that rawURL is an absolute http(s) URL. When
// blockInternal is true it additionally rejects known internal hostnames and
// private IP literals without performing network I/O.
func validateProviderURL(rawURL string, blockInternal bool) error {
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
	if !blockInternal {
		return nil
	}
	internal, err := isInternalHost(parsed.Hostname())
	if err != nil {
		return err
	}
	if internal {
		return errs.ErrInternalHostBlocked
	}
	return nil
}

// validateProviderURLContext resolves hostnames before a connection is made.
// The transport repeats the same check at dial time to prevent DNS rebinding
// and redirects from bypassing the internal-network guard.
func validateProviderURLContext(ctx context.Context, rawURL string, blockInternal bool) error {
	if err := validateProviderURL(rawURL, blockInternal); err != nil || !blockInternal {
		return err
	}
	parsed, _ := url.Parse(strings.TrimSpace(rawURL))
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, parsed.Hostname())
	if err != nil {
		return fmt.Errorf("resolve mcp url host: %w", err)
	}
	if len(addresses) == 0 {
		return errors.New("mcp url host resolved to no addresses")
	}
	for _, address := range addresses {
		if isPrivateIP(address.IP) {
			return errs.ErrInternalHostBlocked
		}
	}
	return nil
}

func newMCPHTTPClient(blockInternal bool) (*http.Client, func()) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if blockInternal {
		transport.Proxy = nil
		dialer := &net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("parse mcp dial address: %w", err)
			}
			if internal, _ := isInternalHost(host); internal {
				return nil, errs.ErrInternalHostBlocked
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("resolve mcp dial host: %w", err)
			}
			for _, resolved := range addresses {
				if isPrivateIP(resolved.IP) {
					return nil, errs.ErrInternalHostBlocked
				}
			}
			if len(addresses) == 0 {
				return nil, errors.New("mcp dial host resolved to no addresses")
			}
			var lastErr error
			for _, resolved := range addresses {
				connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
				if dialErr == nil {
					return connection, nil
				}
				lastErr = dialErr
			}
			return nil, fmt.Errorf("dial mcp host: %w", lastErr)
		}
	}
	return &http.Client{Transport: transport}, transport.CloseIdleConnections
}

// isInternalHost reports whether the host is a local/internal address:
// localhost, .internal/.local suffixes and private IP literals.
func isInternalHost(host string) (bool, error) {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return true, nil
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".lan") || strings.HasSuffix(host, ".home") ||
		strings.HasSuffix(host, ".home.arpa") {
		return true, nil
	}
	if ip := net.ParseIP(host); ip != nil {
		return isPrivateIP(ip), nil
	}
	return false, nil
}

func isPrivateIP(ip net.IP) bool {
	return !ip.IsGlobalUnicast() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}
