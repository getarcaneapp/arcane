package vuln

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	dockerregistry "github.com/moby/moby/api/types/registry"
	"github.com/stretchr/testify/require"
)

func TestParseVersion(t *testing.T) {
	require.Equal(t, "", ParseVersion(""))
	require.Equal(t, "0.50.1", ParseVersion("Version: 0.50.1\nVulnerability DB:\n  Version: 2"))
	require.Equal(t, "raw-output", ParseVersion("raw-output"))
}

func TestBuildDockerConfigJSON(t *testing.T) {
	tests := []struct {
		name      string
		auths     map[string]dockerregistry.AuthConfig
		wantAuths map[string]string
	}{
		{
			name: "nil map",
		},
		{
			name:  "empty map",
			auths: map[string]dockerregistry.AuthConfig{},
		},
		{
			name: "one registry",
			auths: map[string]dockerregistry.AuthConfig{
				"registry.example.com": {Username: "user", Password: "pass"},
			},
			wantAuths: map[string]string{
				"registry.example.com": base64.StdEncoding.EncodeToString([]byte("user:pass")),
			},
		},
		{
			name: "skips blank credentials",
			auths: map[string]dockerregistry.AuthConfig{
				"registry.example.com": {Username: "user", Password: "pass"},
				"blank-user.example":   {Username: "", Password: "pass"},
				"blank-pass.example":   {Username: "user", Password: ""},
				"   ":                  {Username: "user", Password: "pass"},
			},
			wantAuths: map[string]string{
				"registry.example.com": base64.StdEncoding.EncodeToString([]byte("user:pass")),
			},
		},
		{
			name: "two registries",
			auths: map[string]dockerregistry.AuthConfig{
				"registry-a.example.com": {Username: "user-a", Password: "pass-a"},
				"registry-b.example.com": {Username: "user-b", Password: "pass-b"},
			},
			wantAuths: map[string]string{
				"registry-a.example.com": base64.StdEncoding.EncodeToString([]byte("user-a:pass-a")),
				"registry-b.example.com": base64.StdEncoding.EncodeToString([]byte("user-b:pass-b")),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildDockerConfigJSON(tt.auths)
			require.NoError(t, err)
			if len(tt.wantAuths) == 0 {
				require.Nil(t, got)
				return
			}

			var parsed struct {
				Auths map[string]struct {
					Auth string `json:"auth"`
				} `json:"auths"`
			}
			require.NoError(t, json.Unmarshal(got, &parsed))
			require.Len(t, parsed.Auths, len(tt.wantAuths))
			for host, want := range tt.wantAuths {
				require.Equal(t, want, parsed.Auths[host].Auth)
			}
		})
	}
}

func TestScanCacheBackendArgsForArch(t *testing.T) {
	memoryBackend := []string{"--cache-backend", "memory"}

	tests := []struct {
		arch string
		want []string
	}{
		{"arm", memoryBackend},
		{"386", memoryBackend},
		{"mips", memoryBackend},
		{"mipsle", memoryBackend},
		{"amd64", nil},
		{"arm64", nil},
		{"ppc64le", nil},
		{"s390x", nil},
		{"", nil},
	}

	for _, tt := range tests {
		t.Run(tt.arch, func(t *testing.T) {
			got := ScanCacheBackendArgsForArch(tt.arch)
			require.Equal(t, tt.want, got)
			require.NotContains(t, got, "--db-backend")
		})
	}
}

func TestDefaultRepositoryArgs(t *testing.T) {
	args := DefaultRepositoryArgs()
	require.Contains(t, args, "--db-repository")
	require.Contains(t, args, DefaultDBRepository)
	require.Contains(t, args, "--java-db-repository")
	require.Contains(t, args, DefaultJavaDBRepository)
	require.Contains(t, args, "--checks-bundle-repository")
	require.Contains(t, args, DefaultChecksBundleRepository)
}

func TestBuildContainerConfig_IncludesEnv(t *testing.T) {
	config := BuildContainerConfig(
		"ghcr.io/getarcaneapp/tools:latest",
		[]string{"image", "alpine:3.20"},
		[]string{"DOCKER_HOST=tcp://docker-socket-proxy:2375"},
	)

	require.Equal(t, []string{"trivy"}, config.Entrypoint)
	require.Equal(t, []string{"DOCKER_HOST=tcp://docker-socket-proxy:2375"}, config.Env)
}

func TestParseSecurityOpts(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "empty", value: "", want: nil},
		{name: "whitespace only", value: " \n\t ", want: nil},
		{name: "single", value: "label=disable", want: []string{"label=disable"}},
		{name: "comma separated", value: "label=disable,label=type:container_runtime_t", want: []string{"label=disable", "label=type:container_runtime_t"}},
		{name: "newline separated", value: "label=disable\nlabel=type:container_runtime_t", want: []string{"label=disable", "label=type:container_runtime_t"}},
		{name: "mixed whitespace", value: " label=disable,\n\n  label=type:container_runtime_t \r\n privileged=true ", want: []string{"label=disable", "label=type:container_runtime_t", "privileged=true"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ParseSecurityOpts(tt.value))
		})
	}
}

func TestNormalizeNetworkMode(t *testing.T) {
	require.Equal(t, "", NormalizeNetworkMode(""))
	require.Equal(t, "", NormalizeNetworkMode(" \t\n "))
	require.Equal(t, "arcane-external", NormalizeNetworkMode("arcane-external"))
}

func TestParseDockerHost(t *testing.T) {
	tests := []struct {
		name           string
		dockerHost     string
		wantScheme     string
		wantSocketPath string
		wantErr        string
	}{
		{name: "default unix", dockerHost: "", wantScheme: "unix", wantSocketPath: "/var/run/docker.sock"},
		{name: "explicit unix", dockerHost: "unix:///run/user/1000/podman/podman.sock", wantScheme: "unix", wantSocketPath: "/run/user/1000/podman/podman.sock"},
		{name: "tcp host", dockerHost: "tcp://docker-socket-proxy:2375", wantScheme: "tcp"},
		{name: "http host", dockerHost: "http://docker-socket-proxy:2375", wantScheme: "http"},
		{name: "https host", dockerHost: "https://docker.example.com", wantScheme: "https"},
		{name: "unsupported scheme", dockerHost: "ssh://docker.example.com", wantErr: "unsupported docker host scheme"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, socketPath, err := ParseDockerHost(tt.dockerHost)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantScheme, scheme)
			require.Equal(t, tt.wantSocketPath, socketPath)
		})
	}
}
