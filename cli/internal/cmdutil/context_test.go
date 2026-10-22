package cmdutil

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/creack/pty"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/internal/runtime"
)

type confirmationInput struct {
	io.Reader
	terminal *os.File
}

func (input confirmationInput) Fd() uintptr { return input.terminal.Fd() }

func TestConfirmOrCancel(t *testing.T) {
	for _, tt := range []struct {
		name                                                     string
		force, yes, json, terminal, fleet, runtimeYes, confirmed bool
		answer, wantErr                                          string
	}{
		{name: "force", force: true, json: true, confirmed: true},
		{name: "global yes", yes: true, confirmed: true},
		{name: "global yes in JSON", yes: true, json: true, confirmed: true},
		{name: "runtime yes", runtimeYes: true, confirmed: true},
		{name: "fleet consent", fleet: true, confirmed: true},
		{name: "JSON refuses prompt", json: true, wantErr: "confirmation required: re-run with --yes"},
		{name: "piped yes is not consent", answer: "yes\n", wantErr: "confirmation required: re-run with --yes"},
		{name: "terminal yes", terminal: true, answer: "yes\n", confirmed: true},
		{name: "terminal no", terminal: true, answer: "no\n"},
		{name: "terminal blank", terminal: true, answer: "\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			input := strings.NewReader(tt.answer)
			cmd := &cobra.Command{Use: "delete"}
			cmd.SetContext(context.WithValue(t.Context(), runtime.AppContextKey, &runtime.AppContext{
				Options: runtime.Options{AssumeYes: tt.runtimeYes}, FleetConfirmed: tt.fleet,
			}))
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetIn(input)
			cmd.Flags().Bool("json", tt.json, "")
			cmd.Flags().Bool("yes", tt.yes, "")
			if tt.terminal {
				master, terminal, terminalErr := pty.Open()
				require.NoError(t, terminalErr)
				defer func() { _ = master.Close(); _ = terminal.Close() }()
				t.Setenv("TERM", "xterm")
				cmd.SetIn(confirmationInput{Reader: input, terminal: terminal})
			}
			confirmed, confirmErr := ConfirmOrCancel(cmd, tt.force, "Delete record?")
			require.Equal(t, tt.confirmed, confirmed)
			if tt.wantErr != "" {
				require.ErrorContains(t, confirmErr, tt.wantErr)
			} else {
				require.NoError(t, confirmErr)
			}
			require.Empty(t, stdout.String())
			if !tt.terminal {
				require.Equal(t, len(tt.answer), input.Len(), "confirmation must not read noninteractive input")
				require.Empty(t, stderr.String())
			}
			if tt.terminal && !tt.confirmed {
				require.Contains(t, stderr.String(), "Cancelled\n")
			}
		})
	}
}
