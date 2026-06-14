// Package vuln provides stateless helpers for building and parsing the Trivy
// scanner invocations used by the vulnerability service. Functions here have no
// dependency on database or service state so they can be unit-tested in isolation.
package vuln

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"runtime"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	containertypes "github.com/moby/moby/api/types/container"
	dockerregistry "github.com/moby/moby/api/types/registry"
)

const (
	// DefaultDockerHostURI is the fallback Docker endpoint used when none is configured.
	DefaultDockerHostURI = "unix:///var/run/docker.sock"

	// DefaultDBRepository / DefaultJavaDBRepository / DefaultChecksBundleRepository are the
	// Arcane-mirrored Trivy database locations passed to the scanner.
	DefaultDBRepository           = "ghcr.io/getarcaneapp/trivy-db:2"
	DefaultJavaDBRepository       = "ghcr.io/getarcaneapp/trivy-java-db:1"
	DefaultChecksBundleRepository = "ghcr.io/getarcaneapp/trivy-checks:1"
)

// ParseVersion extracts the version string from `trivy --version` output. It
// returns the value after a "Version:" prefix when present, otherwise the
// trimmed output as-is.
func ParseVersion(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	lines := strings.SplitSeq(output, "\n")
	for line := range lines {
		if after, ok := strings.CutPrefix(line, "Version:"); ok {
			return strings.TrimSpace(after)
		}
	}
	return strings.TrimSpace(output)
}

// NormalizeNetworkMode trims a configured Trivy network mode.
func NormalizeNetworkMode(networkMode string) string {
	return strings.TrimSpace(networkMode)
}

// ParseSecurityOpts splits a comma- or newline-separated list of security options
// into a cleaned slice, returning nil when there are no non-empty entries.
func ParseSecurityOpts(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	value = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(value)
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n'
	})
	if len(parts) == 0 {
		return nil
	}

	opts := make([]string, 0, len(parts))
	for _, part := range parts {
		if opt := strings.TrimSpace(part); opt != "" {
			opts = append(opts, opt)
		}
	}

	if len(opts) == 0 {
		return nil
	}

	return opts
}

// ParseDockerHost validates and decomposes a Docker host URI into its scheme and,
// for unix sockets, the socket path. An empty host falls back to DefaultDockerHostURI.
func ParseDockerHost(dockerHost string) (scheme string, socketPath string, err error) {
	dockerHost = strings.TrimSpace(dockerHost)
	if dockerHost == "" {
		dockerHost = DefaultDockerHostURI
	}

	if strings.HasPrefix(dockerHost, "/") {
		return "unix", dockerHost, nil
	}

	parsed, err := url.Parse(dockerHost)
	if err != nil {
		return "", "", fmt.Errorf("parse docker host %q: %w", dockerHost, err)
	}

	scheme = strings.ToLower(strings.TrimSpace(parsed.Scheme))
	switch scheme {
	case "unix":
		socketPath = strings.TrimSpace(parsed.Path)
		if socketPath == "" {
			return "", "", fmt.Errorf("docker host %q is missing a unix socket path", dockerHost)
		}
		return scheme, socketPath, nil
	case "tcp", "http", "https":
		return scheme, "", nil
	default:
		return "", "", fmt.Errorf("unsupported docker host scheme %q", scheme)
	}
}

// BuildDockerHostEnv returns the DOCKER_HOST environment entry for a non-empty host.
func BuildDockerHostEnv(dockerHost string) []string {
	dockerHost = strings.TrimSpace(dockerHost)
	if dockerHost == "" {
		return nil
	}

	return []string{"DOCKER_HOST=" + dockerHost}
}

// ScanCacheBackendArgsForArch returns the Trivy cache-backend flags for a GOARCH.
// The default BoltDB-backed cache can fail with ENOMEM on arm/v7 and 386 because
// Go's heap reservations fragment the limited 32-bit virtual address space.
func ScanCacheBackendArgsForArch(arch string) []string {
	switch arch {
	case "arm", "386", "mips", "mipsle":
		return []string{"--cache-backend", "memory"}
	default:
		return nil
	}
}

// ScanCacheBackendArgs returns the cache-backend flags for the running architecture.
func ScanCacheBackendArgs() []string {
	return ScanCacheBackendArgsForArch(runtime.GOARCH)
}

// DefaultRepositoryArgs returns the Trivy DB repository flags pointing at the
// Arcane-mirrored databases.
func DefaultRepositoryArgs() []string {
	return []string{
		"--db-repository", DefaultDBRepository,
		"--java-db-repository", DefaultJavaDBRepository,
		"--checks-bundle-repository", DefaultChecksBundleRepository,
	}
}

// BuildDockerConfigJSON encodes registry auth configs into a docker config.json
// payload (base64 user:password under each host). It returns (nil, nil) when there
// are no usable credentials.
func BuildDockerConfigJSON(authConfigs map[string]dockerregistry.AuthConfig) ([]byte, error) {
	if len(authConfigs) == 0 {
		return nil, nil
	}

	type authEntry struct {
		Auth string `json:"auth"`
	}

	auths := make(map[string]authEntry, len(authConfigs))
	for host, cfg := range authConfigs {
		host = strings.TrimSpace(host)
		if host == "" || cfg.Username == "" || cfg.Password == "" {
			continue
		}

		auths[host] = authEntry{
			Auth: base64.StdEncoding.EncodeToString([]byte(cfg.Username + ":" + cfg.Password)),
		}
	}

	if len(auths) == 0 {
		return nil, nil
	}

	return json.Marshal(struct {
		Auths map[string]authEntry `json:"auths"`
	}{Auths: auths})
}

// BuildContainerConfig assembles the container.Config for a Trivy scan container.
func BuildContainerConfig(trivyImage string, cmdArgs []string, env []string) *containertypes.Config {
	return &containertypes.Config{
		Image:      trivyImage,
		Entrypoint: []string{"trivy"},
		Cmd:        cmdArgs,
		Env:        append([]string(nil), env...),
		Labels: map[string]string{
			libarcane.InternalResourceLabel: "true",
		},
	}
}
