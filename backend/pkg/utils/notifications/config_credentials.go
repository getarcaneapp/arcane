package notifications

import (
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"

	"go.getarcane.app/sys/crypto"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

// DecodeConfig round-trips a provider config (database.JSON) into a typed struct T.
func DecodeConfig[T any](config database.JSON, providerName string) (T, error) {
	var out T
	configBytes, err := json.Marshal(config)
	if err != nil {
		return out, fmt.Errorf("failed to marshal %s config: %w", providerName, err)
	}
	if unmarshalErr := json.Unmarshal(configBytes, &out); unmarshalErr != nil {
		return out, fmt.Errorf("failed to unmarshal %s config: %w", providerName, unmarshalErr)
	}
	return out, nil
}

// DecryptStringCredential decrypts an encrypted credential in place. A value that
// fails to decrypt but is not plausibly ciphertext is treated as a legacy raw value.
func DecryptStringCredential(value *string) error {
	if *value == "" {
		return nil
	}

	decrypted, err := crypto.Decrypt(*value)
	if err != nil {
		if isPlausibleEncryptedCredentialInternal(*value) {
			return fmt.Errorf("failed to decrypt notification credential: %w", err)
		}
		slog.WarnContext(context.Background(), "Failed to decrypt notification credential, using raw legacy value", //nolint:forbidigo // Credential decryption is a pure helper without a request context.
			"error", err)
		return nil
	}
	*value = decrypted
	return nil
}

func isPlausibleEncryptedCredentialInternal(value string) bool {
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return false
	}
	const minAESGCMCiphertextSize = 12 + 16
	return len(data) >= minAESGCMCiphertextSize
}

// PrepareSlackConfig decodes and decrypts a Slack provider config.
func PrepareSlackConfig(config database.JSON, providerName string, requireToken bool) (SlackConfig, error) {
	slackConfig, err := DecodeConfig[SlackConfig](config, providerName)
	if err != nil {
		return SlackConfig{}, err
	}
	if requireToken && slackConfig.Token == "" {
		return SlackConfig{}, errors.New("slack token not configured")
	}
	if decryptStringCredentialErr := DecryptStringCredential(&slackConfig.Token); decryptStringCredentialErr != nil {
		return SlackConfig{}, decryptStringCredentialErr
	}
	return slackConfig, nil
}

// PrepareNtfyConfig decodes and decrypts an ntfy provider config.
func PrepareNtfyConfig(config database.JSON, providerName string, requireTopic bool) (NtfyConfig, error) {
	ntfyConfig, err := DecodeConfig[NtfyConfig](config, providerName)
	if err != nil {
		return NtfyConfig{}, err
	}
	if requireTopic && ntfyConfig.Topic == "" {
		return NtfyConfig{}, errors.New("ntfy topic is required")
	}
	if _, ok := config["cache"]; !ok {
		ntfyConfig.Cache = true
	}
	if _, ok := config["firebase"]; !ok {
		ntfyConfig.Firebase = true
	}
	if decryptStringCredentialErr := DecryptStringCredential(&ntfyConfig.Password); decryptStringCredentialErr != nil {
		return NtfyConfig{}, decryptStringCredentialErr
	}
	return ntfyConfig, nil
}

// PreparePushoverConfig decodes and decrypts a Pushover provider config.
func PreparePushoverConfig(config database.JSON, providerName string) (PushoverConfig, error) {
	pushoverConfig, err := DecodeConfig[PushoverConfig](config, providerName)
	if err != nil {
		return PushoverConfig{}, err
	}
	if decryptStringCredentialErr := DecryptStringCredential(&pushoverConfig.Token); decryptStringCredentialErr != nil {
		return PushoverConfig{}, decryptStringCredentialErr
	}
	return pushoverConfig, nil
}

// PrepareGotifyConfig decodes and decrypts a Gotify provider config.
func PrepareGotifyConfig(config database.JSON, providerName string) (GotifyConfig, error) {
	gotifyConfig, err := DecodeConfig[GotifyConfig](config, providerName)
	if err != nil {
		return GotifyConfig{}, err
	}
	if decryptStringCredentialErr := DecryptStringCredential(&gotifyConfig.Token); decryptStringCredentialErr != nil {
		return GotifyConfig{}, decryptStringCredentialErr
	}
	return gotifyConfig, nil
}

// PrepareMatrixConfig decodes and decrypts a Matrix provider config.
func PrepareMatrixConfig(config database.JSON) (MatrixConfig, error) {
	matrixConfig, err := DecodeConfig[MatrixConfig](config, "Matrix")
	if err != nil {
		return MatrixConfig{}, err
	}
	if decryptStringCredentialErr := DecryptStringCredential(&matrixConfig.Password); decryptStringCredentialErr != nil {
		return MatrixConfig{}, decryptStringCredentialErr
	}
	return matrixConfig, nil
}
