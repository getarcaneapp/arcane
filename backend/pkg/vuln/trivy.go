package vuln

import (
	"cmp"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"net/url"
	"runtime"
	"strings"

	containertypes "github.com/moby/moby/api/types/container"
	dockerregistry "github.com/moby/moby/api/types/registry"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/pkg/registry"
	"getarcane.dev/arcane/v3/pkg/selfcontainer"
)

const (
	DefaultDockerHostURI = "unix:///var/run/docker.sock"

	DBRepository           = "getarcaneapp/trivy-db:2"
	JavaDBRepository       = "getarcaneapp/trivy-java-db:1"
	ChecksBundleRepository = "getarcaneapp/trivy-checks:1"
)

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

func ParseSecurityOpts(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	value = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(value)
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n'
	})
	opts := kit.TrimNonEmpty(parts)
	return kit.Ternary(len(opts) == 0, nil, opts)
}

func ParseDockerHost(dockerHost string) (scheme, socketPath string, err error) {
	dockerHost = cmp.Or(strings.TrimSpace(dockerHost), DefaultDockerHostURI)

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

func BuildDockerHostEnv(dockerHost string) []string {
	dockerHost = strings.TrimSpace(dockerHost)
	if dockerHost == "" {
		return nil
	}
	return []string{"DOCKER_HOST=" + dockerHost}
}

func ProxyEnv(httpProxy, httpsProxy, noProxy string) []string {
	values := [...][2]string{{"HTTP_PROXY", httpProxy}, {"HTTPS_PROXY", httpsProxy}, {"NO_PROXY", noProxy}}
	env := make([]string, 0, len(values))
	for _, kv := range values {
		if value := strings.TrimSpace(kv[1]); value != "" {
			env = append(env, kv[0]+"="+value)
		}
	}
	return kit.Ternary(len(env) == 0, nil, env)
}

func ScanCacheBackendArgsForArch(arch string) []string {
	switch arch {
	case "arm", "386", "mips", "mipsle":
		return []string{"--cache-backend", "memory"}
	default:
		return nil
	}
}

func RepositoryArgs(registrySetting string) []string {
	host := registry.ArcaneRegistryHost(registrySetting)
	return []string{
		"--db-repository", host + "/" + DBRepository,
		"--java-db-repository", host + "/" + JavaDBRepository,
		"--checks-bundle-repository", host + "/" + ChecksBundleRepository,
	}
}

func ScanSourceArgs(serverURL, registrySetting string) []string {
	serverURL = strings.TrimSpace(serverURL)
	if serverURL == "" {
		return append(ScanCacheBackendArgsForArch(runtime.GOARCH), RepositoryArgs(registrySetting)...)
	}

	return []string{"--server", serverURL}
}

func ServerTokenEnv(token string) []string {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	return []string{"TRIVY_TOKEN=" + token}
}

func IgnoreUnfixedArgs(enabled bool) []string {
	if enabled {
		return []string{"--ignore-unfixed"}
	}
	return nil
}

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

func BuildContainerConfig(scannerImage string, cmdArgs, env []string) *containertypes.Config {
	return &containertypes.Config{
		Image:      scannerImage,
		Entrypoint: []string{"trivy"},
		Cmd:        cmdArgs,
		Env:        append([]string(nil), env...),
		Labels: map[string]string{
			selfcontainer.InternalResourceLabel: "true",
		},
	}
}
