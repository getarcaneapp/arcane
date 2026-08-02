//go:build 386 || arm || mips || mipsle || wasm

package startup

import (
	"strconv"

	"emperror.dev/errors"
)

func parseRuntimeIdentityValueInternal(raw string, key string) (int, uint32, error) {
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, 0, errors.WrapIff(err, "parse %s", key)
	}

	if value < 0 {
		return 0, 0, errors.Errorf("%s must be >= 0", key)
	}

	return int(value), uint32(value), nil
}
