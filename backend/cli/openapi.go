package cli

import (
	"fmt"
	"os"

	"github.com/getarcaneapp/arcane/backend/internal/huma"
	"github.com/spf13/cobra"
)

var openapiCmd = &cobra.Command{
	Use:   "openapi",
	Short: "Export the OpenAPI specification",
	Long:  "Export the OpenAPI 3.1 specification in YAML or JSON format",
	Run: func(cmd *cobra.Command, args []string) {
		format, _ := cmd.Flags().GetString("format")
		downgrade, _ := cmd.Flags().GetBool("downgrade")
		outputFile, _ := cmd.Flags().GetString("output")

		// Create Huma API for spec generation (no services needed)
		api := huma.SetupAPIForSpec()

		// Generate output
		var output []byte
		var err error

		if format == "json" {
			output, err = api.OpenAPI().MarshalJSON()
		} else {
			if downgrade {
				output, err = api.OpenAPI().DowngradeYAML()
			} else {
				output, err = api.OpenAPI().YAML()
			}
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating OpenAPI spec: %v\n", err)
			os.Exit(1)
		}

		// Write to file or stdout
		if outputFile != "" {
			if err := os.WriteFile(outputFile, output, 0o600); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "OpenAPI spec written to %s\n", outputFile)
		} else {
			fmt.Println(string(output))
		}
	},
}

func init() {
	openapiCmd.Flags().StringP("format", "f", "yaml", "Output format: yaml or json")
	openapiCmd.Flags().BoolP("downgrade", "d", false, "Downgrade to OpenAPI 3.0.3 for compatibility")
	openapiCmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	rootCmd.AddCommand(openapiCmd)
}
