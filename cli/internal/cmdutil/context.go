package cmdutil

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	runtimectx "github.com/getarcaneapp/arcane/cli/v2/internal/runtime"
	"github.com/spf13/cobra"
)

// ClientFromCommand returns a configured authenticated client for the command.
func ClientFromCommand(cmd *cobra.Command) (*client.Client, error) {
	if cmd == nil {
		return nil, errors.New("nil command")
	}
	if app, ok := runtimectx.From(cmd.Context()); ok {
		return app.Client()
	}
	return client.NewFromConfig()
}

// UnauthClientFromCommand returns a configured unauthenticated client for the command.
func UnauthClientFromCommand(cmd *cobra.Command) (*client.Client, error) {
	if cmd == nil {
		return nil, errors.New("nil command")
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

// Confirm prompts the user unless --yes is enabled.
func Confirm(cmd *cobra.Command, prompt string) (bool, error) {
	if AssumeYes(cmd) {
		return true, nil
	}

	fmt.Printf("%s (y/N): ", strings.TrimSpace(prompt))
	var response string
	if _, err := fmt.Scanln(&response); err != nil && !errors.Is(err, io.EOF) {
		// Keep EOF as a default "no" response, but surface other input failures.
		return false, fmt.Errorf("failed to read confirmation input: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(response)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
