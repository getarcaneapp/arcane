package bootstrap

import (
	"context"
	"log/slog"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

func initializeDBAndMigrate(ctx context.Context, cfg *config.Config) (*database.DB, error) {
	db, err := database.Initialize(ctx, cfg.DatabaseURL, database.MigrationOptions{
		AllowDowngrade: cfg.AllowDowngrade,
	})
	if err != nil {
		return nil, errors.WrapIf(err, "failed to initialize database")
	}

	slog.Info("Database initialized successfully")
	return db, nil
}
