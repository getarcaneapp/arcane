//go:build !linux

package startup

import (
	"fmt"
	"runtime"
)

func reexecWithRuntimeIdentityInternal(req runtimeIdentityRequest) error {
	return fmt.Errorf("runtime identity switching is only supported on linux, current platform is %s", runtime.GOOS)
}
