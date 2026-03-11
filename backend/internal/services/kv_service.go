package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
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
