package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
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
	var entry models.KVEntry
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
	entry := models.KVEntry{Key: key, Value: value}
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
	if err := s.db.DeleteWhere[models.KVEntry](ctx, "key = ?", key); err != nil {
		return fmt.Errorf("failed to delete kv entry %q: %w", key, err)
	}
	return nil
}

func (s *KVService) ListByPrefix(ctx context.Context, prefix string) ([]models.KVEntry, error) {
	escapedPrefix := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix)
	entries, err := s.db.ListWhere[models.KVEntry](ctx, "key LIKE ? ESCAPE '\\'", escapedPrefix+"%")
	if err != nil {
		return nil, fmt.Errorf("failed to list kv entries with prefix %q: %w", prefix, err)
	}
	return entries, nil
}

func (s *KVService) GetBool(ctx context.Context, key string, defaultValue bool) (bool, error) {
	rawValue, ok, err := s.Get(ctx, key)
	if err != nil {
		return defaultValue, err
	}
	if !ok {
		return defaultValue, nil
	}

	parsedValue, err := strconv.ParseBool(rawValue)
	if err != nil {
		return defaultValue, fmt.Errorf("failed to parse kv entry %q as bool: %w", key, err)
	}

	return parsedValue, nil
}

func (s *KVService) SetBool(ctx context.Context, key string, value bool) error {
	return s.Set(ctx, key, strconv.FormatBool(value))
}

func (s *KVService) GetInt64(ctx context.Context, key string, defaultValue int64) (int64, error) {
	rawValue, ok, err := s.Get(ctx, key)
	if err != nil {
		return defaultValue, err
	}
	if !ok {
		return defaultValue, nil
	}

	parsedValue, err := strconv.ParseInt(rawValue, 10, 64)
	if err != nil {
		return defaultValue, fmt.Errorf("failed to parse kv entry %q as int64: %w", key, err)
	}

	return parsedValue, nil
}

func (s *KVService) IncrementInt64(ctx context.Context, key string, delta int64) (int64, error) {
	var nextValue int64
	err := s.db.WithTx(ctx, func(tx *gorm.DB) error {
		var entry models.KVEntry
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("key = ?", key).First(&entry).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			nextValue = delta
			return tx.Create(&models.KVEntry{
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
