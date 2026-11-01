//go:build !linux

package startup

import (
	"context"
	"fmt"
	"runtime"
)

var _ = runtimeIdentitySupplementaryGroups

func reexecWithRuntimeIdentity(_ context.Context, req runtimeIdentityRequest) error {
	return fmt.Errorf("runtime identity switching is only supported on linux, current platform is %s", runtime.GOOS)
}
