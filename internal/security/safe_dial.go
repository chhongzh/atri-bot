package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"golang.org/x/net/proxy"
)

var ErrPrivateAddressBlocked = errors.New("network address points to an internal or private network")

type SafeDialer struct {
	base           proxy.ContextDialer
	allowPrivateIP bool
}

type allowPrivateDialContextKey struct{}

func (s *SafeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if s.allowPrivateIP || allowsPrivateDial(ctx) {
		return s.base.DialContext(ctx, network, address)
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parse dial address: %w", err)
	}
	if IsInternalHost(host) {
		return nil, ErrPrivateAddressBlocked
	}
	addresses, err := lookupPublicAddresses(ctx, host, "dial host")
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, resolved := range addresses {
		connection, dialErr := s.base.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		lastErr = dialErr
	}
	return nil, fmt.Errorf("dial host: %w", lastErr)
}

// NewSafeHTTPTransport returns a transport that preserves the configured proxy
// while validating every request target before it is sent.
func NewSafeHTTPTransport(transport *http.Transport, allowPrivateIP bool) http.RoundTripper {
	transport.DialContext = NewSafeDialer(&net.Dialer{}, allowPrivateIP).DialContext
	return &safeHTTPTransport{transport: transport, allowPrivateIP: allowPrivateIP}
}

// DefaultSafeHTTPTransport returns a clone of http.DefaultTransport wrapped with
// the private-network protection policy.
func DefaultSafeHTTPTransport(allowPrivateIP bool) http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return NewSafeHTTPTransport(transport, allowPrivateIP)
}

type safeHTTPTransport struct {
	transport      *http.Transport
	allowPrivateIP bool
}

func (t *safeHTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if !t.allowPrivateIP {
		if err := ValidateHost(request.Context(), request.URL.Hostname()); err != nil {
			return nil, err
		}
	}
	proxyURL, err := t.transport.Proxy(request)
	if err != nil {
		return nil, err
	}
	if proxyURL != nil {
		request = request.Clone(context.WithValue(request.Context(), allowPrivateDialContextKey{}, true))
	}
	return t.transport.RoundTrip(request)
}

func allowsPrivateDial(ctx context.Context) bool {
	allowed, _ := ctx.Value(allowPrivateDialContextKey{}).(bool)
	return allowed
}

// ValidateHost rejects internal hostnames and hosts resolving to private IP addresses.
func ValidateHost(ctx context.Context, host string) error {
	if IsInternalHost(host) {
		return ErrPrivateAddressBlocked
	}
	_, err := lookupPublicAddresses(ctx, host, "host")
	return err
}

func lookupPublicAddresses(ctx context.Context, host, subject string) ([]net.IPAddr, error) {
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", subject, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("%s resolved to no addresses", subject)
	}
	for _, address := range addresses {
		if IsPrivateIP(address.IP) {
			return nil, ErrPrivateAddressBlocked
		}
	}
	return addresses, nil
}

func NewSafeDialer(base proxy.ContextDialer, allowPrivateIP bool) *SafeDialer {
	return &SafeDialer{base, allowPrivateIP}
}

var _ proxy.ContextDialer = &SafeDialer{}

// IsInternalHost reports whether host is a local/internal hostname or private IP literal.
func IsInternalHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".lan") || strings.HasSuffix(host, ".home") ||
		strings.HasSuffix(host, ".home.arpa") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return IsPrivateIP(ip)
	}
	return false
}

// IsPrivateIP reports whether ip must not be reached by an external network client.
func IsPrivateIP(ip net.IP) bool {
	return ip == nil || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}
