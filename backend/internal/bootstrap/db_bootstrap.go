package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
)

func initializeDBAndMigrate(ctx context.Context, cfg *config.Config) (*database.DB, error) {
	db, err := database.Initialize(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	slog.Info("Database initialized successfully")
	return db, nil
}
