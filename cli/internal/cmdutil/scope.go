package cmdutil

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/settings"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v2/internal/runtime"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

const (
	ScopeRead        = "read"
	ScopeMerged      = "merged"
	ScopeFleet       = "fleet"
	ScopeDestination = "destination"
	ScopeResource    = "resource"
	ScopeGlobal      = "global"
)

type mergedScopeKey struct{}

type mergedScope struct {
	names    map[string]string
	failures []base.EnvironmentFailure
}

type environmentResult struct {
	EnvironmentID   string         `json:"environmentId"`
	EnvironmentName string         `json:"environmentName"`
	Result          jsontext.Value `json:"result,omitempty"`
	Error           string         `json:"error,omitempty"`
}

// SetResource binds raw identifiers through a command-owned closure; call after assigning RunE.
func SetResource[D, S any](ref ResourceRef[D, S], commands ...*cobra.Command) {
	SetScope(ScopeResource, commands...)
	for _, cmd := range commands {
		if cmd.Annotations["arcane:resource-wrapped"] == "true" {
			continue
		}
		cmd.Annotations["arcane:resource-wrapped"] = "true"
		run := cmd.RunE
		cmd.RunE = func(command *cobra.Command, args []string) error {
			app, _ := runtime.From(command.Context())
			if !app.PreviewEnabled() {
				return run(command, args)
			}
			defer app.SelectEnvironment("")
			c, err := ClientFromCommand(command)
			if err != nil {
				return err
			}
			resolved, _, resolveErr := ref.Resolve(command.Context(), c, args[0], false)
			if resolveErr != nil {
				return resolveErr
			}
			resolvedArgs := slices.Clone(args)
			if ref.IdentifierOf != nil {
				resolvedArgs[0] = ref.IdentifierOf(*resolved)
			}
			return run(command, resolvedArgs)
		}
	}
}

// SetScope declares how commands behave when all-environments preview is enabled.
func SetScope(scope string, commands ...*cobra.Command) {
	for _, cmd := range commands {
		if cmd.Annotations == nil {
			cmd.Annotations = make(map[string]string)
		}
		cmd.Annotations["arcane:scope"] = scope
	}
}

// RunScoped reuses the command's requests and rendering for each selected environment.
func RunScoped(cmd *cobra.Command, args []string, run func(*cobra.Command, []string) error) error {
	app, ok := runtime.From(cmd.Context())
	if !ok || !app.PreviewEnabled() {
		return run(cmd, args)
	}
	var scope string
	for current := cmd; current != nil; current = current.Parent() {
		if scope = current.Annotations["arcane:scope"]; scope != "" {
			break
		}
	}
	if flag := cmd.Annotations["arcane:scope-flag"]; flag != "" {
		enabled, _ := cmd.Flags().GetBool(flag)
		if !enabled {
			return run(cmd, args)
		}
	}
	if scope == "" || scope == ScopeGlobal {
		return run(cmd, args)
	}
	if scope == ScopeResource {
		return run(cmd, args)
	}
	if scope == ScopeMerged {
		return RunMerged(cmd, args, run)
	}
	if scope != ScopeDestination {
		return RunEnvironments(cmd, args, run, scope == ScopeFleet)
	}
	environments, err := app.Environments(cmd.Context())
	if err != nil {
		return err
	}
	if len(environments) == 0 {
		return errors.New("no enabled environments available")
	}
	if JSONOutputEnabled(cmd) || !prompt.IsInteractive() {
		return errors.New("an environment destination is required; pass --env <id>")
	}
	options := make([]string, len(environments))
	for i, env := range environments {
		options[i] = fmt.Sprintf("%s (%s)", cmp.Or(env.Name, env.ID), env.ID)
	}
	choice, selectErr := prompt.Select("environment", options)
	if selectErr != nil {
		return selectErr
	}
	app.SelectEnvironment(environments[choice].ID)
	defer app.SelectEnvironment("")
	return run(cmd, args)
}

func RunMerged(cmd *cobra.Command, args []string, run func(*cobra.Command, []string) error) error {
	app, _ := runtime.From(cmd.Context())
	environments, discoveryErr := app.Environments(cmd.Context())
	if discoveryErr != nil {
		return discoveryErr
	}
	if len(environments) == 0 {
		return RunEnvironments(cmd, args, run, false)
	}
	limit := 64
	// The limit is at least 2, so only more environments need it.
	if len(environments) > 2 {
		c, clientErr := ClientFromCommand(cmd)
		if clientErr != nil {
			return clientErr
		}
		publicSettings, settingsErr := c.DoJSON[[]settings.PublicSetting](cmd.Context(), http.MethodGet, types.SettingsPublic("0"), nil)
		if settingsErr != nil {
			return fmt.Errorf("failed to read the environment limit: %w", settingsErr)
		}
		for _, setting := range publicSettings {
			if parsed, parseErr := strconv.Atoi(setting.Value); setting.Key == "allEnvironmentsLimit" && parseErr == nil && parsed > 0 {
				limit = parsed
			}
		}
	}
	merged := &mergedScope{names: make(map[string]string, len(environments))}
	ids := make([]string, 0, min(limit, len(environments)))
	for i, env := range environments {
		merged.names[env.ID] = cmp.Or(env.Name, env.ID)
		if i < limit {
			ids = append(ids, env.ID)
			continue
		}
		merged.failures = append(merged.failures, base.EnvironmentFailure{
			Environment: base.EnvironmentRef{ID: env.ID, Name: merged.names[env.ID]},
			Message:     fmt.Sprintf("not included: more than %d environments are shown at once", limit),
		})
	}
	commandContext := cmd.Context()
	cmd.SetContext(context.WithValue(commandContext, mergedScopeKey{}, merged))
	defer cmd.SetContext(commandContext)
	app.SelectEnvironment(strings.Join(ids, ","))
	defer app.SelectEnvironment("")
	if runErr := run(cmd, args); runErr != nil {
		return runErr
	}
	failures := make([]error, len(merged.failures))
	for i, failure := range merged.failures {
		failures[i] = fmt.Errorf("%s (%s): %s", cmp.Or(merged.names[failure.Environment.ID], failure.Environment.Name), failure.Environment.ID, failure.Message)
		if !JSONOutputEnabled(cmd) {
			_, _ = fmt.Fprintln(output.Stderr(), failures[i])
		}
	}
	return errors.Join(failures...)
}

