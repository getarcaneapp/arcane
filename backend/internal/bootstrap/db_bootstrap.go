package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/storage"
)

func initializeRuntimeStorage(ctx context.Context, cfg *config.Config) (*storage.RuntimeStorage, error) {
	runtimeStorage, err := storage.InitializeRuntimeStorage(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize runtime storage: %w", err)
	}

	slog.Info("Runtime storage initialized successfully", "backend", runtimeStorage.Backend)
	return runtimeStorage, nil
}
