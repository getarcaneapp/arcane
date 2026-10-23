package completion

import (
	"bytes"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
)

// NewCommand returns an explicit shell completion command.
func NewCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Annotations:           map[string]string{"arcane:scope": cmdutil.ScopeGlobal},
		Use:                   "completion <bash|zsh|fish|powershell>",
		Short:                 "Generate shell completion scripts",
		Long:                  "Generate shell completion scripts for bash, zsh, fish, or powershell.",
		Args:                  cobra.ExactArgs(1),
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell", "pwsh"},
		SilenceUsage:          true,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var script bytes.Buffer
			var generationErr error
			switch args[0] {
			case "bash":
				generationErr = root.GenBashCompletionV2(&script, true)
			case "zsh":
				generationErr = root.GenZshCompletion(&script)
			case "fish":
				generationErr = root.GenFishCompletion(&script, true)
			case "powershell", "pwsh":
				generationErr = root.GenPowerShellCompletionWithDesc(&script)
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
			}
			if generationErr != nil {
				return generationErr
			}
			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(struct {
					Shell  string `json:"shell"`
					Script string `json:"script"`
				}{Shell: args[0], Script: script.String()})
			}
			_, writeErr := fmt.Fprint(cmd.OutOrStdout(), script.String())
			return writeErr
		},
	}
}
