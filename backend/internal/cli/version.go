package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/backend/internal/config"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Print detailed version information about Arcane.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Arcane version: %s\n", config.Version)
		fmt.Printf("Git revision: %s\n", config.Revision)
		fmt.Printf("Go version: %s\n", runtime.Version())
		fmt.Printf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
