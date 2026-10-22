package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/acfs/atomic"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
)

var permissionsTSKeyPattern = regexp.MustCompile(`[^A-Z0-9]+`)

func newPermissionsTSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "permissions-ts",
		Short:  "Generate the frontend permission constants",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			source, err := renderPermissionsTS()
			if err != nil {
				return err
			}
			outputFile, err := cmd.Flags().GetString("output")
			if err != nil {
				return err
			}
			if outputFile == "" {
				cmd.Print(source)
				return nil
			}
			if writeErr := atomic.WriteFile(outputFile, []byte(source), 0o644); writeErr != nil {
				return fmt.Errorf("write %s: %w", outputFile, writeErr)
			}
			cmd.PrintErrf("Permission constants written to %s\n", outputFile)
			return nil
		},
	}
	cmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	return cmd
}

func renderPermissionsTS() (string, error) {
	var b strings.Builder
	seen := map[string]string{}
	b.WriteString("export const PERMISSIONS = {\n")
	for _, perm := range append([]string{authz.PermAll}, authz.AllPermissions()...) {
		key := strings.Trim(permissionsTSKeyPattern.ReplaceAllString(strings.ToUpper(perm), "_"), "_")
		if perm == authz.PermAll {
			key = "ALL"
		}
		if existing, ok := seen[key]; ok {
			return "", fmt.Errorf("permissions %q and %q both map to key %s", existing, perm, key)
		}
		seen[key] = perm
		fmt.Fprintf(&b, "\t%s: '%s',\n", key, perm)
	}
	b.WriteString("} as const;\n\n")
	b.WriteString("export type Permission = (typeof PERMISSIONS)[keyof typeof PERMISSIONS];\n\n")

	selectorKinds, err := json.Marshal([]types.SelectorKind{
		types.SelectorKindLabels,
		types.SelectorKindProjects,
		types.SelectorKindOwner,
		types.SelectorKindNames,
	})
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "export const SELECTOR_KINDS = %s as const;\n\n", selectorKinds)
	b.WriteString("export type SelectorKind = (typeof SELECTOR_KINDS)[number];\n")
	return b.String(), nil
}
