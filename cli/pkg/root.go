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
//	--log-level string   Log level (debug, info, warn, error, fatal) (default "info")
//	--json               Output in JSON format
//	-v, --version        Print version information
//
// # Command Groups
//
//   - admin: Administration & platform management
//   - alerts: Show dashboard alerts
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

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/fang"
	"github.com/fatih/color"
	"github.com/getarcaneapp/arcane/cli/internal/config"
	"github.com/getarcaneapp/arcane/cli/internal/logger"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/runstate"
	runtimectx "github.com/getarcaneapp/arcane/cli/internal/runtime"
	"github.com/getarcaneapp/arcane/cli/pkg/admin"
	"github.com/getarcaneapp/arcane/cli/pkg/alerts"
	"github.com/getarcaneapp/arcane/cli/pkg/auth"
	"github.com/getarcaneapp/arcane/cli/pkg/completion"
	configClient "github.com/getarcaneapp/arcane/cli/pkg/config"
	"github.com/getarcaneapp/arcane/cli/pkg/containers"
	"github.com/getarcaneapp/arcane/cli/pkg/doctor"
	"github.com/getarcaneapp/arcane/cli/pkg/environments"
	"github.com/getarcaneapp/arcane/cli/pkg/generate"
	"github.com/getarcaneapp/arcane/cli/pkg/gitops"
	"github.com/getarcaneapp/arcane/cli/pkg/images"
	"github.com/getarcaneapp/arcane/cli/pkg/jobs"
	"github.com/getarcaneapp/arcane/cli/pkg/networks"
	"github.com/getarcaneapp/arcane/cli/pkg/projects"
	"github.com/getarcaneapp/arcane/cli/pkg/registries"
	"github.com/getarcaneapp/arcane/cli/pkg/repos"
	"github.com/getarcaneapp/arcane/cli/pkg/selfupdate"
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
	Use:  "arcane-cli",
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
	if err := fang.Execute(
		context.Background(),
		rootCmd,
		fang.WithColorSchemeFunc(arcaneFangColorSchemeInternal),
		fang.WithVersion(config.Version),
		fang.WithCommit(config.Revision),
	); err != nil {
		os.Exit(1)
	}
}

// RootCommand returns the configured root command.
// Intended for integration tests and embedding.
func RootCommand() *cobra.Command {
	return rootCmd
}

func arcaneFangColorSchemeInternal(c lipgloss.LightDarkFunc) fang.ColorScheme {
	scheme := fang.DefaultColorScheme(c)
	scheme.Base = c(lipgloss.Color("#1f2937"), lipgloss.Color("#e5e7eb"))
	scheme.Title = c(lipgloss.Color("#6d28d9"), lipgloss.Color("#a78bfa"))
	scheme.Description = scheme.Base
	scheme.Codeblock = c(lipgloss.Color("#f5f3ff"), lipgloss.Color("#241b35"))
	scheme.Program = scheme.Title
	scheme.Command = c(lipgloss.Color("#7c3aed"), lipgloss.Color("#c4b5fd"))
	scheme.DimmedArgument = c(lipgloss.Color("#64748b"), lipgloss.Color("#94a3b8"))
	scheme.Comment = c(lipgloss.Color("#64748b"), lipgloss.Color("#cbd5e1"))
	scheme.Flag = scheme.Title
	scheme.FlagDefault = c(lipgloss.Color("#64748b"), lipgloss.Color("#cbd5e1"))
	scheme.Argument = scheme.Base
	scheme.Help = scheme.Base
	scheme.Dash = scheme.Base
	return scheme
}

func instrumentCommandTreeInternal(cmd *cobra.Command) {
	for _, child := range cmd.Commands() {
		instrumentCommandTreeInternal(child)
	}

	if cmd.RunE == nil && cmd.Run == nil {
		return
	}

	if cmd.Annotations == nil {
		cmd.Annotations = make(map[string]string)
	}
	if cmd.Annotations["arcane:logging-wrapped"] == "true" {
		return
	}
	cmd.Annotations["arcane:logging-wrapped"] = "true"

	action := strings.TrimSpace(cmd.Short)
	if action == "" {
		action = "Running command"
	}

	if cmd.RunE != nil {
		originalRunE := cmd.RunE
		cmd.RunE = func(command *cobra.Command, args []string) error {
			logger.GetLogger().Debug(action, "command", command.CommandPath(), "arg_count", len(args))
			return originalRunE(command, args)
		}
	}

	if cmd.Run != nil {
		originalRun := cmd.Run
		cmd.Run = func(command *cobra.Command, args []string) {
			logger.GetLogger().Debug(action, "command", command.CommandPath(), "arg_count", len(args))
			originalRun(command, args)
		}
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error, fatal)")
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
	rootCmd.AddCommand(alerts.AlertsCmd)
	rootCmd.AddCommand(containers.ContainersCmd)
	rootCmd.AddCommand(images.ImagesCmd)
	rootCmd.AddCommand(volumes.VolumesCmd)
	rootCmd.AddCommand(networks.NetworksCmd)
	rootCmd.AddCommand(projects.ProjectsCmd)
	rootCmd.AddCommand(environments.EnvironmentsCmd)
	rootCmd.AddCommand(registries.RegistriesCmd)
	rootCmd.AddCommand(repos.ReposCmd)
	rootCmd.AddCommand(templates.TemplatesCmd)
	rootCmd.AddCommand(settings.SettingsCmd)
	rootCmd.AddCommand(jobs.JobsCmd)
	rootCmd.AddCommand(system.SystemCmd)
	rootCmd.AddCommand(updater.UpdaterCmd)
	rootCmd.AddCommand(selfupdate.Cmd)
	rootCmd.AddCommand(admin.AdminCmd)
	rootCmd.AddCommand(gitops.GitopsCmd)

	instrumentCommandTreeInternal(rootCmd)
}
