package migrate

import (
	"bytes"
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownRequiresTarget(t *testing.T) {
	var out bytes.Buffer
	cmd := newCommand(&out)
	cmd.SetArgs([]string{"down"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorContains(t, err, "accepts 1 arg")
}

func TestCommandIncludesMigrationCommands(t *testing.T) {
	cmd := newCommand(nil)
	assert.Equal(t, "migrate", cmd.Name())

	commandNames := make(map[string]struct{})
	for _, child := range cmd.Commands() {
		commandNames[child.Name()] = struct{}{}
	}

	assert.Contains(t, commandNames, "status")
	assert.Contains(t, commandNames, "up")
	assert.Contains(t, commandNames, "down")
	assert.Contains(t, commandNames, "generate-manifest")
}

func newCommand(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "migrate",
		Short:         "Manage Arcane database schema migrations",
		Version:       MigrateCmd.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	configureCommand(cmd, out)
	return cmd
}
