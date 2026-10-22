package generate

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/spf13/cobra"
)

const (
	apiKeyPrefix = "arc_"
	apiKeyLength = 32
)

func newAPIKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "apikey",
		Aliases: []string{"api-key"},
		Short:   "Generate a static admin API key",
		Long:    `Generate a static Arcane API key suitable for ARCANE_ADMIN_STATIC_API_KEY. This is just a local generation, nothing is stored to the database with these commands.`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			apiKey, err := GenerateAPIKey()
			if err != nil {
				return err
			}
			if jsonOutput(cmd) {
				return printJSON(cmd, struct {
					Key string `json:"key"`
				}{Key: apiKey})
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), apiKey)
			return nil
		},
	}
}

func GenerateAPIKey() (string, error) {
	keyBytes := make([]byte, apiKeyLength)
	if _, err := rand.Read(keyBytes); err != nil {
		return "", fmt.Errorf("failed to generate API key: %w", err)
	}

	return apiKeyPrefix + hex.EncodeToString(keyBytes), nil
}
