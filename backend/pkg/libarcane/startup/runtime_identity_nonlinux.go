//go:build !linux

package startup

import (
	"context"
	"runtime"

	"emperror.dev/errors"
)

var (
	_ = runtimeIdentitySupplementaryGroupsInternal
	_ = dockerSocketPathInternal
)

func reexecWithRuntimeIdentityInternal(_ context.Context, req runtimeIdentityRequest) error {
	return errors.Errorf("runtime identity switching is only supported on linux, current platform is %s", runtime.GOOS)
}
