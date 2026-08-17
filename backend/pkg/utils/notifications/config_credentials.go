package notifications

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"encoding/base64"
	"encoding/json/v2"
	"log/slog"

	"emperror.dev/errors"

	"go.getarcane.app/sys/crypto"
)

// DecodeConfig round-trips a provider config (database.JSON) into a typed struct T.
func DecodeConfig[T any](config database.JSON, providerName string) (T, error) {
	var out T
	configBytes, err := json.Marshal(config)
	if err != nil {
		return out, errors.WrapIff(err, "failed to marshal %s config", providerName)
	}
	if err := json.Unmarshal(configBytes, &out); err != nil {
		return out, errors.WrapIff(err, "failed to unmarshal %s config", providerName)
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
			return errors.WrapIf(err, "failed to decrypt notification credential")
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
func PrepareSlackConfig(config database.JSON, providerName string, requireToken bool) (SlackConfig, error) {
	slackConfig, err := DecodeConfig[SlackConfig](config, providerName)
	if err != nil {
		return SlackConfig{}, err
	}
	if requireToken && slackConfig.Token == "" {
		return SlackConfig{}, errors.New("slack token not configured")
	}
	if err := DecryptStringCredential(&slackConfig.Token); err != nil {
		return SlackConfig{}, err
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
	if err := DecryptStringCredential(&ntfyConfig.Password); err != nil {
		return NtfyConfig{}, err
	}
	return ntfyConfig, nil
}

// PreparePushoverConfig decodes and decrypts a Pushover provider config.
func PreparePushoverConfig(config database.JSON, providerName string) (PushoverConfig, error) {
	pushoverConfig, err := DecodeConfig[PushoverConfig](config, providerName)
	if err != nil {
		return PushoverConfig{}, err
	}
	if err := DecryptStringCredential(&pushoverConfig.Token); err != nil {
		return PushoverConfig{}, err
	}
	return pushoverConfig, nil
}

// PrepareGotifyConfig decodes and decrypts a Gotify provider config.
func PrepareGotifyConfig(config database.JSON, providerName string) (GotifyConfig, error) {
	gotifyConfig, err := DecodeConfig[GotifyConfig](config, providerName)
	if err != nil {
		return GotifyConfig{}, err
	}
	if err := DecryptStringCredential(&gotifyConfig.Token); err != nil {
		return GotifyConfig{}, err
	}
	return gotifyConfig, nil
}

// PrepareMatrixConfig decodes and decrypts a Matrix provider config.
func PrepareMatrixConfig(config database.JSON) (MatrixConfig, error) {
	matrixConfig, err := DecodeConfig[MatrixConfig](config, "Matrix")
	if err != nil {
		return MatrixConfig{}, err
	}
	if err := DecryptStringCredential(&matrixConfig.Password); err != nil {
		return MatrixConfig{}, err
	}
	return matrixConfig, nil
}