// RunEnvironments executes a command sequentially and reports each environment's result.
func RunEnvironments(cmd *cobra.Command, args []string, run func(*cobra.Command, []string) error, mutation bool) error {
	app, _ := runtime.From(cmd.Context())
	if !app.PreviewEnabled() {
		return run(cmd, args)
	}
	environments, discoveryErr := app.Environments(cmd.Context())
	if discoveryErr != nil {
		return discoveryErr
	}
	if mutation && len(environments) == 0 {
		return errors.New("no enabled environments available")
	}
	skip, _ := cmd.Flags().GetBool("force")
	if mutation && !skip {
		labels := make([]string, len(environments))
		for i, env := range environments {
			labels[i] = cmp.Or(env.Name, env.ID)
		}
		confirmed, confirmErr := Confirm(cmd, fmt.Sprintf("%s across %d environments: %s?", cmd.Short, len(labels), strings.Join(labels, ", ")))
		if confirmErr != nil {
			return confirmErr
		}
		if !confirmed {
			return nil
		}
	}
	if mutation {
		app.FleetConfirmed = true
		defer func() { app.FleetConfirmed = false }()
	}
	jsonMode := JSONOutputEnabled(cmd)
	combined := &tableOutput{}
	commandContext := cmd.Context()
	if !jsonMode {
		cmd.SetContext(context.WithValue(commandContext, tableContextKey{}, combined))
		defer cmd.SetContext(commandContext)
	}
	stdout, stderr := output.Stdout(), output.Stderr()
	defer output.Configure(stdout, stderr, jsonMode, app.NoColor, cmd.Short)
	defer app.SelectEnvironment("")
	result := struct {
		Success      bool                `json:"success"`
		Environments []environmentResult `json:"environments"`
	}{Success: true, Environments: make([]environmentResult, len(environments))}
	var failures []error
	var details []struct{ environment, body string }
	for i, env := range environments {
		entry := &result.Environments[i]
		entry.EnvironmentID, entry.EnvironmentName = env.ID, cmp.Or(env.Name, env.ID)
		if err := cmd.Context().Err(); err != nil {
			entry.Error = "not started: " + err.Error()
			failures = append(failures, fmt.Errorf("%s (%s): %w", entry.EnvironmentName, env.ID, err))
			continue
		}
		app.SelectEnvironment(env.ID)
		var buffer bytes.Buffer
		combined.environment = entry.EnvironmentName
		output.Configure(&buffer, stderr, jsonMode, app.NoColor, cmd.Short)
		runErr := run(cmd, args)
		if !jsonMode && buffer.Len() > 0 {
			details = append(details, struct{ environment, body string }{fmt.Sprintf("%s (%s)", entry.EnvironmentName, env.ID), buffer.String()})
		}
		if jsonMode {
			entry.Result = bytes.Clone(bytes.TrimSpace(buffer.Bytes()))
			if !entry.Result.IsValid() {
				runErr = cmp.Or(runErr, errors.New("command did not produce a valid JSON result"))
			}
		}
		if runErr != nil {
			entry.Result = nil
			entry.Error = runErr.Error()
			failures = append(failures, fmt.Errorf("%s (%s): %w", entry.EnvironmentName, env.ID, runErr))
			if !jsonMode {
				_, _ = fmt.Fprintf(stderr, "%s (%s): %v\n", entry.EnvironmentName, env.ID, runErr)
			}
		}
	}
	result.Success = len(failures) == 0
	output.Configure(stdout, stderr, jsonMode, app.NoColor, cmd.Short)
	cmd.SetContext(commandContext)
	if jsonMode {
		if err := PrintJSON(result); err != nil {
			failures = append(failures, err)
		}
		return errors.Join(failures...)
	}
	if combined.headers != nil {
		if err := PrintTable(cmd, combined.headers, combined.rows, combined.summary); err != nil {
			failures = append(failures, err)
		}
	}
	for _, detail := range details {
		output.Header("%s", detail.environment)
		if _, err := fmt.Fprint(stdout, detail.body); err != nil {
			failures = append(failures, err)
		}
	}
	if len(environments) == 0 {
		output.Info("No enabled environments")
	}
	return errors.Join(failures...)
}
