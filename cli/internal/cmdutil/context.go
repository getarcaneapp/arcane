package cmdutil

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"getarcane.dev/cli/v3/internal/client"
	"getarcane.dev/cli/v3/internal/output"
	"getarcane.dev/cli/v3/internal/prompt"
	"getarcane.dev/cli/v3/internal/runtime"
)

type (
	ConfirmOptions struct {
		Label string
		Force bool
	}
)

func ClientFromCommand(cmd *cobra.Command) (*client.Client, error) {
	if cmd == nil {
		return nil, errors.New("nil command")
	}
	if app, ok := runtime.From(cmd.Context()); ok {
		return app.Client()
	}
	return client.NewFromConfig()
}

func UnauthClientFromCommand(cmd *cobra.Command) (*client.Client, error) {
	if cmd == nil {
		return nil, errors.New("nil command")
	}
	if app, ok := runtime.From(cmd.Context()); ok {
		return app.UnauthClient()
	}
	return client.NewFromConfigUnauthenticated()
}

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

func AssumeYes(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	yes, _ := cmd.Flags().GetBool("yes")
	app, ok := runtime.From(cmd.Context())
	return yes || ok && app.AssumeYes
}

func AllowPrompt(cmd *cobra.Command) bool {
	return !JSONOutputEnabled(cmd) && prompt.IsInteractive()
}

func ConfirmOrCancel(cmd *cobra.Command, opts ConfirmOptions) (bool, error) {
	app, _ := runtime.From(cmd.Context())
	if opts.Force || AssumeYes(cmd) || app != nil && app.AllConfirmed {
		return true, nil
	}
	if JSONOutputEnabled(cmd) {
		return false, fmt.Errorf("confirmation required: re-run with --yes (%s)", opts.Label)
	}
	confirmed, err := output.Confirm(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Label)
	if err == nil && !confirmed {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled")
	}
	return confirmed, err
}
