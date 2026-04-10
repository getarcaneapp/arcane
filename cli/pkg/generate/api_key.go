package generate

import (
	crand "crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/spf13/cobra"
)

const (
	apiKeyPrefix = "arc_"
	apiKeyLength = 32
)

var apiKeyCmd = &cobra.Command{
	Use:   "api-key",
	Short: "Generate a static admin API key",
	Long:  `Generate a static Arcane API key suitable for ADMIN_STATIC_API_KEY. This is just a local generation, nothing is stored to the database with these commands.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return generateAPIKeyOutputInternal()
	},
}

func init() {
	GenerateCmd.AddCommand(apiKeyCmd)
}

func GenerateAPIKey() (string, error) {
	keyBytes := make([]byte, apiKeyLength)
	if _, err := crand.Read(keyBytes); err != nil {
		return "", fmt.Errorf("failed to generate API key: %w", err)
	}

	return apiKeyPrefix + hex.EncodeToString(keyBytes), nil
}

func generateAPIKeyOutputInternal() error {
	apiKey, err := GenerateAPIKey()
	if err != nil {
		return err
	}

	fmt.Println(apiKey)
	return nil
}
