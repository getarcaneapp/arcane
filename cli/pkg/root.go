// Package cli provides the root command and entry point for the Arcane CLI.
package cli

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"charm.land/fang/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/config"
	"github.com/getarcaneapp/arcane/cli/v3/internal/logger"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	"github.com/getarcaneapp/arcane/cli/v3/internal/runstate"
	runtimectx "github.com/getarcaneapp/arcane/cli/v3/internal/runtime"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/activities"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/admin"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/auth"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/completion"
	configClient "github.com/getarcaneapp/arcane/cli/v3/pkg/config"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/containers"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/doctor"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/environments"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/generate"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/gitops"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/images"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/jobs"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/networks"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/podman"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/projects"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/registries"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/repos"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/selfupdate"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/settings"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/system"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/templates"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/updater"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/variables"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/version"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/volumes"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/vulnerabilities"
	"github.com/getarcaneapp/arcane/cli/v3/pkg/webhooks"
)

type rootOptions struct {
	logLevel, configPath, outputMode, envOverride  string
	logJSON, showVersion, assumeYes, noColor, json bool
	requestTimeout                                 time.Duration
}

// NewRootCommand builds a fresh Arcane CLI command tree.
func NewRootCommand() *cobra.Command {
	opts := &rootOptions{}
	root := &cobra.Command{
		Use:  "arcane-cli",
		Long: "Arcane CLI - The official command line interface for Arcane",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if opts.configPath != "" {
				if err := config.SetConfigPath(opts.configPath); err != nil {
					return err
				}
			}

			jsonMode := opts.json || opts.outputMode == "json"
			runtimeMode := kit.Ternary(jsonMode, runtimectx.OutputModeJSON, runtimectx.OutputModeText)
			output.Configure(cmd.OutOrStdout(), cmd.ErrOrStderr(), jsonMode, opts.noColor, cmd.Short)

			app, err := runtimectx.New(runtimectx.Options{
				EnvOverride:    opts.envOverride,
				OutputMode:     runtimeMode,
				AssumeYes:      opts.assumeYes,
				NoColor:        opts.noColor,
				RequestTimeout: opts.requestTimeout,
			})
			if err != nil {
				return err
			}
			if cfg := app.Config(); !cmd.Flags().Changed("log-level") && cfg != nil && cfg.LogLevel != "" {
				opts.logLevel = cfg.LogLevel
			}
			logger.Setup(cmd.ErrOrStderr(), opts.logLevel, opts.logJSON)
			cmd.SetContext(context.WithValue(cmd.Context(), runtimectx.AppContextKey, app))
			runstate.Set(runstate.State{
				EnvOverride:    opts.envOverride,
				OutputMode:     string(runtimeMode),
				AssumeYes:      opts.assumeYes,
				NoColor:        opts.noColor,
				RequestTimeout: opts.requestTimeout,
			})
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if !opts.showVersion {
				return cmd.Help()
			}
			_, _ = fmt.Fprintf(output.Stdout(), "Arcane CLI version: %s\n", config.Version)
			_, _ = fmt.Fprintf(output.Stdout(), "Git revision: %s\n", config.Revision)
			_, _ = fmt.Fprintf(output.Stdout(), "Go version: %s\n", runtime.Version())
			_, _ = fmt.Fprintf(output.Stdout(), "OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			return nil
		},
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}

	flags := root.PersistentFlags()
	cmdutil.EnumVarP(root, flags, &opts.logLevel, "log-level", "", "info", "Log level", "debug", "info", "warn", "error", "fatal")
	flags.StringVarP(&opts.configPath, "config", "c", "", "Path to config file (default ~/.config/arcanecli.yml)")
	cmdutil.EnumVarP(root, flags, &opts.outputMode, "output", "", "text", "Output mode", "text", "json")
	flags.StringVar(&opts.envOverride, "env", "", "Limit this invocation to one environment ID (default: all environments)")
	flags.BoolVarP(&opts.assumeYes, "yes", "y", false, "Automatic yes to prompts")
	flags.BoolVar(&opts.noColor, "no-color", false, "Disable colored output")
	flags.DurationVar(&opts.requestTimeout, "request-timeout", 0, "HTTP request timeout override (e.g. 30s, 2m)")
	flags.BoolVar(&opts.json, "json", false, "Alias for --output json")
	flags.BoolVar(&opts.logJSON, "log-json", false, "Log in JSON format")
	root.Flags().BoolVarP(&opts.showVersion, "version", "v", false, "Print version information")

	imagesCmd := images.Command()
	imagesCmd.AddCommand(vulnerabilities.Command())
	generateCmd := generate.Command()
	cmdutil.SetScope(cmdutil.ScopeGlobal, generateCmd)
	root.AddCommand(
		configClient.Command(), completion.NewCommand(root), doctor.Command(), generateCmd, version.Command(), auth.Command(),
		containers.Command(), imagesCmd, volumes.Command(), networks.Command(), projects.Command(), environments.Command(),
		registries.Command(), repos.Command(), templates.Command(), settings.Command(), jobs.Command(), system.Command(),
		updater.Command(), selfupdate.Command(), admin.Command(), gitops.Command(), variables.Command(), activities.Command(),
		webhooks.Command(), podman.Command(),
	)
	instrumentCommandTree(root)
	return root
}

