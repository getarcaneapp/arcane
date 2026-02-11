// Package cli provides the root command and entry point for the Arcane CLI.
//
// The Arcane CLI is the official command-line interface for interacting with
// Arcane servers. It provides commands for managing containers, images,
// configuration, and more.
//
// # Getting Started
//
// Configure the CLI with your server URL and API key:
//
//	arcane config set --server-url https://your-server.com --api-key YOUR_API_KEY
//
// # Global Flags
//
// The following flags are available on all commands:
//
//	--log-level string   Log level (debug, info, warn, error, fatal, panic) (default "info")
//	--json               Output in JSON format
//	-v, --version        Print version information
//
// # Command Groups
//
//   - admin: Administration & platform management
//   - auth: Authentication operations
//   - config: Manage CLI configuration
//   - containers: Manage containers
//   - images: Manage Docker images and updates
//   - jobs: Manage background jobs
//   - generate: Generate secrets and tokens
//   - version: Display version information
package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/charmbracelet/fang"
	"github.com/getarcaneapp/arcane/cli/internal/config"
	"github.com/getarcaneapp/arcane/cli/internal/logger"
	"github.com/getarcaneapp/arcane/cli/pkg/admin"
	"github.com/getarcaneapp/arcane/cli/pkg/auth"
	configClient "github.com/getarcaneapp/arcane/cli/pkg/config"
	"github.com/getarcaneapp/arcane/cli/pkg/containers"
	"github.com/getarcaneapp/arcane/cli/pkg/environments"
	"github.com/getarcaneapp/arcane/cli/pkg/generate"
	"github.com/getarcaneapp/arcane/cli/pkg/images"
	"github.com/getarcaneapp/arcane/cli/pkg/jobs"
	"github.com/getarcaneapp/arcane/cli/pkg/networks"
	"github.com/getarcaneapp/arcane/cli/pkg/projects"
	"github.com/getarcaneapp/arcane/cli/pkg/registries"
	"github.com/getarcaneapp/arcane/cli/pkg/settings"
	"github.com/getarcaneapp/arcane/cli/pkg/system"
	"github.com/getarcaneapp/arcane/cli/pkg/templates"
	"github.com/getarcaneapp/arcane/cli/pkg/updater"
	"github.com/getarcaneapp/arcane/cli/pkg/version"
	"github.com/getarcaneapp/arcane/cli/pkg/volumes"
	"github.com/spf13/cobra"
)

var (
	logLevel    string
	jsonOutput  bool
	showVersion bool
	configPath  string
)

var rootCmd = &cobra.Command{
	Use:  "arcane",
	Long: "Arcane CLI - The official command line interface for Arcane",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if configPath != "" {
			if err := config.SetConfigPath(configPath); err != nil {
				return err
			}
		}

		// Load config to check for log level setting
		cfg, _ := config.Load()

		// If flag is not explicitly set, try to use config value
		if !cmd.Flags().Changed("log-level") && cfg != nil && cfg.LogLevel != "" {
			logLevel = cfg.LogLevel
		}

		logger.Setup(logLevel, jsonOutput)
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if showVersion {
			fmt.Printf("Arcane CLI version: %s\n", config.Version)
			fmt.Printf("Git revision: %s\n", config.Revision)
			fmt.Printf("Go version: %s\n", runtime.Version())
			fmt.Printf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			return nil
		}
		return cmd.Help()
	},
	CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
}

func Execute() {
	if err := fang.Execute(context.Background(), rootCmd); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error, fatal, panic)")
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "Path to config file (default ~/.config/arcanecli.yml)")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Log in JSON format")
	rootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "Print version information")

	rootCmd.AddCommand(configClient.ConfigCmd)
	rootCmd.AddCommand(generate.GenerateCmd)
	rootCmd.AddCommand(version.VersionCmd)
	rootCmd.AddCommand(auth.AuthCmd)
	rootCmd.AddCommand(containers.ContainersCmd)
	rootCmd.AddCommand(images.ImagesCmd)
	rootCmd.AddCommand(volumes.VolumesCmd)
	rootCmd.AddCommand(networks.NetworksCmd)
	rootCmd.AddCommand(projects.ProjectsCmd)
	rootCmd.AddCommand(environments.EnvironmentsCmd)
	rootCmd.AddCommand(registries.RegistriesCmd)
	rootCmd.AddCommand(templates.TemplatesCmd)
	rootCmd.AddCommand(settings.SettingsCmd)
	rootCmd.AddCommand(jobs.JobsCmd)
	rootCmd.AddCommand(system.SystemCmd)
	rootCmd.AddCommand(updater.UpdaterCmd)
	rootCmd.AddCommand(admin.AdminCmd)
}
