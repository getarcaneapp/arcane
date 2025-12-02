// FIXME(thaJeztah): remove once we are a module; the go:build directive prevents go from downgrading language version to go1.16:
//go:build go1.23

package registry

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/containerd/log"
	"github.com/distribution/reference"
	"github.com/docker/docker/api/types/registry"
)

// ServiceOptions holds command line options.
//
// TODO(thaJeztah): add CertsDir as option to replace the [CertsDir] function, which sets the location magically.
type ServiceOptions struct {
	InsecureRegistries []string `json:"insecure-registries,omitempty"`
}

// serviceConfig holds daemon configuration for the registry service.
//
// It's a reduced version of [registry.ServiceConfig] for the CLI.
type serviceConfig struct {
	insecureRegistryCIDRs []*net.IPNet
	indexConfigs          map[string]*registry.IndexInfo
}

// TODO(thaJeztah) both the "index.docker.io" and "registry-1.docker.io" domains
// are here for historic reasons and backward-compatibility. These domains
// are still supported by Docker Hub (and will continue to be supported), but
// there are new domains already in use, and plans to consolidate all legacy
// domains to new "canonical" domains. Once those domains are decided on, we
// should update these consts (but making sure to preserve compatibility with
// existing installs, clients, and user configuration).
const (
	// DefaultNamespace is the default namespace
	DefaultNamespace = "docker.io"
	// IndexHostname is the index hostname, used for authentication and image search.
	IndexHostname = "index.docker.io"
	// IndexServer is used for user auth and image search
	IndexServer = "https://index.docker.io/v1/"
	// IndexName is the name of the index
	IndexName = "docker.io"
)

var (
	// DefaultV2Registry is the URI of the default (Docker Hub) registry
	// used for pushing and pulling images. This hostname is hard-coded to handle
	// the conversion from image references without registry name (e.g. "ubuntu",
	// or "ubuntu:latest"), as well as references using the "docker.io" domain
	// name, which is used as canonical reference for images on Docker Hub, but
	// does not match the domain-name of Docker Hub's registry.
	DefaultV2Registry = &url.URL{Scheme: "https", Host: "registry-1.docker.io"}

	validHostPortRegex = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`^` + reference.DomainRegexp.String() + `$`)
	})
)

// runningWithRootlessKit is a fork of [rootless.RunningWithRootlessKit],
// but inlining it to prevent adding that as a dependency for docker/cli.
//
// [rootless.RunningWithRootlessKit]: https://github.com/moby/moby/blob/b4bdf12daec84caaf809a639f923f7370d4926ad/pkg/rootless/rootless.go#L5-L8
func runningWithRootlessKit() bool {
	return runtime.GOOS == "linux" && os.Getenv("ROOTLESSKIT_STATE_DIR") != ""
}

// CertsDir is the directory where certificates are stored.
//
// - Linux: "/etc/docker/certs.d/"
// - Linux (with rootlessKit): $XDG_CONFIG_HOME/docker/certs.d/" or "$HOME/.config/docker/certs.d/"
// - Windows: "%PROGRAMDATA%/docker/certs.d/"
//
// TODO(thaJeztah): certsDir but stored in our config, and passed when needed. For the CLI, we should also default to same path as rootless.
func CertsDir() string {
	certsDir := "/etc/docker/certs.d"
	if runningWithRootlessKit() {
		if configHome, _ := os.UserConfigDir(); configHome != "" {
			certsDir = filepath.Join(configHome, "docker", "certs.d")
		}
	} else if runtime.GOOS == "windows" {
		certsDir = filepath.Join(os.Getenv("programdata"), "docker", "certs.d")
	}
	return certsDir
}

// newServiceConfig creates a new service config with the given options.
func newServiceConfig(registries []string) (*serviceConfig, error) {
	if len(registries) == 0 {
		return &serviceConfig{}, nil
	}
	// Localhost is by default considered as an insecure registry. This is a
	// stop-gap for people who are running a private registry on localhost.
	registries = append(registries, "::1/128", "127.0.0.0/8")

	var (
		insecureRegistryCIDRs = make([]*net.IPNet, 0)
		indexConfigs          = make(map[string]*registry.IndexInfo)
	)

skip:
	for _, r := range registries {
		if scheme, host, ok := strings.Cut(r, "://"); ok {
			switch strings.ToLower(scheme) {
			case "http", "https":
				log.G(context.TODO()).Warnf("insecure registry %[1]s should not contain '%[2]s' and '%[2]ss' has been removed from the insecure registry config", r, scheme)
				r = host
			default:
				// unsupported scheme
				return nil, invalidParam(fmt.Errorf("insecure registry %s should not contain '://'", r))
			}
		}
		// Check if CIDR was passed to --insecure-registry
		_, ipnet, err := net.ParseCIDR(r)
		if err == nil {
			// Valid CIDR. If ipnet is already in config.InsecureRegistryCIDRs, skip.
			for _, value := range insecureRegistryCIDRs {
				if value.IP.String() == ipnet.IP.String() && value.Mask.String() == ipnet.Mask.String() {
					continue skip
				}
			}
			// ipnet is not found, add it in config.InsecureRegistryCIDRs
			insecureRegistryCIDRs = append(insecureRegistryCIDRs, ipnet)
		} else {
			if err := validateHostPort(r); err != nil {
				return nil, invalidParam(fmt.Errorf("insecure registry %s is not valid: %w", r, err))
			}
			// Assume `host:port` if not CIDR.
			indexConfigs[r] = &registry.IndexInfo{
				Name:     r,
				Secure:   false,
				Official: false,
			}
		}
	}

	// Configure public registry.
	indexConfigs[IndexName] = &registry.IndexInfo{
		Name:     IndexName,
		Secure:   true,
		Official: true,
	}

	return &serviceConfig{
		indexConfigs:          indexConfigs,
		insecureRegistryCIDRs: insecureRegistryCIDRs,
	}, nil
}

