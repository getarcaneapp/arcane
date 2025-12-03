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
//   - config: Manage CLI configuration
//   - images: Manage Docker images
//   - containers: Manage containers
//   - generate: Generate secrets and tokens
//   - version: Display version information
package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/config"
	"go.getarcane.app/cli/internal/logger"
	configClient "go.getarcane.app/cli/pkg/config"
	"go.getarcane.app/cli/pkg/containers"
	"go.getarcane.app/cli/pkg/generate"
	"go.getarcane.app/cli/pkg/images"
	"go.getarcane.app/cli/pkg/version"
)

var (
	logLevel    string
	jsonOutput  bool
	showVersion bool
)

var rootCmd = &cobra.Command{
	Use:  "arcane",
	Long: "Arcane CLI - The official command line interface for Arcane",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Load config to check for log level setting
		cfg, _ := config.Load()

		// If flag is not explicitly set, try to use config value
		if !cmd.Flags().Changed("log-level") && cfg != nil && cfg.LogLevel != "" {
			logLevel = cfg.LogLevel
		}

		logger.Setup(logLevel, jsonOutput)
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
	err := rootCmd.ExecuteContext(context.Background())
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error, fatal, panic)")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Log in JSON format")
	rootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "Print version information")

	rootCmd.AddCommand(configClient.ConfigCmd)
	rootCmd.AddCommand(generate.GenerateCmd)
	rootCmd.AddCommand(version.VersionCmd)
	rootCmd.AddCommand(containers.ContainersCmd)
	rootCmd.AddCommand(images.ImagesCmd)
}
