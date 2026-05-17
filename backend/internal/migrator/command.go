package migrator

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/backend/internal/bootstrap"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/startup"
)

func NewCommand(out io.Writer) *cobra.Command {
	if out == nil {
		out = io.Discard
	}

	rootCmd := &cobra.Command{
		Use:           "arcane-migrator",
		Short:         "Manage Arcane database schema migrations",
		Version:       config.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.SetOut(out)

	rootCmd.AddCommand(newStatusCommand(out))
	rootCmd.AddCommand(newDowngradeCommand(out))
	return rootCmd
}

func newStatusCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current Arcane database migration status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(cmd.Context())
			if err != nil {
				return err
			}

			status, err := database.GetMigrationStatus(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}

			knownVersions, err := database.ListAppMigrationVersions()
			if err != nil {
				return err
			}

			printStatus(out, status, knownVersions)
			return nil
		},
	}
}

func newDowngradeCommand(out io.Writer) *cobra.Command {
	var targetAppVersion string

	cmd := &cobra.Command{
		Use:   "downgrade --target <version>",
		Short: "Downgrade the Arcane database schema for a target Arcane version",
		RunE: func(cmd *cobra.Command, args []string) error {
			targetAppVersion = strings.TrimSpace(targetAppVersion)
			if targetAppVersion == "" {
				return fmt.Errorf("--target is required")
			}

			cfg, err := loadConfig(cmd.Context())
			if err != nil {
				return err
			}

			targetMigrationVersion, err := database.ResolveAppMigrationVersion(targetAppVersion)
			if err != nil {
				return err
			}

			status, err := database.GetMigrationStatus(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			if status.Dirty {
				return fmt.Errorf("database schema version %d is dirty; resolve the dirty migration state before downgrading", status.CurrentVersion)
			}
			if targetMigrationVersion > status.LatestVersion {
				return fmt.Errorf("target Arcane version %s requires schema version %d, but this migrator only includes migrations through %d", targetAppVersion, targetMigrationVersion, status.LatestVersion)
			}
			if !status.HasVersion {
				return fmt.Errorf("database has no migration version; start Arcane once to initialize the schema before downgrading")
			}
			if status.CurrentVersion < targetMigrationVersion {
				return fmt.Errorf("database schema version %d is older than target Arcane version %s schema version %d", status.CurrentVersion, targetAppVersion, targetMigrationVersion)
			}
			if status.CurrentVersion == targetMigrationVersion {
				fmt.Fprintf(out, "Database schema is already at version %d for Arcane %s\n", targetMigrationVersion, targetAppVersion)
				return nil
			}

			slog.Warn("Downgrading Arcane database schema",
				"provider", status.Provider,
				"currentVersion", status.CurrentVersion,
				"targetVersion", targetMigrationVersion,
				"targetAppVersion", targetAppVersion,
			)
			if err := database.MigrateToVersion(cmd.Context(), cfg.DatabaseURL, targetMigrationVersion); err != nil {
				return err
			}

			fmt.Fprintf(out, "Database schema downgraded from version %d to %d for Arcane %s\n", status.CurrentVersion, targetMigrationVersion, targetAppVersion)
			return nil
		},
	}

	cmd.Flags().StringVar(&targetAppVersion, "target", "", "Target Arcane app version, for example v1.18.0")
	return cmd
}

func loadConfig(ctx context.Context) (*config.Config, error) {
	_ = godotenv.Load()
	cfg := config.Load()

	runtimeIdentityCfg := &startup.RuntimeIdentityConfig{
		PUID:         cfg.PUID,
		PGID:         cfg.PGID,
		DockerHost:   cfg.DockerHost,
		DockerConfig: cfg.DockerConfig,
		DatabaseURL:  cfg.DatabaseURL,
	}
	if err := startup.ApplyRequestedRuntimeIdentity(ctx, runtimeIdentityCfg); err != nil {
		return nil, fmt.Errorf("apply runtime identity: %w", err)
	}
	cfg.DockerConfig = runtimeIdentityCfg.DockerConfig

	bootstrap.SetupSlogLogger(cfg)
	bootstrap.ConfigureGormLogger(cfg)
	return cfg, nil
}

func printStatus(out io.Writer, status *database.MigrationStatus, knownVersions []database.AppMigrationVersion) {
	fmt.Fprintf(out, "Provider: %s\n", status.Provider)
	if status.HasVersion {
		fmt.Fprintf(out, "Current schema version: %d\n", status.CurrentVersion)
	} else {
		fmt.Fprintln(out, "Current schema version: none")
	}
	fmt.Fprintf(out, "Latest embedded schema version: %d\n", status.LatestVersion)
	fmt.Fprintf(out, "Dirty: %t\n", status.Dirty)
	fmt.Fprintln(out, "Known app-version targets:")
	for _, version := range knownVersions {
		fmt.Fprintf(out, "  %s -> schema %d\n", version.AppVersion, version.MigrationVersion)
	}
}
