//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x

package startup

import (
	"fmt"
	"math"
	"strconv"
)

func parseRuntimeIdentityValueInternal(raw string, key string) (int, uint32, error) {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, 0, fmt.Errorf("parse %s: %w", key, err)
	}

	if value < 0 {
		return 0, 0, fmt.Errorf("%s must be >= 0", key)
	}

	if value > math.MaxUint32 {
		return 0, 0, fmt.Errorf("%s value %d exceeds max uint32", key, value)
	}

	return value, uint32(value), nil
}
