package kv

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// KVService persists lightweight application state in the kv table.
type KVService struct {
	db *database.DB
}

func NewKVService(db *database.DB) *KVService {
	return &KVService{db: db}
}

func (s *KVService) Get(ctx context.Context, key string) (string, bool, error) {
	var entry KVEntry
	err := s.db.WithContext(ctx).Where("key = ?", key).First(&entry).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to load kv entry %q: %w", key, err)
	}

	return entry.Value, true, nil
}

func (s *KVService) Set(ctx context.Context, key, value string) error {
	entry := KVEntry{Key: key, Value: value}
	err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
		}).
		Create(&entry).Error
	if err != nil {
		return fmt.Errorf("failed to upsert kv entry %q: %w", key, err)
	}

	return nil
}

func (s *KVService) Delete(ctx context.Context, key string) error {
	if err := s.db.WithContext(ctx).Delete(&KVEntry{}, "key = ?", key).Error; err != nil {
		return fmt.Errorf("failed to delete kv entry %q: %w", key, err)
	}
	return nil
}

func (s *KVService) ListByPrefix(ctx context.Context, prefix string) ([]KVEntry, error) {
	var entries []KVEntry
	escapedPrefix := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix)
	if err := s.db.WithContext(ctx).Where("key LIKE ? ESCAPE '\\'", escapedPrefix+"%").Find(&entries).Error; err != nil {
		return nil, fmt.Errorf("failed to list kv entries with prefix %q: %w", prefix, err)
	}
	return entries, nil
}

// GetTyped loads one kv entry and parses it, returning defaultValue when the
// key is absent or unreadable.
func (s *KVService) GetTyped[T any](ctx context.Context, key string, defaultValue T, parse func(string) (T, error)) (T, error) {
	rawValue, ok, err := s.Get(ctx, key)
	if err != nil || !ok {
		return defaultValue, err
	}

	parsedValue, err := parse(rawValue)
	if err != nil {
		return defaultValue, fmt.Errorf("failed to parse kv entry %q as %T: %w", key, defaultValue, err)
	}

	return parsedValue, nil
}

func (s *KVService) GetBool(ctx context.Context, key string, defaultValue bool) (bool, error) {
	return s.GetTyped(ctx, key, defaultValue, strconv.ParseBool)
}

func (s *KVService) SetBool(ctx context.Context, key string, value bool) error {
	return s.Set(ctx, key, strconv.FormatBool(value))
}

func (s *KVService) GetInt64(ctx context.Context, key string, defaultValue int64) (int64, error) {
	return s.GetTyped(ctx, key, defaultValue, func(value string) (int64, error) {
		return strconv.ParseInt(value, 10, 64)
	})
}

func (s *KVService) IncrementInt64(ctx context.Context, key string, delta int64) (int64, error) {
	var nextValue int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var entry KVEntry
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("key = ?", key).First(&entry).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			nextValue = delta
			return tx.Create(&KVEntry{
				Key:   key,
				Value: strconv.FormatInt(nextValue, 10),
			}).Error
		}
		if err != nil {
			return err
		}

		currentValue, parseErr := strconv.ParseInt(entry.Value, 10, 64)
		if parseErr != nil {
			return fmt.Errorf("failed to parse kv entry %q as int64: %w", key, parseErr)
		}

		nextValue = currentValue + delta
		entry.Value = strconv.FormatInt(nextValue, 10)
		return tx.Save(&entry).Error
	})
	if err != nil {
		return 0, fmt.Errorf("failed to increment kv entry %q: %w", key, err)
	}

	return nextValue, nil
}

// CreateIfAbsent atomically creates an entry without replacing an existing value.
func (s *KVService) CreateIfAbsent(ctx context.Context, key, value string) (bool, error) {
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&KVEntry{Key: key, Value: value})
	if err := result.Error; err != nil {
		return result.RowsAffected == 1, fmt.Errorf("create kv entry: %w", err)
	}
	return result.RowsAffected == 1, nil
}

// CompareAndSwap updates only the exact value read by the caller.
func (s *KVService) CompareAndSwap(ctx context.Context, key, previous, next string) (bool, error) {
	result := s.db.WithContext(ctx).Model(&KVEntry{}).Where("key = ? AND value = ?", key, previous).Updates(map[string]any{"value": next, "updated_at": time.Now()})
	if err := result.Error; err != nil {
		return result.RowsAffected == 1, fmt.Errorf("compare and swap kv entry: %w", err)
	}
	return result.RowsAffected == 1, nil
}
