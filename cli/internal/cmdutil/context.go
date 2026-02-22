package cmdutil

import (
	"fmt"
	"strings"

	"github.com/getarcaneapp/arcane/cli/internal/client"
	runtimectx "github.com/getarcaneapp/arcane/cli/internal/runtime"
	"github.com/spf13/cobra"
)

// ClientFromCommand returns a configured authenticated client for the command.
func ClientFromCommand(cmd *cobra.Command) (*client.Client, error) {
	if cmd == nil {
		return nil, fmt.Errorf("nil command")
	}
	if app, ok := runtimectx.From(cmd.Context()); ok {
		return app.Client()
	}
	return client.NewFromConfig()
}

// UnauthClientFromCommand returns a configured unauthenticated client for the command.
func UnauthClientFromCommand(cmd *cobra.Command) (*client.Client, error) {
	if cmd == nil {
		return nil, fmt.Errorf("nil command")
	}
	if app, ok := runtimectx.From(cmd.Context()); ok {
		return app.UnauthClient()
	}
	return client.NewFromConfigUnauthenticated()
}

// JSONOutputEnabled returns true if JSON output is enabled for this command.
func JSONOutputEnabled(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	if flag := cmd.Flags().Lookup("json"); flag != nil && flag.Changed {
		val, err := cmd.Flags().GetBool("json")
		return err == nil && val
	}
	if app, ok := runtimectx.From(cmd.Context()); ok {
		return app.IsJSON()
	}
	return false
}

// AssumeYes returns true when prompts should be skipped.
func AssumeYes(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	if app, ok := runtimectx.From(cmd.Context()); ok {
		return app.AssumeYes()
	}
	return false
}

// ResolveOutputMode validates a free-form output mode value.
func ResolveOutputMode(mode string) (runtimectx.OutputMode, error) {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "" {
		return runtimectx.OutputModeText, nil
	}
	switch runtimectx.OutputMode(normalized) {
	case runtimectx.OutputModeText, runtimectx.OutputModeJSON:
		return runtimectx.OutputMode(normalized), nil
	default:
		return "", fmt.Errorf("invalid output mode %q", mode)
	}
}
