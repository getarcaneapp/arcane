package secretsource

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

var (
	// Secret managers usually live on private, VPN, tailnet, or Docker network
	// addresses, so Arcane's SSRF-safe client, which refuses those, cannot be
	// used. This transport still refuses the addresses no secret manager lives
	// on: link-local ranges, which hold cloud metadata services that hand out
	// instance credentials, and the metadata addresses outside them.
	blockedSecretAddresses = []netip.Prefix{
		netip.MustParsePrefix("169.254.0.0/16"),         // IPv4 link-local, including 169.254.169.254
		netip.MustParsePrefix("fe80::/10"),              // IPv6 link-local
		netip.MustParsePrefix("fd00:ec2::254/128"),      // AWS metadata over IPv6
		netip.MustParsePrefix("100.100.100.200/32"),     // Alibaba Cloud metadata
		netip.MustParsePrefix("168.63.129.16/32"),       // Azure wireserver
		netip.MustParsePrefix("fd20:ce::254/128"),       // Google Cloud metadata over IPv6
		netip.MustParsePrefix("64:ff9b::a9fe:a9fe/128"), // 169.254.169.254 through NAT64
	}

	// blockedSecretHosts are metadata names, refused before a proxy is used.
	blockedSecretHosts = map[string]struct{}{
		"metadata.google.internal": {},
		"metadata.goog":            {},
	}

	errBlockedSecretAddress = errors.New("this address is reserved for cloud metadata services and cannot be used as a secret source")
)

// checkSecretAddressInternal refuses blocked destinations. It runs on every
// connection, after DNS, so a name that resolves to a metadata address is
// refused too.
func checkSecretAddressInternal(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("unexpected dial address %q", address)
	}
	return checkSecretIPInternal(ip)
}

func checkSecretIPInternal(ip netip.Addr) error {
	// Contains never matches an address with a zone, so drop it first.
	ip = ip.WithZone("").Unmap()
	for _, prefix := range blockedSecretAddresses {
		if prefix.Contains(ip) {
			return fmt.Errorf("%s: %w", ip, errBlockedSecretAddress)
		}
	}
	return nil
}

// checkSecretHostInternal refuses blocked names and literal addresses in a
// request URL. With an HTTP proxy the dial check only sees the proxy, so the
// target is checked here as well.
func checkSecretHostInternal(host string) error {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if _, blocked := blockedSecretHosts[host]; blocked {
		return fmt.Errorf("%s: %w", host, errBlockedSecretAddress)
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return checkSecretIPInternal(ip)
	}
	return nil
}

// newSecretHTTPClientInternal returns the client providers use.
func newSecretHTTPClientInternal(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	proxy := transport.Proxy
	transport.Proxy = func(req *http.Request) (*url.URL, error) {
		if err := checkSecretHostInternal(req.URL.Hostname()); err != nil {
			return nil, err
		}
		if proxy == nil {
			return nil, nil
		}
		return proxy(req)
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second, Control: checkSecretAddressInternal}
	transport.DialContext = dialer.DialContext
	return &http.Client{Timeout: timeout, Transport: transport}
}
