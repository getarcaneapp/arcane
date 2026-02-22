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
//	arcane config set server-url https://your-server.com api-key YOUR_API_KEY
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
	"strings"
	"time"

	"github.com/charmbracelet/fang"
	"github.com/fatih/color"
	"github.com/getarcaneapp/arcane/cli/internal/config"
	"github.com/getarcaneapp/arcane/cli/internal/logger"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/runstate"
	runtimectx "github.com/getarcaneapp/arcane/cli/internal/runtime"
	"github.com/getarcaneapp/arcane/cli/pkg/admin"
	"github.com/getarcaneapp/arcane/cli/pkg/auth"
	"github.com/getarcaneapp/arcane/cli/pkg/completion"
	configClient "github.com/getarcaneapp/arcane/cli/pkg/config"
	"github.com/getarcaneapp/arcane/cli/pkg/containers"
	"github.com/getarcaneapp/arcane/cli/pkg/doctor"
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
	logLevel       string
	logJSONOutput  bool
	showVersion    bool
	configPath     string
	outputMode     string
	envOverride    string
	assumeYes      bool
	noColorOutput  bool
	requestTimeout time.Duration
	globalJSON     bool
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

		if globalJSON && !cmd.Flags().Changed("output") {
			outputMode = string(runtimectx.OutputModeJSON)
		}
		outputMode = strings.ToLower(strings.TrimSpace(outputMode))
		if outputMode == "" {
			outputMode = string(runtimectx.OutputModeText)
		}
		if outputMode != string(runtimectx.OutputModeText) && outputMode != string(runtimectx.OutputModeJSON) {
			return fmt.Errorf("invalid --output value %q (expected text or json)", outputMode)
		}
		if outputMode == string(runtimectx.OutputModeJSON) {
			if flag := cmd.Flags().Lookup("json"); flag != nil && !flag.Changed {
				_ = cmd.Flags().Set("json", "true")
			}
		}

		// Load config to check for log level setting
		cfg, _ := config.Load()

		// If flag is not explicitly set, try to use config value
		if !cmd.Flags().Changed("log-level") && cfg != nil && cfg.LogLevel != "" {
			logLevel = cfg.LogLevel
		}

		if noColorOutput {
			output.SetColorEnabled(false)
			color.NoColor = true
		} else {
			output.SetColorEnabled(true)
			color.NoColor = false
		}

		logger.Setup(logLevel, logJSONOutput)

		app, err := runtimectx.New(runtimectx.Options{
			EnvOverride:    envOverride,
			OutputMode:     runtimectx.OutputMode(outputMode),
			AssumeYes:      assumeYes,
			NoColor:        noColorOutput,
			RequestTimeout: requestTimeout,
		})
		if err != nil {
			return err
		}
		cmd.SetContext(runtimectx.WithAppContext(cmd.Context(), app))
		runstate.Set(runstate.State{
			EnvOverride:    envOverride,
			OutputMode:     outputMode,
			AssumeYes:      assumeYes,
			NoColor:        noColorOutput,
			RequestTimeout: requestTimeout,
		})
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

// RootCommand returns the configured root command.
// Intended for integration tests and embedding.
func RootCommand() *cobra.Command {
	return rootCmd
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error, fatal, panic)")
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "Path to config file (default ~/.config/arcanecli.yml)")
	rootCmd.PersistentFlags().StringVar(&outputMode, "output", "text", "Output mode (text, json)")
	rootCmd.PersistentFlags().StringVar(&envOverride, "env", "", "Override default environment ID for this invocation")
	rootCmd.PersistentFlags().BoolVarP(&assumeYes, "yes", "y", false, "Automatic yes to prompts")
	rootCmd.PersistentFlags().BoolVar(&noColorOutput, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().DurationVar(&requestTimeout, "request-timeout", 0, "HTTP request timeout override (e.g. 30s, 2m)")
	rootCmd.PersistentFlags().BoolVar(&globalJSON, "json", false, "Alias for --output json")
	rootCmd.PersistentFlags().BoolVar(&logJSONOutput, "log-json", false, "Log in JSON format")
	rootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "Print version information")

	rootCmd.AddCommand(configClient.ConfigCmd)
	rootCmd.AddCommand(completion.NewCommand(rootCmd))
	rootCmd.AddCommand(doctor.DoctorCmd)
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
