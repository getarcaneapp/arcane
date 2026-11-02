package cli

import (
	"context"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"kit.getarcane.app/signals"

	"getarcane.dev/arcane/v3/cli/admin"
	"getarcane.dev/arcane/v3/cli/recovery"
	"getarcane.dev/arcane/v3/cli/upgrade"
	"getarcane.dev/arcane/v3/internal/bootstrap"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/cli/v3/pkg/generate"
)

var rootCmd = &cobra.Command{
	Use:     "arcane",
	Long:    "Arcane - Modern Docker Management, Designed for Everyone.",
	Version: config.Version,
	Run: func(cmd *cobra.Command, args []string) {
		err := bootstrap.Bootstrap(cmd.Context())
		if err != nil {
			slog.ErrorContext(cmd.Context(), "Failed to run Arcane", "error", err)
			os.Exit(1)
		}
	},
	CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
}

func init() {
	rootCmd.AddCommand(admin.AdminCmd)
	rootCmd.AddCommand(upgrade.UpgradeCmd)
	rootCmd.AddCommand(recovery.RestoreCmd)
	rootCmd.AddCommand(generate.Command())
}

func Execute() {
	ctx := signals.SignalContext(context.Background()) //nolint:forbidigo // Process entry point creates the signal-controlled application context.

	err := rootCmd.ExecuteContext(ctx)
	if err != nil {
		os.Exit(1)
	}
}
