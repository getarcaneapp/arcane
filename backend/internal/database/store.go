package database

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// First fetches a single row of M matching the provided WHERE clause.
// It maps gorm.ErrRecordNotFound to notFound (when non-nil) and otherwise wraps
// the error with a generic "failed to query <M>" prefix.
//
// Example:
//
//	user, err := s.db.First[models.User](ctx, ErrUserNotFound, "username = ?", username)
func (db *DB) First[M any](ctx context.Context, notFound error, where string, args ...any) (*M, error) {
	var out M
	if err := db.WithContext(ctx).Where(where, args...).First(&out).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) && notFound != nil {
			return nil, notFound
		}
		return nil, fmt.Errorf("failed to query %T: %w", out, err)
	}
	return &out, nil
}

// ListWhere fetches all rows of M matching the provided WHERE clause; an empty
// where clause lists every row.
func (db *DB) ListWhere[M any](ctx context.Context, where string, args ...any) ([]M, error) {
	var out []M
	query := db.WithContext(ctx)
	if where != "" {
		query = query.Where(where, args...)
	}
	if err := query.Find(&out).Error; err != nil {
		return nil, fmt.Errorf("failed to list %T: %w", out, err)
	}
	return out, nil
}

// Create inserts m.
func (db *DB) Create[M any](ctx context.Context, m *M) error {
	if err := db.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("failed to create %T: %w", m, err)
	}
	return nil
}

// Save upserts m (all fields).
func (db *DB) Save[M any](ctx context.Context, m *M) error {
	if err := db.WithContext(ctx).Save(m).Error; err != nil {
		return fmt.Errorf("failed to save %T: %w", m, err)
	}
	return nil
}

// DeleteWhere deletes all rows of M matching the provided WHERE clause.
func (db *DB) DeleteWhere[M any](ctx context.Context, where string, args ...any) error {
	var m M
	if err := db.WithContext(ctx).Where(where, args...).Delete(&m).Error; err != nil {
		return fmt.Errorf("failed to delete %T: %w", m, err)
	}
	return nil
}

// Count counts rows of M matching the provided WHERE clause; an empty where
// clause counts every row.
func (db *DB) Count[M any](ctx context.Context, where string, args ...any) (int64, error) {
	var m M
	var count int64
	query := db.WithContext(ctx).Model(&m)
	if where != "" {
		query = query.Where(where, args...)
	}
	if err := query.Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count %T: %w", m, err)
	}
	return count, nil
}

// Exists reports whether any row of M matches the provided WHERE clause.
func (db *DB) Exists[M any](ctx context.Context, where string, args ...any) (bool, error) {
	count, err := db.Count[M](ctx, where, args...)
	return count > 0, err
}

// WithTx runs fn inside a GORM transaction bound to ctx.
func (db *DB) WithTx(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return db.WithContext(ctx).Transaction(fn)
}
