package migrator

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownRequiresTarget(t *testing.T) {
	var out bytes.Buffer
	cmd := NewCommand(&out)
	cmd.SetArgs([]string{"down"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorContains(t, err, "accepts 1 arg")
}

func TestCommandIncludesMigrationCommands(t *testing.T) {
	cmd := NewCommand(nil)

	commandNames := make(map[string]struct{})
	for _, child := range cmd.Commands() {
		commandNames[child.Name()] = struct{}{}
	}

	assert.Contains(t, commandNames, "status")
	assert.Contains(t, commandNames, "up")
	assert.Contains(t, commandNames, "down")
	assert.Contains(t, commandNames, "generate-manifest")
}
