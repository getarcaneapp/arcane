package generate

import (
	"github.com/spf13/cobra"
)

var GenerateCmd = &cobra.Command{
	Use:     "generate",
	Aliases: []string{"gen", "g"},
	Short:   "Generate secrets for arcanes backend",
}
