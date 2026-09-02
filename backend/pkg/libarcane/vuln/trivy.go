// Package vuln drives Trivy scans: building and parsing scanner invocations,
// plus the container-level mechanics of running one — runtime/network
// detection, container spec and resources, temp files, log copy and tar
// extraction, and report decoding. Nothing here depends on Arcane's database or
// service graph, so it is unit-testable in isolation.
package vuln

import (
	"encoding/base64"
	"encoding/json/v2"
	"net/url"
	"runtime"
	"strings"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	containertypes "github.com/moby/moby/api/types/container"
	dockerregistry "github.com/moby/moby/api/types/registry"
)

const (
	// DefaultDockerHostURI is the fallback Docker endpoint used when none is configured.
	DefaultDockerHostURI = "unix:///var/run/docker.sock"

	// DBRepository / JavaDBRepository / ChecksBundleRepository are the Arcane-mirrored Trivy database paths without a registry host.
	DBRepository           = "getarcaneapp/trivy-db:2"
	JavaDBRepository       = "getarcaneapp/trivy-java-db:1"
	ChecksBundleRepository = "getarcaneapp/trivy-checks:1"
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
		return "", "", errors.WrapIff(err, "parse docker host %q", dockerHost)
	}

	scheme = strings.ToLower(strings.TrimSpace(parsed.Scheme))
	switch scheme {
	case "unix":
		socketPath = strings.TrimSpace(parsed.Path)
		if socketPath == "" {
			return "", "", errors.Errorf("docker host %q is missing a unix socket path", dockerHost)
		}
		return scheme, socketPath, nil
	case "tcp", "http", "https":
		return scheme, "", nil
	default:
		return "", "", errors.Errorf("unsupported docker host scheme %q", scheme)
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

// RepositoryArgs returns the Trivy DB repository flags pointing at the Arcane-mirrored databases on the given registry.
func RepositoryArgs(registry string) []string {
	host := libarcane.ArcaneRegistryHost(registry)
	return []string{
		"--db-repository", host + "/" + DBRepository,
		"--java-db-repository", host + "/" + JavaDBRepository,
		"--checks-bundle-repository", host + "/" + ChecksBundleRepository,
	}
}

// ScanSourceArgs returns the Trivy flags selecting where vulnerability data comes
// from. In client/server mode (serverURL set) the client never opens the local
// BoltDB, so --server replaces the cache-backend and DB-repository flags. This is
// the supported path on 32-bit hosts (arm/v7) where the local DB cannot be
// memory-mapped. With an empty serverURL it falls back to the standalone
// local-database flags. The server token is passed via ServerTokenEnv rather than
// a flag so it is not exposed in the process arguments.
func ScanSourceArgs(serverURL string, registry string) []string {
	serverURL = strings.TrimSpace(serverURL)
	if serverURL == "" {
		return append(ScanCacheBackendArgs(), RepositoryArgs(registry)...)
	}

	return []string{"--server", serverURL}
}

// ServerTokenEnv returns the TRIVY_TOKEN environment entry for a non-empty Trivy
// server token. Passing the token through the environment instead of a --token CLI
// flag keeps it out of the container's process arguments (/proc/<pid>/cmdline, ps).
func ServerTokenEnv(token string) []string {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}

	return []string{"TRIVY_TOKEN=" + token}
}

// IgnoreUnfixedArgs returns --ignore-unfixed when enabled so Trivy only reports
// vulnerabilities that have a known fix.
func IgnoreUnfixedArgs(enabled bool) []string {
	if enabled {
		return []string{"--ignore-unfixed"}
	}
	return nil
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
func BuildContainerConfig(scannerImage string, cmdArgs []string, env []string) *containertypes.Config {
	return &containertypes.Config{
		Image:      scannerImage,
		Entrypoint: []string{"trivy"},
		Cmd:        cmdArgs,
		Env:        append([]string(nil), env...),
		Labels: map[string]string{
			libarcane.InternalResourceLabel: "true",
		},
	}
}
