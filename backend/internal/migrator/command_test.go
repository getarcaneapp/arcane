package migrator

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDowngradeRequiresTarget(t *testing.T) {
	var out bytes.Buffer
	cmd := NewCommand(&out)
	cmd.SetArgs([]string{"downgrade"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorContains(t, err, "--target is required")
}

func TestCommandIncludesStatusAndDowngrade(t *testing.T) {
	cmd := NewCommand(nil)

	commandNames := make(map[string]struct{})
	for _, child := range cmd.Commands() {
		commandNames[child.Name()] = struct{}{}
	}

	assert.Contains(t, commandNames, "status")
	assert.Contains(t, commandNames, "downgrade")
	assert.Contains(t, commandNames, "generate-manifest")
}
