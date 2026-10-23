package cli

import (
	"slices"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
)

func TestEveryRunnableCommandHasScope(t *testing.T) {
	scopes := []string{
		cmdutil.ScopeRead,
		cmdutil.ScopeMerged,
		cmdutil.ScopeAll,
		cmdutil.ScopeDestination,
		cmdutil.ScopeResource,
		cmdutil.ScopeGlobal,
	}
	var missing []string
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, child := range cmd.Commands() {
			if child.Runnable() && !slices.Contains(scopes, cmdutil.Scope(child)) {
				missing = append(missing, child.CommandPath())
			}
			walk(child)
		}
	}
	walk(NewRootCommand())
	require.Empty(t, missing)
}
