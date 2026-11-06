package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

func IsRetryableWriteError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(errMsg, "database is locked") ||
		strings.Contains(errMsg, "database table is locked") ||
		strings.Contains(errMsg, "database is busy") ||
		strings.Contains(errMsg, "resource busy") ||
		strings.Contains(errMsg, "timeout")
}

func FirstWhere[T any](ctx context.Context, db *gorm.DB, notFound error, where string, args ...any) (*T, error) {
	var out T
	if err := db.WithContext(ctx).Where(where, args...).First(&out).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) && notFound != nil {
			return nil, notFound
		}
		return nil, fmt.Errorf("failed to query %T: %w", out, err)
	}
	return &out, nil
}
