package cli

import (
	"fmt"
	"os"

	"github.com/getarcaneapp/arcane/backend/internal/configschema"
	"github.com/spf13/cobra"
)

var configSchemaCmd = &cobra.Command{
	Use:   "config-schema",
	Short: "Export the config/settings schema for docs",
	Long:  "Export the canonical JSON schema for runtime config and settings environment overrides",
	Run: func(cmd *cobra.Command, args []string) {
		outputFile, err := cmd.Flags().GetString("output")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading --output flag: %v\n", err)
			os.Exit(1)
		}

		sourceRoot, err := cmd.Flags().GetString("source-root")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading --source-root flag: %v\n", err)
			os.Exit(1)
		}

		doc, err := configschema.GenerateWithSourceRoot(sourceRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating config schema: %v\n", err)
			os.Exit(1)
		}

		output, err := configschema.MarshalJSON(doc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error encoding config schema: %v\n", err)
			os.Exit(1)
		}

		if outputFile != "" {
			if err := os.WriteFile(outputFile, output, 0o600); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Config schema written to %s\n", outputFile)
			return
		}

		fmt.Print(string(output))
	},
}

func init() {
	configSchemaCmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	configSchemaCmd.Flags().String("source-root", "", "Repository root or backend source root (default: auto-detect from working directory)")
	rootCmd.AddCommand(configSchemaCmd)
}
