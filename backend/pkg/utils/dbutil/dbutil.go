// Package dbutil provides small generic helpers around GORM that consolidate
// repetitive single-row lookup and transaction boilerplate found across services.
package dbutil

import (
	"context"
	"strings"

	"emperror.dev/errors"

	"gorm.io/gorm"
)

// IsRetryableWriteError reports whether a database write failed because the
// database was temporarily busy or timed out.
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

// FirstWhere fetches a single row of T matching the provided WHERE clause.
// It maps gorm.ErrRecordNotFound to notFound (when non-nil) and otherwise wraps
// the error with a generic "failed to query <T>" prefix.
//
// Example:
//
//	user, err := dbutil.FirstWhere[common.User](ctx, s.db.DB, ErrUserNotFound, "username = ?", username)
func FirstWhere[T any](ctx context.Context, db *gorm.DB, notFound error, where string, args ...any) (*T, error) {
	var out T
	if err := db.WithContext(ctx).Where(where, args...).First(&out).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) && notFound != nil {
			return nil, notFound
		}
		return nil, errors.WrapIff(err, "failed to query %T", out)
	}
	return &out, nil
}

// WithTx runs fn inside a GORM transaction bound to ctx. Equivalent to
// db.WithContext(ctx).Transaction(fn) but tightens the surface area at call sites.
func WithTx(ctx context.Context, db *gorm.DB, fn func(tx *gorm.DB) error) error {
	return db.WithContext(ctx).Transaction(fn)
}
