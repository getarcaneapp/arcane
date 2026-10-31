package cmdutil

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v3/internal/client"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	"github.com/getarcaneapp/arcane/cli/v3/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v3/internal/runtime"
)

// ClientFromCommand returns a configured authenticated client for the command.
func ClientFromCommand(cmd *cobra.Command) (*client.Client, error) {
	if cmd == nil {
		return nil, errors.New("nil command")
	}
	if app, ok := runtime.From(cmd.Context()); ok {
		return app.Client()
	}
	return client.NewFromConfig()
}

// UnauthClientFromCommand returns a configured unauthenticated client for the command.
func UnauthClientFromCommand(cmd *cobra.Command) (*client.Client, error) {
	if cmd == nil {
		return nil, errors.New("nil command")
	}
	if app, ok := runtime.From(cmd.Context()); ok {
		return app.UnauthClient()
	}
	return client.NewFromConfigUnauthenticated()
}

// JSONOutputEnabled reads the parsed local or inherited --json flag, then runtime output mode.
// Runtime JSON mode includes --output json, even when --json is explicitly false.
func JSONOutputEnabled(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	jsonMode, _ := cmd.Flags().GetBool("json")
	if jsonMode {
		return true
	}
	if app, ok := runtime.From(cmd.Context()); ok {
		return app.IsJSON()
	}
	return false
}

// AssumeYes reads the global --yes flag or invocation consent.
func AssumeYes(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	yes, _ := cmd.Flags().GetBool("yes")
	app, ok := runtime.From(cmd.Context())
	return yes || ok && app.AssumeYes
}

// AllowPrompt reports whether interactive prompts are allowed for this command.
func AllowPrompt(cmd *cobra.Command) bool {
	return !JSONOutputEnabled(cmd) && prompt.IsInteractive()
}

// Declining prints Cancelled to stderr and returns false without an error.
func ConfirmOrCancel(cmd *cobra.Command, force bool, label string) (bool, error) {
	app, _ := runtime.From(cmd.Context())
	if force || AssumeYes(cmd) || app != nil && app.AllConfirmed {
		return true, nil
	}
	if JSONOutputEnabled(cmd) {
		return false, fmt.Errorf("confirmation required: re-run with --yes (%s)", label)
	}
	confirmed, err := output.Confirm(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), label)
	if err == nil && !confirmed {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled")
	}
	return confirmed, err
}

// Confirm prompts through ConfirmOrCancel unless invocation consent is already present.
func Confirm(cmd *cobra.Command, label string) (bool, error) {
	return ConfirmOrCancel(cmd, false, label)
}
