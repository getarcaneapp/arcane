package gitops

import (
	"errors"
	"strings"

	"go.getarcane.app/kit/pkg"
)

func normalizeSyncLimitSetting(value, defaultValue int) int {
	return kit.Ternary(value < 0, defaultValue, value)
}

// nullableTrimmedString trims p and maps an empty or missing value to nil, which stores NULL.
func nullableTrimmedString(p *string) *string {
	trimmed := strings.TrimSpace(kit.FromPtr(p))
	return kit.Ternary(trimmed == "", nil, &trimmed)
}

func validateSyncLimits(maxFiles *int, maxTotalSize, maxBinarySize *int64) error {
	if maxFiles != nil && *maxFiles < 0 {
		return errors.New("maxSyncFiles must be non-negative")
	}
	if maxTotalSize != nil && *maxTotalSize < 0 {
		return errors.New("maxSyncTotalSize must be non-negative")
	}
	if maxBinarySize != nil && *maxBinarySize < 0 {
		return errors.New("maxSyncBinarySize must be non-negative")
	}
	return nil
}
