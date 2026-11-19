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

	"getarcane.dev/arcane/v3/internal/common"
)

type (
	LookupIPFunc func(ctx context.Context, host string) ([]net.IP, error)

	PrivateNetworkClientConfig struct {
		Timeout               time.Duration
		ResponseHeaderTimeout time.Duration
	}

	addressPolicy struct {
		blockedPrefixes []netip.Prefix
		blockedHosts    []string
		allowPrivate    bool
	}
)

var (
	// blockedRemotePrefixes lists special-use ranges the netip.Addr helpers in resolveAllowedIPs do not cover,
	// keeping the SSRF policy reviewable in one place.
	blockedRemotePrefixes = []netip.Prefix{
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

	privateNetworkBlockedPrefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.100.100.200/32"),
		netip.MustParsePrefix("168.63.129.16/32"),
		netip.MustParsePrefix("169.254.0.0/16"),
		netip.MustParsePrefix("240.0.0.0/4"),
		netip.MustParsePrefix("fd00:ec2::254/128"),
		netip.MustParsePrefix("fe80::/10"),
	}

	nat64Prefix     = netip.MustParsePrefix("64:ff9b::/96")
	sixToFourPrefix = netip.MustParsePrefix("2002::/16")

	metadataHostnames = []string{
		"metadata",
		"metadata.goog",
		"metadata.google.internal",
		"instance-data",
		"instance-data.ec2.internal",
	}

	publicOnlyPolicy = addressPolicy{
		blockedPrefixes: blockedRemotePrefixes,
	}

	privateNetworkPolicy = addressPolicy{
		blockedPrefixes: privateNetworkBlockedPrefixes,
		blockedHosts:    metadataHostnames,
		allowPrivate:    true,
	}
)

func ValidateSafeRemoteURL(ctx context.Context, rawURL string, lookupIP LookupIPFunc) (*url.URL, error) {
	parsed, err := ValidateOutboundHTTPURL(rawURL)
	if err != nil {
		return nil, common.Classify(
			common.ErrUnsafeRemoteURL,
			fmt.Errorf("Remote URL is not allowed: %w", err), //nolint:staticcheck // Preserve the existing error message.
		)
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil, common.Classify(
			common.ErrUnsafeRemoteURL,
			fmt.Errorf("Remote URL is not allowed: %w", errors.New("missing or blocked hostname")), //nolint:staticcheck // Preserve the existing error message.
		)
	}

	if _, err = resolveAllowedIPs(ctx, parsed.Hostname(), lookupIP, publicOnlyPolicy); err != nil {
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
		transport = NewTransport(TransportConfig{})
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

	transport.DialContext = guardedDialContext(baseDial, lookupIP, publicOnlyPolicy)

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

func NewPrivateNetworkHTTPClient(cfg PrivateNetworkClientConfig) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Client{
		Transport: otelhttp.NewTransport(NewTransport(TransportConfig{
			DialContext:           guardedDialContext(dialer.DialContext, nil, privateNetworkPolicy),
			ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
			DisableProxy:          true,
		})),
		Timeout:       max(cfg.Timeout, 0),
		CheckRedirect: sameOriginRedirect,
	}
}

func sameOriginRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("stopped after 5 redirects")
	}
	if !strings.EqualFold(req.URL.Host, via[0].URL.Host) || !strings.EqualFold(req.URL.Scheme, via[0].URL.Scheme) {
		return common.Classify(common.ErrUnsafeRemoteURL, fmt.Errorf("redirect to a different host is not allowed: %s", req.URL.Host))
	}
	return nil
}

func guardedDialContext(
	baseDial func(ctx context.Context, network, address string) (net.Conn, error),
	lookupIP LookupIPFunc,
	policy addressPolicy,
) func(ctx context.Context, network, address string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, common.Classify(
				common.ErrUnsafeRemoteURL,
				fmt.Errorf("Remote URL is not allowed: %w", err), //nolint:staticcheck // Preserve the existing error message.
			)
		}

		ips, err := resolveAllowedIPs(ctx, host, lookupIP, policy)
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
}

func resolveAllowedIPs(ctx context.Context, host string, lookupIP LookupIPFunc, policy addressPolicy) ([]net.IP, error) {
	if slices.Contains(policy.blockedHosts, strings.TrimSuffix(strings.ToLower(host), ".")) {
		return nil, common.Classify(common.ErrUnsafeRemoteURL, fmt.Errorf("blocked hostname %s", host))
	}

	literal, _, _ := strings.Cut(host, "%")
	ips := []net.IP{net.ParseIP(literal)}
	if ips[0] == nil {
		if lookupIP == nil {
			lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
				return net.DefaultResolver.LookupIP(ctx, "ip", host)
			}
		}
		var err error
		if ips, err = lookupIP(ctx, host); err != nil {
			return nil, common.Classify(
				common.ErrUnsafeRemoteURL,
				fmt.Errorf("Remote URL is not allowed: %w", err), //nolint:staticcheck // Preserve the existing error message.
			)
		}
	}

	allowed := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		addr, ok := netip.AddrFromSlice(ip)
		addr = addr.Unmap()
		if !ok || policy.blocks(addr) || policy.blocks(embeddedIPv4(addr)) {
			return nil, common.Classify(
				common.ErrUnsafeRemoteURL,
				fmt.Errorf("Remote URL is not allowed: %w", fmt.Errorf("blocked IP address %s", ip)), //nolint:staticcheck // Preserve the existing error message.
			)
		}
		allowed = append(allowed, ip)
	}

	if len(allowed) == 0 {
		return nil, common.Classify(
			common.ErrUnsafeRemoteURL,
			fmt.Errorf("Remote URL is not allowed: %w", errors.New("host did not resolve to an allowed IP")), //nolint:staticcheck // Preserve the existing error message.
		)
	}
	return allowed, nil
}

func (p addressPolicy) blocks(addr netip.Addr) bool {
	return addr.IsMulticast() || addr.IsUnspecified() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		(!p.allowPrivate && (addr.IsLoopback() || addr.IsPrivate())) ||
		slices.ContainsFunc(p.blockedPrefixes, func(prefix netip.Prefix) bool {
			return prefix.Contains(addr)
		})
}

func embeddedIPv4(addr netip.Addr) netip.Addr {
	raw := addr.As16()
	switch {
	case nat64Prefix.Contains(addr):
		return netip.AddrFrom4([4]byte(raw[12:16]))
	case sixToFourPrefix.Contains(addr):
		return netip.AddrFrom4([4]byte(raw[2:6]))
	default:
		return addr
	}
}
