package desktop

import (
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

const (
	socketName    = "docker-desktop-build.sock"
	socketPath    = ".docker/desktop"
	wslSocketPath = "/mnt/wsl/docker-desktop/shared-sockets/host-services"
)

func BuildServerAddr() (string, error) {
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		socket := filepath.Join(wslSocketPath, socketName)
		if _, err := os.Stat(socket); os.IsNotExist(err) {
			return "", errors.New("Docker Desktop Build backend is not yet supported on WSL. Please run this command on Windows host instead.") //nolint:revive
		}
		return "unix://" + socket, nil
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "failed to get user home directory")
	}
	return "unix://" + filepath.Join(dir, socketPath, socketName), nil
}
