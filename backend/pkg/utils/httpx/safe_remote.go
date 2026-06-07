package httpx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/common"
)

type LookupIPFunc func(ctx context.Context, host string) ([]net.IP, error)

// blockedRemotePrefixes contains special-use ranges that should never be treated
// as valid public registry destinations. We intentionally keep this list explicit
// because Go exposes helpers for some classes (for example private, loopback, and
// link-local) but does not provide one "publicly routable on the internet" check
// that covers the full SSRF threat model. These prefixes complement the net.IP
// helper checks in isBlockedIPInternal and make the policy reviewable in one place.
var blockedRemotePrefixes = mustParsePrefixesInternal(
	"0.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"::/128",
	"::1/128",
	"2001:db8::/32",
	"fc00::/7",
	"fe80::/10",
	"ff00::/8",
)

func DefaultLookupIP(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

func ValidateSafeRemoteURL(ctx context.Context, rawURL string, lookupIP LookupIPFunc) (*url.URL, error) {
	if lookupIP == nil {
		lookupIP = DefaultLookupIP
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, &common.UnsafeRemoteURLError{Err: err}
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, &common.UnsafeRemoteURLError{Err: fmt.Errorf("scheme %q is not allowed", parsed.Scheme)}
	}

	if parsed.User != nil {
		return nil, &common.UnsafeRemoteURLError{Err: errors.New("embedded credentials are not allowed")}
	}

	host := parsed.Hostname()
	if host == "" || isBlockedHostnameInternal(host) {
		return nil, &common.UnsafeRemoteURLError{Err: fmt.Errorf("host %q is not allowed", host)}
	}

	ips, err := resolveAllowedIPsInternal(ctx, host, lookupIP)
	if err != nil || len(ips) == 0 {
		if err == nil {
			err = errors.New("host did not resolve to an allowed IP")
		}
		return nil, &common.UnsafeRemoteURLError{Err: err}
	}

	return parsed, nil
}

func NewSafeOutboundHTTPClient(base *http.Client, lookupIP LookupIPFunc) (*http.Client, error) {
	if lookupIP == nil {
		lookupIP = DefaultLookupIP
	}
	if base == nil {
		base = http.DefaultClient
	}

	transport, err := cloneHTTPTransportInternal(base.Transport)
	if err != nil {
		return nil, err
	}

	baseDial := transport.DialContext
	if baseDial == nil {
		dialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		baseDial = dialer.DialContext
	}

	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, &common.UnsafeRemoteURLError{Err: err}
		}

		ips, err := resolveAllowedIPsInternal(ctx, host, lookupIP)
		if err != nil {
			return nil, err
		}

		var lastErr error
		for _, ip := range ips {
			conn, dialErr := baseDial(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}

		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("failed to resolve remote host")
	}

	client := *base
	client.Transport = transport
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if _, err := ValidateSafeRemoteURL(req.Context(), req.URL.String(), lookupIP); err != nil {
			return err
		}
		if base.CheckRedirect != nil {
			return base.CheckRedirect(req, via)
		}
		return nil
	}

	return &client, nil
}

func cloneHTTPTransportInternal(base http.RoundTripper) (*http.Transport, error) {
	switch t := base.(type) {
	case nil:
		return http.DefaultTransport.(*http.Transport).Clone(), nil
	case *http.Transport:
		return t.Clone(), nil
	default:
		return nil, fmt.Errorf("unsupported HTTP transport type %T", base)
	}
}

func resolveAllowedIPsInternal(ctx context.Context, host string, lookupIP LookupIPFunc) ([]net.IP, error) {
	if parsedIP := parseIPLiteralInternal(host); parsedIP != nil {
		if isBlockedIPInternal(parsedIP) {
			return nil, &common.UnsafeRemoteURLError{Err: fmt.Errorf("IP %s is not allowed", parsedIP.String())}
		}
		return []net.IP{parsedIP}, nil
	}

	ips, err := lookupIP(ctx, host)
	if err != nil {
		return nil, err
	}

	allowed := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		if isBlockedIPInternal(ip) {
			return nil, &common.UnsafeRemoteURLError{Err: fmt.Errorf("IP %s is not allowed", ip.String())}
		}
		allowed = append(allowed, ip)
	}

	if len(allowed) == 0 {
		return nil, &common.UnsafeRemoteURLError{Err: errors.New("host did not resolve to an allowed IP")}
	}

	return allowed, nil
}

func parseIPLiteralInternal(host string) net.IP {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if zoneIdx := strings.Index(host, "%"); zoneIdx != -1 {
		host = host[:zoneIdx]
	}
	return net.ParseIP(host)
}

func isBlockedHostnameInternal(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

func isBlockedIPInternal(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	// net.IP helper methods do not cover every special-use range we want to keep
	// off-limits for remote registry fetching, so we also check the explicit prefix list above.
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}

	for _, prefix := range blockedRemotePrefixes {
		if prefix.Contains(addr.Unmap()) {
			return true
		}
	}

	return false
}

func mustParsePrefixesInternal(raw ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(raw))
	for _, cidr := range raw {
		prefixes = append(prefixes, netip.MustParsePrefix(cidr))
	}
	return prefixes
}
