package desktop

import (
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

const (
	socketName = "docker-desktop-build.sock"
	socketPath = "Library/Containers/com.docker.docker/Data"
)

func BuildServerAddr() (string, error) {
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "failed to get user home directory")
	}
	return "unix://" + filepath.Join(dir, socketPath, socketName), nil
}
