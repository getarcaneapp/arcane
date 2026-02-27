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
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"charm.land/lipgloss/v2/table"
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
	"github.com/spf13/pflag"
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

var (
	helpPurple = compat.AdaptiveColor{
		Light: lipgloss.Color("#6d28d9"),
		Dark:  lipgloss.Color("#a78bfa"),
	}
	helpTextPrimary = compat.AdaptiveColor{
		Light: lipgloss.Color("#1f2937"),
		Dark:  lipgloss.Color("#e5e7eb"),
	}
	helpTextMuted = compat.AdaptiveColor{
		Light: lipgloss.Color("#64748b"),
		Dark:  lipgloss.Color("#cbd5e1"),
	}

	helpTitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(helpPurple).Padding(0, 1)
	helpSubtitleStyle = lipgloss.NewStyle().Foreground(helpTextMuted).PaddingLeft(1)
	helpVersionStyle  = lipgloss.NewStyle().Foreground(helpTextMuted).PaddingLeft(1)
	helpSectionStyle  = lipgloss.NewStyle().Bold(true).Foreground(helpPurple).Padding(0, 1)

	helpCmdHeader = lipgloss.NewStyle().Bold(true).Foreground(helpPurple).Align(lipgloss.Center).Padding(0, 2)
	helpCmdCell   = lipgloss.NewStyle().Padding(0, 2)
	helpCmdOdd    = helpCmdCell.Foreground(helpTextPrimary)
	helpCmdEven   = helpCmdCell.Foreground(helpTextMuted)
	helpCmdBorder = lipgloss.NewStyle().Foreground(helpPurple)

	helpTableWrap   = lipgloss.NewStyle().Padding(0, 1)
	helpBodyStyle   = lipgloss.NewStyle().Foreground(helpTextPrimary).PaddingLeft(2)
	helpFooterStyle = lipgloss.NewStyle().Foreground(helpTextMuted).Padding(1, 1, 0, 1)
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
	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		os.Exit(1)
	}
}

// RootCommand returns the configured root command.
// Intended for integration tests and embedding.
func RootCommand() *cobra.Command {
	return rootCmd
}

func init() {
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		renderCommandHelp(cmd)
	})

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
	rootCmd.AddCommand(alerts.AlertsCmd)
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

func renderCommandHelp(cmd *cobra.Command) {
	title := cmd.CommandPath()
	if cmd == rootCmd {
		title = "Arcane CLI"
	}
	lipgloss.Println(helpTitleStyle.Render(title))

	summary := strings.TrimSpace(cmd.Long)
	if summary == "" {
		summary = strings.TrimSpace(cmd.Short)
	}
	if summary != "" {
		lipgloss.Println(helpSubtitleStyle.Render(summary))
	}

	if cmd == rootCmd {
		lipgloss.Println(helpVersionStyle.Render(fmt.Sprintf("Version %s (%s)", config.Version, config.Revision)))
	}
	lipgloss.Println()

	usageLine := cmd.UseLine()
	if usageLine != "" {
		lipgloss.Println(helpSectionStyle.Render("Usage"))
		lipgloss.Println(helpBodyStyle.Render(usageLine))
		lipgloss.Println()
	}

	if len(cmd.Aliases) > 0 {
		lipgloss.Println(helpSectionStyle.Render("Aliases"))
		lipgloss.Println(helpBodyStyle.Render(strings.Join(cmd.Aliases, ", ")))
		lipgloss.Println()
	}

	commandRows := collectCommandRows(cmd)
	if len(commandRows) > 0 {
		lipgloss.Println(helpSectionStyle.Render("Commands"))
		renderHelpTable([]string{"COMMAND", "DESCRIPTION"}, commandRows)
		lipgloss.Println()
	}

	localFlagRows := collectFlagRows(cmd.NonInheritedFlags())
	if len(localFlagRows) > 0 {
		lipgloss.Println(helpSectionStyle.Render("Flags"))
		renderHelpTable([]string{"FLAG", "TYPE", "DEFAULT", "DESCRIPTION"}, localFlagRows)
		lipgloss.Println()
	}

	inheritedFlagRows := collectFlagRows(cmd.InheritedFlags())
	if len(inheritedFlagRows) > 0 {
		lipgloss.Println(helpSectionStyle.Render("Global Flags"))
		renderHelpTable([]string{"FLAG", "TYPE", "DEFAULT", "DESCRIPTION"}, inheritedFlagRows)
		lipgloss.Println()
	}

	examples := strings.TrimSpace(cmd.Example)
	if examples != "" {
		lipgloss.Println(helpSectionStyle.Render("Examples"))
		for _, line := range strings.Split(examples, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				lipgloss.Println()
				continue
			}
			lipgloss.Println(helpBodyStyle.Render(trimmed))
		}
		lipgloss.Println()
	}

	if cmd.HasAvailableSubCommands() {
		lipgloss.Println(helpFooterStyle.Render(fmt.Sprintf("Run '%s [command] --help' for command-specific help.", cmd.CommandPath())))
	}
}

func collectCommandRows(cmd *cobra.Command) [][]string {
	rows := make([][]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		if !sub.IsAvailableCommand() || sub.Hidden || sub.IsAdditionalHelpTopicCommand() {
			continue
		}
		rows = append(rows, []string{sub.Name(), sub.Short})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i][0] < rows[j][0]
	})

	return rows
}

func collectFlagRows(flags *pflag.FlagSet) [][]string {
	if flags == nil {
		return nil
	}

	rows := make([][]string, 0)
	flags.VisitAll(func(f *pflag.Flag) {
		if f == nil || f.Hidden {
			return
		}

		name := "--" + f.Name
		if f.Shorthand != "" && f.ShorthandDeprecated == "" {
			name = fmt.Sprintf("-%s, --%s", f.Shorthand, f.Name)
		}

		defaultValue := strings.TrimSpace(f.DefValue)
		if defaultValue == "" {
			defaultValue = "—"
		}

		flagType := "value"
		if f.Value != nil {
			if t := strings.TrimSpace(f.Value.Type()); t != "" {
				flagType = t
			}
		}

		rows = append(rows, []string{name, flagType, defaultValue, f.Usage})
	})

	sort.Slice(rows, func(i, j int) bool {
		return rows[i][0] < rows[j][0]
	})

	return rows
}

func renderHelpTable(headers []string, rows [][]string) {
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(helpCmdBorder).
		StyleFunc(func(row, col int) lipgloss.Style {
			switch {
			case row == table.HeaderRow:
				return helpCmdHeader
			case row%2 == 0:
				return helpCmdEven
			default:
				return helpCmdOdd
			}
		}).
		Headers(headers...)

	if len(rows) > 0 {
		t = t.Rows(rows...)
	}

	lipgloss.Println(helpTableWrap.Render(fmt.Sprint(t)))
}
