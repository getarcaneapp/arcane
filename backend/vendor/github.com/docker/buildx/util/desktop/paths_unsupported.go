//go:build !windows && !darwin && !linux

package desktop

import (
	"runtime"

	"github.com/pkg/errors"
)

func BuildServerAddr() (string, error) {
	return "", errors.Errorf("Docker Desktop unsupported on %s", runtime.GOOS)
}
