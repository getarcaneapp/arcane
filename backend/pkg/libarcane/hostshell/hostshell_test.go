package hostshell

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	systemtypes "github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
)

func TestValidateShell_Accepts(t *testing.T) {
	for _, shell := range []string{"/bin/sh", "/bin/bash", "/usr/bin/zsh", "/bin/ash"} {
		got, err := ValidateShell(shell)
		require.NoError(t, err, shell)
		require.Equal(t, shell, got)
	}
}

func TestValidateShell_Rejects(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"bash",               // not absolute
		"/bin/sh -c evil",    // arguments
		"/bin/../etc/passwd", // traversal
		"/bin/sh; rm -rf /",  // shell metacharacters
		"/bin/sh\n",          // trailing control char
		"/" + strings.Repeat("a", maxShellLength),
	}
	for _, shell := range cases {
		_, err := ValidateShell(shell)
		require.Error(t, err, shell)
		require.ErrorIs(t, err, ErrInvalidShell, shell)
	}
}

func TestPreflight(t *testing.T) {
	tests := []struct {
		name    string
		info    systemtypes.Info
		wantErr error
	}{
		{
			name:    "linux ok",
			info:    systemtypes.Info{OSType: "linux", OperatingSystem: "Ubuntu 22.04.3 LTS"},
			wantErr: nil,
		},
		{
			name:    "windows rejected",
			info:    systemtypes.Info{OSType: "windows"},
			wantErr: ErrHostNotLinux,
		},
		{
			name:    "docker desktop rejected",
			info:    systemtypes.Info{OSType: "linux", OperatingSystem: "Docker Desktop"},
			wantErr: ErrDockerDesktop,
		},
		{
			name: "rootless rejected",
			info: systemtypes.Info{
				OSType:          "linux",
				OperatingSystem: "Ubuntu 22.04.3 LTS",
				SecurityOptions: []string{"name=seccomp,profile=default", "name=rootless"},
			},
			wantErr: ErrRootlessDaemon,
		},
		{
			name: "rootful podman ok",
			info: systemtypes.Info{
				OSType:          "linux",
				OperatingSystem: "Fedora Linux",
				SecurityOptions: []string{"name=seccomp,profile=default"},
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Preflight(tt.info)
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestLabels(t *testing.T) {
	labels := Labels("session-123")

	require.Equal(t, "true", labels[libarcane.InternalResourceLabel])
	require.Equal(t, "true", labels[ContainerLabel])
	require.Equal(t, "session-123", labels[SessionLabel])
	require.Len(t, labels, 3)
}

func TestIsHelperContainer(t *testing.T) {
	require.True(t, IsHelperContainer(Labels("x")))
	require.False(t, IsHelperContainer(map[string]string{"other": "true"}))
	require.False(t, IsHelperContainer(nil))
}

func TestNsenterCommand(t *testing.T) {
	require.Equal(
		t,
		[]string{"nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p", "--", "/bin/sh"},
		NsenterCommand("", []string{"/bin/sh"}),
	)
	require.Equal(
		t,
		[]string{"nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p", "-w", "/srv/app", "--", "/bin/sh", "-s"},
		NsenterCommand("/srv/app", []string{"/bin/sh", "-s"}),
	)
}

func TestHostConfig(t *testing.T) {
	hc := HostConfig()

	require.True(t, hc.Privileged)
	require.EqualValues(t, "host", hc.PidMode)
	require.EqualValues(t, "host", hc.NetworkMode)
	require.EqualValues(t, "host", hc.IpcMode)
	require.EqualValues(t, "host", hc.UTSMode)
	require.False(t, hc.AutoRemove)
}

func TestConfig(t *testing.T) {
	cfg := Config("ghcr.io/getarcaneapp/tools:latest", "session-1", []string{"nsenter", "-t", "1"}, []string{"TERM=xterm-256color"}, true)

	require.Equal(t, "ghcr.io/getarcaneapp/tools:latest", cfg.Image)
	require.Empty(t, cfg.Entrypoint)
	require.Equal(t, []string{"nsenter", "-t", "1"}, cfg.Cmd)
	require.Equal(t, []string{"TERM=xterm-256color"}, cfg.Env)
	require.True(t, cfg.Tty)
	require.True(t, cfg.OpenStdin)
	require.True(t, cfg.AttachStdin)
	require.True(t, cfg.AttachStdout)
	require.True(t, cfg.AttachStderr)
	require.Equal(t, "true", cfg.Labels[ContainerLabel])
}

func TestResolveImage_UsesLocalImageWhenPresent(t *testing.T) {
	var pullCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"tools-image"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/images/create"):
			pullCalls.Add(1)
			http.Error(w, "unexpected pull", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	image, err := ResolveImage(context.Background(), newTestDockerClientInternal(t, server), "")

	require.NoError(t, err)
	require.Equal(t, DefaultToolsImage, image)
	require.Zero(t, pullCalls.Load())
}

func TestResolveImage_HonoursOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"custom-image"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	image, err := ResolveImage(context.Background(), newTestDockerClientInternal(t, server), "registry.internal/custom-tools:v1")

	require.NoError(t, err)
	require.Equal(t, "registry.internal/custom-tools:v1", image)
}

func TestResolveImage_PullsWhenMissing(t *testing.T) {
	var pullCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			http.NotFound(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/images/create"):
			pullCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"pulled"}` + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	image, err := ResolveImage(context.Background(), newTestDockerClientInternal(t, server), "")

	require.NoError(t, err)
	require.Equal(t, DefaultToolsImage, image)
	require.EqualValues(t, 1, pullCalls.Load())
}

func TestResolveImage_ReturnsErrImageUnavailableOnPullFailure_NoFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			http.NotFound(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/images/create"):
			http.Error(w, "pull failed", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	// Note: unlike volumehelper.ResolveHelperImage, this must NOT fall back
	// to the Arcane runtime image even if one is discoverable — there is no
	// container list/inspect fallback path here at all.
	image, err := ResolveImage(context.Background(), newTestDockerClientInternal(t, server), "")

	require.Error(t, err)
	require.Empty(t, image)
	require.ErrorIs(t, err, ErrImageUnavailable)
}

func newTestDockerClientInternal(t *testing.T, server *httptest.Server) *client.Client {
	t.Helper()

	dockerClient, err := client.NewClientWithOpts(
		client.WithHost(server.URL),
		client.WithVersion("1.54"),
	)
	require.NoError(t, err)
	return dockerClient
}
