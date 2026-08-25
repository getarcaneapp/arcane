package integrationtest

import (
	"strings"
	"testing"

	clipkg "github.com/getarcaneapp/arcane/cli/v2/pkg"
	"github.com/spf13/cobra"
)

// Command names must never contain hyphens; multi-word actions are flags or
// nested subcommands instead. Hidden commands are exempt (back-compat
// spellings of renamed commands), as is self-update — the one allowed
// hyphenated name.
func TestNoHyphenatedCommandNames(t *testing.T) {
	var walk func(cmd *cobra.Command, path string)
	walk = func(cmd *cobra.Command, path string) {
		for _, child := range cmd.Commands() {
			if child.Hidden || child.Name() == "self-update" {
				continue
			}
			childPath := path + " " + child.Name()
			if strings.Contains(child.Name(), "-") {
				t.Errorf("command %q has a hyphenated name; use a flag or nested subcommand instead", childPath)
			}
			walk(child, childPath)
		}
	}
	root := clipkg.RootCommand()
	walk(root, root.Name())
}
