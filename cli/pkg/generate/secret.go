package generate

import (
	crand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/spf13/cobra"
)

var (
	secretFormat string
	secretLength int
)

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Generate cryptographic secrets",
	Long:  `Generate secure cryptographic secrets for ENCRYPTION_KEY and JWT_SECRET.`,
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
	if _, err := crand.Read(encryptionKey); err != nil {
		return fmt.Errorf("failed to generate encryption key: %w", err)
	}

	jwtSecret := make([]byte, secretLength)
	if _, err := crand.Read(jwtSecret); err != nil {
		return fmt.Errorf("failed to generate JWT secret: %w", err)
	}

	switch secretFormat {
	case "base64":
		printBase64Format(encryptionKey, jwtSecret)
	case "hex":
		printHexFormat(encryptionKey, jwtSecret)
	case "env":
		printEnvFormat(encryptionKey, jwtSecret)
	case "docker":
		printDockerFormat(encryptionKey, jwtSecret)
	case "all":
		printAllFormats(encryptionKey, jwtSecret)
	default:
		return fmt.Errorf("unknown format: %s (supported: base64, hex, env, docker, all)", secretFormat)
	}

	return nil
}

func printBase64Format(encKey, jwtKey []byte) {
	fmt.Println("BASE64")
	fmt.Println("------")
	fmt.Printf("ENCRYPTION_KEY=%s\n", base64.StdEncoding.EncodeToString(encKey))
	fmt.Printf("JWT_SECRET=%s\n", base64.StdEncoding.EncodeToString(jwtKey))
}

func printHexFormat(encKey, jwtKey []byte) {
	fmt.Println("HEX")
	fmt.Println("---")
	fmt.Printf("ENCRYPTION_KEY=%s\n", hex.EncodeToString(encKey))
	fmt.Printf("JWT_SECRET=%s\n", hex.EncodeToString(jwtKey))
}

func printEnvFormat(encKey, jwtKey []byte) {
	fmt.Println("ENV (.env) FORMAT")
	fmt.Println("-------------------")
	fmt.Printf("ENCRYPTION_KEY=%s\n", base64.StdEncoding.EncodeToString(encKey))
	fmt.Printf("JWT_SECRET=%s\n", base64.StdEncoding.EncodeToString(jwtKey))
}

func printDockerFormat(encKey, jwtKey []byte) {
	fmt.Println("DOCKER COMPOSE ENVIRONMENT")
	fmt.Println("--------------------------")
	fmt.Println("environment:")
	fmt.Printf("  - ENCRYPTION_KEY=%s\n", base64.StdEncoding.EncodeToString(encKey))
	fmt.Printf("  - JWT_SECRET=%s\n", base64.StdEncoding.EncodeToString(jwtKey))
}

func printAllFormats(encKey, jwtKey []byte) {
	fmt.Println("Arcane cryptographic secrets")
	fmt.Println("===========================")
	fmt.Println()

	fmt.Println("ENV (.env) - recommended")
	fmt.Println("------------------------")
	fmt.Printf("ENCRYPTION_KEY=%s\n", base64.StdEncoding.EncodeToString(encKey))
	fmt.Printf("JWT_SECRET=%s\n", base64.StdEncoding.EncodeToString(jwtKey))
	fmt.Println()

	fmt.Println("Docker Compose (environment block)")
	fmt.Println("-------------------------------")
	fmt.Println("environment:")
	fmt.Printf("  - ENCRYPTION_KEY=%s\n", base64.StdEncoding.EncodeToString(encKey))
	fmt.Printf("  - JWT_SECRET=%s\n", base64.StdEncoding.EncodeToString(jwtKey))
	fmt.Println()

	fmt.Println("HEX")
	fmt.Println("---")
	fmt.Printf("ENCRYPTION_KEY=%s\n", hex.EncodeToString(encKey))
	fmt.Printf("JWT_SECRET=%s\n", hex.EncodeToString(jwtKey))
	fmt.Println()
}
