package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

func initializeDB(ctx context.Context, cfg *config.Config) (*database.DB, error) {
	db, err := database.Initialize(ctx, cfg.DatabaseURL, database.MigrationOptions{
		AllowDowngrade: cfg.AllowDowngrade,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	slog.Info("Database initialized successfully")
	return db, nil
}
