package notifications

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/crypto"
)

// DecodeConfig round-trips a provider config (models.JSON) into a typed struct T.
func DecodeConfig[T any](config models.JSON, providerName string) (T, error) {
	var out T
	configBytes, err := json.Marshal(config)
	if err != nil {
		return out, fmt.Errorf("failed to marshal %s config: %w", providerName, err)
	}
	if err := json.Unmarshal(configBytes, &out); err != nil {
		return out, fmt.Errorf("failed to unmarshal %s config: %w", providerName, err)
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
		slog.Warn("Failed to decrypt notification credential, using raw legacy value", "error", err)
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
func PrepareSlackConfig(config models.JSON, providerName string, requireToken bool) (models.SlackConfig, error) {
	slackConfig, err := DecodeConfig[models.SlackConfig](config, providerName)
	if err != nil {
		return models.SlackConfig{}, err
	}
	if requireToken && slackConfig.Token == "" {
		return models.SlackConfig{}, errors.New("slack token not configured")
	}
	if err := DecryptStringCredential(&slackConfig.Token); err != nil {
		return models.SlackConfig{}, err
	}
	return slackConfig, nil
}

// PrepareNtfyConfig decodes and decrypts an ntfy provider config.
func PrepareNtfyConfig(config models.JSON, providerName string, requireTopic bool) (models.NtfyConfig, error) {
	ntfyConfig, err := DecodeConfig[models.NtfyConfig](config, providerName)
	if err != nil {
		return models.NtfyConfig{}, err
	}
	if requireTopic && ntfyConfig.Topic == "" {
		return models.NtfyConfig{}, errors.New("ntfy topic is required")
	}
	if err := DecryptStringCredential(&ntfyConfig.Password); err != nil {
		return models.NtfyConfig{}, err
	}
	return ntfyConfig, nil
}

// PreparePushoverConfig decodes and decrypts a Pushover provider config.
func PreparePushoverConfig(config models.JSON, providerName string) (models.PushoverConfig, error) {
	pushoverConfig, err := DecodeConfig[models.PushoverConfig](config, providerName)
	if err != nil {
		return models.PushoverConfig{}, err
	}
	if err := DecryptStringCredential(&pushoverConfig.Token); err != nil {
		return models.PushoverConfig{}, err
	}
	return pushoverConfig, nil
}

// PrepareGotifyConfig decodes and decrypts a Gotify provider config.
func PrepareGotifyConfig(config models.JSON, providerName string) (models.GotifyConfig, error) {
	gotifyConfig, err := DecodeConfig[models.GotifyConfig](config, providerName)
	if err != nil {
		return models.GotifyConfig{}, err
	}
	if err := DecryptStringCredential(&gotifyConfig.Token); err != nil {
		return models.GotifyConfig{}, err
	}
	return gotifyConfig, nil
}

// PrepareMatrixConfig decodes and decrypts a Matrix provider config.
func PrepareMatrixConfig(config models.JSON) (models.MatrixConfig, error) {
	matrixConfig, err := DecodeConfig[models.MatrixConfig](config, "Matrix")
	if err != nil {
		return models.MatrixConfig{}, err
	}
	if err := DecryptStringCredential(&matrixConfig.Password); err != nil {
		return models.MatrixConfig{}, err
	}
	return matrixConfig, nil
}
