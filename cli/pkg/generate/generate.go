package generate

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Annotations: map[string]string{"arcane:scope": cmdutil.ScopeGlobal},
		Use:         "generate",
		Aliases:     []string{"gen", "g"},
		Short:       "Generate secrets and bootstrap credentials for Arcane",
	}
	cmd.AddCommand(newAPIKeyCmd(), newMTLSCmd(), newTLSCmd(), newSecretCmd())
	return cmd
}

// The backend embeds this package, so it stays off cmdutil to avoid pulling in the CLI's TUI dependencies.
func jsonOutput(cmd *cobra.Command) bool {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	outputMode, _ := cmd.Flags().GetString("output")
	return jsonFlag || outputMode == "json"
}

func printJSON(cmd *cobra.Command, v any) error {
	if err := json.MarshalWrite(cmd.OutOrStdout(), v, jsontext.WithIndent("  ")); err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout())
	return err
}
