package cli

import (
	"context"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/backend/cli/generate"
	"github.com/getarcaneapp/arcane/backend/cli/upgrade"
	"github.com/getarcaneapp/arcane/backend/internal/bootstrap"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/utils/signals"
)

var rootCmd = &cobra.Command{
	Use:     "arcane",
	Long:    "Arcane - Modern Docker Management, Designed for Everyone.",
	Version: getVersion(),
	Run: func(cmd *cobra.Command, args []string) {
		err := bootstrap.Bootstrap(cmd.Context())
		if err != nil {
			slog.Error("Failed to run Arcane", "error", err)
			os.Exit(1)
		}
	},
	CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
}

func Execute() {
	ctx := signals.SignalContext(context.Background())

	err := rootCmd.ExecuteContext(ctx)
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(upgrade.UpgradeCmd)
	rootCmd.AddCommand(generate.GenerateCmd)
}

func getVersion() string {
	return config.Version
}
