package cli

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"

	"github.com/spf13/cobra"

	"kit.getarcane.app/fs/atomic"

	"getarcane.dev/arcane/v3/api"
	"getarcane.dev/arcane/v3/internal/bootstrap"
	"getarcane.dev/arcane/v3/pkg/authz"
)

var permissionsCmd = &cobra.Command{
	Use:   "permissions",
	Short: "Export the permission catalog and the endpoints each permission covers",
	Long:  "Export the permission catalog, the API endpoints each permission covers, and every endpoint's access requirement as JSON",
	Run: func(cmd *cobra.Command, args []string) {
		outputFile, err := cmd.Flags().GetString("output")
		if err != nil {
			cmd.PrintErrf("Error reading --output flag: %v\n", err)
			os.Exit(1)
		}

		output, err := json.Marshal(
			authz.BuildPermissionExport(api.SetupAPIForSpec(), bootstrap.EchoRoutePermissions()),
			json.Deterministic(true),
			jsontext.WithIndent("  "),
		)
		if err != nil {
			cmd.PrintErrf("Error encoding permission export: %v\n", err)
			os.Exit(1)
		}
		output = append(output, '\n')

		if outputFile != "" {
			if writeFileErr := atomic.WriteFile(outputFile, output, 0o600); writeFileErr != nil {
				cmd.PrintErrf("Error writing file: %v\n", writeFileErr)
				os.Exit(1)
			}
			cmd.PrintErrf("Permission export written to %s\n", outputFile)
			return
		}

		cmd.Print(string(output))
	},
}

func init() {
	permissionsCmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	rootCmd.AddCommand(permissionsCmd)
}
