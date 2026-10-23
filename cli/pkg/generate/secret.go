package generate

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	kit "go.getarcane.app/kit/pkg"
)

var secretFormats = []string{"base64", "hex", "env", "docker", "all"}

func newSecretCmd() *cobra.Command {
	var opts struct {
		format string
		length int
	}
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Generate cryptographic secrets",
		Long:  `Generate a secure cryptographic secret for ARCANE_ENCRYPTION_KEY.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return generateSecrets(cmd, opts.format, opts.length)
		},
	}
	cmd.Flags().StringVarP(&opts.format, "format", "f", "base64", "output format: "+strings.Join(secretFormats, ", "))
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions(secretFormats, cobra.ShellCompDirectiveNoFileComp))
	cmd.Flags().IntVarP(&opts.length, "length", "l", 32, "secret length in bytes (default: 32 for AES-256)")
	return cmd
}

func generateSecrets(cmd *cobra.Command, secretFormat string, secretLength int) error {
	if !slices.Contains(secretFormats, secretFormat) {
		return fmt.Errorf("unknown format: %s (supported: %s)", secretFormat, strings.Join(secretFormats, ", "))
	}
	w := cmd.OutOrStdout()
	encryptionKey := make([]byte, secretLength)
	if _, err := rand.Read(encryptionKey); err != nil {
		return fmt.Errorf("failed to generate encryption key: %w", err)
	}

	if jsonOutput(cmd) {
		return printJSON(cmd, struct {
			EncryptionKey string `json:"encryptionKey"`
			Hex           string `json:"hex,omitempty"`
		}{
			EncryptionKey: kit.Ternary(secretFormat == "hex", hex.EncodeToString(encryptionKey), base64.StdEncoding.EncodeToString(encryptionKey)),
			Hex:           kit.Ternary(secretFormat == "all", hex.EncodeToString(encryptionKey), ""),
		})
	}
	switch secretFormat {
	case "base64":
		_, _ = fmt.Fprintln(w, "BASE64")
		_, _ = fmt.Fprintln(w, "------")
		_, _ = fmt.Fprintf(w, "ARCANE_ENCRYPTION_KEY=%s\n", base64.StdEncoding.EncodeToString(encryptionKey))
	case "hex":
		_, _ = fmt.Fprintln(w, "HEX")
		_, _ = fmt.Fprintln(w, "---")
		_, _ = fmt.Fprintf(w, "ARCANE_ENCRYPTION_KEY=%s\n", hex.EncodeToString(encryptionKey))
	case "env":
		_, _ = fmt.Fprintln(w, "ENV (.env) FORMAT")
		_, _ = fmt.Fprintln(w, "-------------------")
		_, _ = fmt.Fprintf(w, "ARCANE_ENCRYPTION_KEY=%s\n", base64.StdEncoding.EncodeToString(encryptionKey))
	case "docker":
		_, _ = fmt.Fprintln(w, "DOCKER COMPOSE ENVIRONMENT")
		_, _ = fmt.Fprintln(w, "--------------------------")
		_, _ = fmt.Fprintln(w, "environment:")
		_, _ = fmt.Fprintf(w, "  - ARCANE_ENCRYPTION_KEY=%s\n", base64.StdEncoding.EncodeToString(encryptionKey))
	case "all":
		_, _ = fmt.Fprintln(w, "Arcane cryptographic secrets")
		_, _ = fmt.Fprintln(w, "===========================")
		_, _ = fmt.Fprintln(w)

		_, _ = fmt.Fprintln(w, "ENV (.env) - recommended")
		_, _ = fmt.Fprintln(w, "------------------------")
		_, _ = fmt.Fprintf(w, "ARCANE_ENCRYPTION_KEY=%s\n", base64.StdEncoding.EncodeToString(encryptionKey))
		_, _ = fmt.Fprintln(w)

		_, _ = fmt.Fprintln(w, "Docker Compose (environment block)")
		_, _ = fmt.Fprintln(w, "-------------------------------")
		_, _ = fmt.Fprintln(w, "environment:")
		_, _ = fmt.Fprintf(w, "  - ARCANE_ENCRYPTION_KEY=%s\n", base64.StdEncoding.EncodeToString(encryptionKey))
		_, _ = fmt.Fprintln(w)

		_, _ = fmt.Fprintln(w, "HEX")
		_, _ = fmt.Fprintln(w, "---")
		_, _ = fmt.Fprintf(w, "ARCANE_ENCRYPTION_KEY=%s\n", hex.EncodeToString(encryptionKey))
		_, _ = fmt.Fprintln(w)
	}

	return nil
}