func Execute() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt) //nolint:forbidigo // CLI process context.
	// Restore default handling after the first interrupt so a second one exits.
	context.AfterFunc(ctx, cancel)
	if err := fang.Execute(
		ctx,
		NewRootCommand(),
		fang.WithColorSchemeFunc(func(c lipgloss.LightDarkFunc) fang.ColorScheme {
			defaults := fang.DefaultColorScheme(c)
			base := c(lipgloss.Color("#1f2937"), lipgloss.Color("#e5e7eb"))
			title := c(lipgloss.Color("#6d28d9"), lipgloss.Color("#a78bfa"))
			return fang.ColorScheme{
				Base:           base,
				Title:          title,
				Description:    base,
				Codeblock:      c(lipgloss.Color("#f5f3ff"), lipgloss.Color("#241b35")),
				Program:        title,
				Command:        c(lipgloss.Color("#7c3aed"), lipgloss.Color("#c4b5fd")),
				DimmedArgument: c(lipgloss.Color("#64748b"), lipgloss.Color("#94a3b8")),
				Comment:        c(lipgloss.Color("#64748b"), lipgloss.Color("#cbd5e1")),
				Flag:           title,
				FlagDefault:    c(lipgloss.Color("#64748b"), lipgloss.Color("#cbd5e1")),
				Argument:       base,
				Help:           base,
				Dash:           base,
				QuotedString:   defaults.QuotedString,
				ErrorHeader:    defaults.ErrorHeader,
				ErrorDetails:   defaults.ErrorDetails,
			}
		}),
		fang.WithErrorHandler(func(w io.Writer, _ fang.Styles, err error) { output.WriteError(w, err) }),
		fang.WithVersion(config.Version),
		fang.WithCommit(config.Revision),
	); err != nil {
		cancel()
		os.Exit(1)
	}
	cancel()
}

func instrumentCommandTree(cmd *cobra.Command) {
	for _, child := range cmd.Commands() {
		instrumentCommandTree(child)
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

	action := cmp.Or(strings.TrimSpace(cmd.Short), "Running command")

	if cmd.RunE != nil {
		originalRunE := cmd.RunE
		cmd.RunE = func(command *cobra.Command, args []string) error {
			logger.GetLogger().Debug(action, "command", command.CommandPath(), "arg_count", len(args))
			if !command.HasParent() {
				return originalRunE(command, args)
			}
			return cmdutil.RunScoped(command, args, originalRunE)
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
