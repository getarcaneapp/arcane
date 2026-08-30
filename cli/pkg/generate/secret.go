package generate

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"emperror.dev/errors"
	"github.com/spf13/cobra"
)

var (
	secretFormat string
	secretLength int
)

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Generate cryptographic secrets",
	Long:  `Generate a secure cryptographic secret for ENCRYPTION_KEY.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return generateSecrets()
	},
}

func init() {
	GenerateCmd.AddCommand(secretCmd)
	secretCmd.Flags().StringVarP(&secretFormat, "format", "f", "base64", "output format: base64, hex, env, docker, all")
	secretCmd.Flags().IntVarP(&secretLength, "length", "l", 32, "secret length in bytes (default: 32 for AES-256)")
}

func generateSecrets() error {
	encryptionKey := make([]byte, secretLength)
	if _, err := rand.Read(encryptionKey); err != nil {
		return errors.WrapIf(err, "failed to generate encryption key")
	}

	switch secretFormat {
	case "base64":
		printBase64Format(encryptionKey)
	case "hex":
		printHexFormat(encryptionKey)
	case "env":
		printEnvFormat(encryptionKey)
	case "docker":
		printDockerFormat(encryptionKey)
	case "all":
		printAllFormats(encryptionKey)
	default:
		return errors.Errorf("unknown format: %s (supported: base64, hex, env, docker, all)", secretFormat)
	}

	return nil
}

func printBase64Format(encKey []byte) {
	fmt.Println("BASE64")
	fmt.Println("------")
	fmt.Printf("ENCRYPTION_KEY=%s\n", base64.StdEncoding.EncodeToString(encKey))
}

func printHexFormat(encKey []byte) {
	fmt.Println("HEX")
	fmt.Println("---")
	fmt.Printf("ENCRYPTION_KEY=%s\n", hex.EncodeToString(encKey))
}

func printEnvFormat(encKey []byte) {
	fmt.Println("ENV (.env) FORMAT")
	fmt.Println("-------------------")
	fmt.Printf("ENCRYPTION_KEY=%s\n", base64.StdEncoding.EncodeToString(encKey))
}

func printDockerFormat(encKey []byte) {
	fmt.Println("DOCKER COMPOSE ENVIRONMENT")
	fmt.Println("--------------------------")
	fmt.Println("environment:")
	fmt.Printf("  - ENCRYPTION_KEY=%s\n", base64.StdEncoding.EncodeToString(encKey))
}

func printAllFormats(encKey []byte) {
	fmt.Println("Arcane cryptographic secrets")
	fmt.Println("===========================")
	fmt.Println()

	fmt.Println("ENV (.env) - recommended")
	fmt.Println("------------------------")
	fmt.Printf("ENCRYPTION_KEY=%s\n", base64.StdEncoding.EncodeToString(encKey))
	fmt.Println()

	fmt.Println("Docker Compose (environment block)")
	fmt.Println("-------------------------------")
	fmt.Println("environment:")
	fmt.Printf("  - ENCRYPTION_KEY=%s\n", base64.StdEncoding.EncodeToString(encKey))
	fmt.Println()

	fmt.Println("HEX")
	fmt.Println("---")
	fmt.Printf("ENCRYPTION_KEY=%s\n", hex.EncodeToString(encKey))
	fmt.Println()
}
