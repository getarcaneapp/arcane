package httpx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
)

type LookupIPFunc func(ctx context.Context, host string) ([]net.IP, error)

// blockedRemotePrefixes lists special-use ranges the netip.Addr helpers in resolveAllowedIPs do not cover,
// keeping the SSRF policy reviewable in one place.
var blockedRemotePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

func DefaultLookupIP(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

func ValidateSafeRemoteURL(ctx context.Context, rawURL string, lookupIP LookupIPFunc) (*url.URL, error) {
	parsed, err := ValidateOutboundHTTPURL(rawURL)
	if err != nil {
		return nil, unsafeRemoteURLError(err)
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil, unsafeRemoteURLError(errors.New("missing or blocked hostname"))
	}

	if _, err = resolveAllowedIPs(ctx, parsed.Hostname(), lookupIP); err != nil {
		return nil, err
	}
	return parsed, nil
}

func NewSafeOutboundHTTPClient(base *http.Client, lookupIP LookupIPFunc) (*http.Client, error) {
	if base == nil {
		base = http.DefaultClient
	}

	var transport *http.Transport
	switch t := base.Transport.(type) {
	case nil:
		defaultTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, errors.New("http.DefaultTransport is not *http.Transport")
		}
		transport = defaultTransport.Clone()
	case *http.Transport:
		transport = t.Clone()
	default:
		return nil, fmt.Errorf("unsupported HTTP transport type %T", base.Transport)
	}

	baseDial := transport.DialContext
	if baseDial == nil {
		dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		baseDial = dialer.DialContext
	}

	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, unsafeRemoteURLError(err)
		}

		ips, err := resolveAllowedIPs(ctx, host, lookupIP)
		if err != nil {
			return nil, err
		}

		var dialErr error
		for _, ip := range ips {
			conn, connErr := baseDial(ctx, network, net.JoinHostPort(ip.String(), port))
			if connErr == nil {
				return conn, nil
			}
			dialErr = connErr
		}
		return nil, dialErr
	}

	client := *base
	// Instrumented after the SSRF dialer is installed; the wrapper never dials itself.
	client.Transport = otelhttp.NewTransport(transport)
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

// resolveAllowedIPs resolves host (or parses an IP literal) and rejects it if any address is blocked.
func resolveAllowedIPs(ctx context.Context, host string, lookupIP LookupIPFunc) ([]net.IP, error) {
	literal, _, _ := strings.Cut(host, "%")
	ips := []net.IP{net.ParseIP(literal)}
	if ips[0] == nil {
		if lookupIP == nil {
			lookupIP = DefaultLookupIP
		}
		var err error
		if ips, err = lookupIP(ctx, host); err != nil {
			return nil, unsafeRemoteURLError(err)
		}
	}

	allowed := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		addr, ok := netip.AddrFromSlice(ip)
		addr = addr.Unmap()
		blocked := !ok || addr.IsLoopback() || addr.IsPrivate() || addr.IsMulticast() || addr.IsUnspecified() ||
			addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || slices.ContainsFunc(blockedRemotePrefixes, func(p netip.Prefix) bool { return p.Contains(addr) })
		if blocked {
			return nil, unsafeRemoteURLError(fmt.Errorf("blocked IP address %s", ip))
		}
		allowed = append(allowed, ip)
	}

	if len(allowed) == 0 {
		return nil, unsafeRemoteURLError(errors.New("host did not resolve to an allowed IP"))
	}
	return allowed, nil
}

func unsafeRemoteURLError(err error) error {
	return common.Classify(common.ErrUnsafeRemoteURL, fmt.Errorf("Remote URL is not allowed: %w", err)) //nolint:staticcheck // Preserve the existing error message.
}
