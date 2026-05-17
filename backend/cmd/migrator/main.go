package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/getarcaneapp/arcane/backend/internal/migrator"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/signals"
)

func main() {
	ctx := signals.SignalContext(context.Background())
	cmd := migrator.NewCommand(os.Stdout)
	if err := cmd.ExecuteContext(ctx); err != nil {
		slog.Error("Arcane migrator failed", "error", err)
		os.Exit(1)
	}
}
