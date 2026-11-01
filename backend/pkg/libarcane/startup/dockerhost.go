package startup

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	kit "kit.getarcane.app"
)

func ResolveDockerHost(host string, stat func(string) (fs.FileInfo, error)) string {
	if host != "unix://"+defaultDockerSocketPath {
		return host
	}
	if _, err := stat(defaultDockerSocketPath); err == nil {
		return host
	}

	xdgRuntimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
	for _, candidate := range []string{
		"/run/podman/podman.sock",
		kit.Ternary(xdgRuntimeDir == "", "", filepath.Join(xdgRuntimeDir, "podman", "podman.sock")),
		fmt.Sprintf("/run/user/%d/podman/podman.sock", os.Getuid()),
	} {
		if candidate == "" {
			continue
		}
		if _, err := stat(candidate); err == nil {
			return "unix://" + candidate
		}
	}

	return host
}