// isSecureIndex returns false if the provided indexName is part of the list of insecure registries
// Insecure registries accept HTTP and/or accept HTTPS with certificates from unknown CAs.
//
// The list of insecure registries can contain an element with CIDR notation to specify a whole subnet.
// If the subnet contains one of the IPs of the registry specified by indexName, the latter is considered
// insecure.
//
// indexName should be a URL.Host (`host:port` or `host`) where the `host` part can be either a domain name
// or an IP address. If it is a domain name, then it will be resolved in order to check if the IP is contained
// in a subnet. If the resolving is not successful, isSecureIndex will only try to match hostname to any element
// of insecureRegistries.
func (config *serviceConfig) isSecureIndex(indexName string) bool {
	// Check for configured index, first.  This is needed in case isSecureIndex
	// is called from anything besides newIndexInfo, in order to honor per-index configurations.
	if index, ok := config.indexConfigs[indexName]; ok {
		return index.Secure
	}

	return !isCIDRMatch(config.insecureRegistryCIDRs, indexName)
}

// for mocking in unit tests.
var lookupIP = net.LookupIP

// isCIDRMatch returns true if urlHost matches an element of cidrs. urlHost is a URL.Host ("host:port" or "host")
// where the `host` part can be either a domain name or an IP address. If it is a domain name, then it will be
// resolved to IP addresses for matching. If resolution fails, false is returned.
func isCIDRMatch(cidrs []*net.IPNet, urlHost string) bool {
	if len(cidrs) == 0 {
		return false
	}

	host, _, err := net.SplitHostPort(urlHost)
	if err != nil {
		// Assume urlHost is a host without port and go on.
		host = urlHost
	}

	var addresses []net.IP
	if ip := net.ParseIP(host); ip != nil {
		// Host is an IP-address.
		addresses = append(addresses, ip)
	} else {
		// Try to resolve the host's IP-address.
		addresses, err = lookupIP(host)
		if err != nil {
			// We failed to resolve the host; assume there's no match.
			return false
		}
	}

	for _, addr := range addresses {
		for _, ipnet := range cidrs {
			// check if the addr falls in the subnet
			if ipnet.Contains(addr) {
				return true
			}
		}
	}

	return false
}

func normalizeIndexName(val string) string {
	if val == "index.docker.io" {
		return "docker.io"
	}
	return val
}

func validateHostPort(s string) error {
	// Split host and port, and in case s can not be split, assume host only
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		host = s
		port = ""
	}
	// If match against the `host:port` pattern fails,
	// it might be `IPv6:port`, which will be captured by net.ParseIP(host)
	if !validHostPortRegex().MatchString(s) && net.ParseIP(host) == nil {
		return invalidParamf("invalid host %q", host)
	}
	if port != "" {
		v, err := strconv.Atoi(port)
		if err != nil {
			return err
		}
		if v < 0 || v > 65535 {
			return invalidParamf("invalid port %q", port)
		}
	}
	return nil
}

// NewIndexInfo creates a new [registry.IndexInfo] or the given
// repository-name, and detects whether the registry is considered
// "secure" (non-localhost).
func NewIndexInfo(reposName reference.Named) *registry.IndexInfo {
	indexName := normalizeIndexName(reference.Domain(reposName))
	if indexName == IndexName {
		return &registry.IndexInfo{
			Name:     IndexName,
			Secure:   true,
			Official: true,
		}
	}

	return &registry.IndexInfo{
		Name:   indexName,
		Secure: !isInsecure(indexName),
	}
}

// isInsecure is used to detect whether a registry domain or IP-address is allowed
// to use an insecure (non-TLS, or self-signed cert) connection according to the
// defaults, which allows for insecure connections with registries running on a
// loopback address ("localhost", "::1/128", "127.0.0.0/8").
//
// It is used in situations where we don't have access to the daemon's configuration,
// for example, when used from the client / CLI.
func isInsecure(hostNameOrIP string) bool {
	// Attempt to strip port if present; this also strips brackets for
	// IPv6 addresses with a port (e.g. "[::1]:5000").
	//
	// This is best-effort; we'll continue using the address as-is if it fails.
	if host, _, err := net.SplitHostPort(hostNameOrIP); err == nil {
		hostNameOrIP = host
	}
	if hostNameOrIP == "127.0.0.1" || hostNameOrIP == "::1" || strings.EqualFold(hostNameOrIP, "localhost") {
		// Fast path; no need to resolve these, assuming nobody overrides
		// "localhost" for anything else than a loopback address (sorry, not sorry).
		return true
	}

	var addresses []net.IP
	if ip := net.ParseIP(hostNameOrIP); ip != nil {
		addresses = append(addresses, ip)
	} else {
		// Try to resolve the host's IP-addresses.
		addrs, _ := lookupIP(hostNameOrIP)
		addresses = append(addresses, addrs...)
	}

	for _, addr := range addresses {
		if addr.IsLoopback() {
			return true
		}
	}
	return false
}
