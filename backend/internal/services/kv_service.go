package services

import (
	"context"
	"fmt"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	arcstorage "github.com/getarcaneapp/arcane/backend/internal/storage"
)

// KVService persists lightweight application state in the kv table.
type KVService struct {
	db   *database.DB
	repo arcstorage.KVRepository
}

func NewKVService(db *database.DB, repo ...arcstorage.KVRepository) *KVService {
	var selectedRepo arcstorage.KVRepository
	if len(repo) > 0 && repo[0] != nil {
		selectedRepo = repo[0]
	} else if db != nil {
		selectedRepo = arcstorage.NewSQLKVRepository(db)
	}

	return &KVService{db: db, repo: selectedRepo}
}

func (s *KVService) Get(ctx context.Context, key string) (string, bool, error) {
	entry, err := s.repo.Get(ctx, key)
	if err != nil {
		return "", false, fmt.Errorf("failed to load kv entry %q: %w", key, err)
	}
	if entry == nil {
		return "", false, nil
	}

	return entry.Value, true, nil
}

func (s *KVService) Set(ctx context.Context, key, value string) error {
	if err := s.repo.Set(ctx, key, value); err != nil {
		return fmt.Errorf("failed to upsert kv entry %q: %w", key, err)
	}

	return nil
}
