package startup

import (
	"fmt"
	"io/fs"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveDockerHost(t *testing.T) {
	const defaultHost = "unix:///var/run/docker.sock"
	userSocket := fmt.Sprintf("/run/user/%d/podman/podman.sock", os.Getuid())

	tests := []struct {
		name     string
		host     string
		xdg      string
		want     string
		existing []string
	}{
		{
			name:     "custom host is kept",
			host:     "tcp://docker:2375",
			existing: []string{"/run/podman/podman.sock"},
			want:     "tcp://docker:2375",
		},
		{
			name:     "docker socket present keeps default",
			host:     defaultHost,
			existing: []string{"/var/run/docker.sock", "/run/podman/podman.sock"},
			want:     defaultHost,
		},
		{
			name:     "rootful podman socket wins",
			host:     defaultHost,
			xdg:      "/run/user/1000",
			existing: []string{"/run/podman/podman.sock", "/run/user/1000/podman/podman.sock"},
			want:     "unix:///run/podman/podman.sock",
		},
		{
			name:     "xdg runtime dir socket",
			host:     defaultHost,
			xdg:      "/run/user/1000",
			existing: []string{"/run/user/1000/podman/podman.sock", userSocket},
			want:     "unix:///run/user/1000/podman/podman.sock",
		},
		{
			name:     "uid runtime dir socket without xdg",
			host:     defaultHost,
			existing: []string{userSocket},
			want:     "unix://" + userSocket,
		},
		{
			name: "no sockets keeps default",
			host: defaultHost,
			xdg:  "/run/user/1000",
			want: defaultHost,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_RUNTIME_DIR", tt.xdg)
			stat := func(path string) (fs.FileInfo, error) {
				if slices.Contains(tt.existing, path) {
					return nil, nil
				}
				return nil, fs.ErrNotExist
			}

			assert.Equal(t, tt.want, ResolveDockerHost(tt.host, stat))
		})
	}
}
