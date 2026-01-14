package crypto

import (
	"encoding/base64"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseExplicitKey(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		description string
	}{
		{
			name:        "valid hex key from openssl rand -hex 32",
			input:       "e176e082991ba899f0687bdc00c4372f62ecfa2f021f6b226103166f2be775dd",
			expectError: false,
			description: "64 hex characters representing 32 bytes",
		},
		{
			name:        "valid hex key uppercase",
			input:       "0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF",
			expectError: false,
			description: "Uppercase hex should work",
		},
		{
			name:        "valid hex key with whitespace",
			input:       "  0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  \n",
			expectError: false,
			description: "Hex with leading/trailing whitespace should be trimmed",
		},
		{
			name:        "valid base64 standard encoding",
			input:       base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901")),
			expectError: false,
			description: "Standard base64 encoded 32 bytes",
		},
		{
			name:        "valid base64 raw encoding",
			input:       base64.RawStdEncoding.EncodeToString([]byte("01234567890123456789012345678901")),
			expectError: false,
			description: "Raw base64 encoded 32 bytes (no padding)",
		},
		{
			name:        "valid raw 32 byte string",
			input:       "01234567890123456789012345678901",
			expectError: false,
			description: "Exactly 32 ASCII characters",
		},
		{
			name:        "invalid hex key too short",
			input:       "0123456789abcdef",
			expectError: true,
			description: "16 hex characters (8 bytes) - too short",
		},
		{
			name:        "valid hex key longer than 32 bytes uses first 32",
			input:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef00",
			expectError: false,
			description: "66 hex characters (33 bytes) - uses first 32 bytes",
		},
		{
			name:        "invalid raw string too short",
			input:       "tooshort",
			expectError: true,
			description: "Less than 32 characters",
		},
		{
			name:        "valid raw string longer than 32 uses first 32",
			input:       "012345678901234567890123456789012",
			expectError: false,
			description: "33 characters - uses first 32",
		},
		{
			name:        "invalid base64 wrong length",
			input:       base64.StdEncoding.EncodeToString([]byte("short")),
			expectError: true,
			description: "Base64 of less than 32 bytes",
		},
		{
			name:        "empty string",
			input:       "",
			expectError: true,
			description: "Empty encryption key",
		},
		{
			name:        "whitespace only",
			input:       "   \n\t  ",
			expectError: true,
			description: "Only whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := parseExplicitKey(tt.input)

			if tt.expectError {
				require.Error(t, err, tt.description)
				assert.Nil(t, key)
			} else {
				require.NoError(t, err, tt.description)
				assert.NotNil(t, key)
				assert.Len(t, key, 32, "Key must be exactly 32 bytes")
			}
		})
	}
}

func TestInitEncryption(t *testing.T) {
	t.Run("with explicit hex key", func(t *testing.T) {
		hexKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		cfg := &config.Config{
			EncryptionKey: hexKey,
			Environment:   "production",
		}

		InitEncryption(cfg)

		assert.NotNil(t, encryptionKey)
		assert.Len(t, encryptionKey, 32)
	})

	t.Run("with explicit base64 key", func(t *testing.T) {
		rawKey := []byte("01234567890123456789012345678901")
		b64Key := base64.StdEncoding.EncodeToString(rawKey)
		cfg := &config.Config{
			EncryptionKey: b64Key,
			Environment:   "production",
		}

		InitEncryption(cfg)

		assert.NotNil(t, encryptionKey)
		assert.Len(t, encryptionKey, 32)
	})

	t.Run("with explicit raw key", func(t *testing.T) {
		cfg := &config.Config{
			EncryptionKey: "01234567890123456789012345678901",
			Environment:   "production",
		}

		InitEncryption(cfg)

		assert.NotNil(t, encryptionKey)
		assert.Len(t, encryptionKey, 32)
	})

	t.Run("dev mode with no key", func(t *testing.T) {
		cfg := &config.Config{
			EncryptionKey: "",
			Environment:   "development",
			AgentMode:     false,
		}

		InitEncryption(cfg)

		assert.NotNil(t, encryptionKey)
		assert.Len(t, encryptionKey, 32)
	})

	t.Run("production mode without key panics", func(t *testing.T) {
		cfg := &config.Config{
			EncryptionKey: "",
			Environment:   "production",
			AgentMode:     false,
		}

		assert.Panics(t, func() {
			InitEncryption(cfg)
		}, "Should panic when no encryption key provided in production")
	})

	t.Run("invalid key panics", func(t *testing.T) {
		cfg := &config.Config{
			EncryptionKey: "tooshort",
			Environment:   "production",
		}

		assert.Panics(t, func() {
			InitEncryption(cfg)
		}, "Should panic with invalid encryption key")
	})
}

func TestEncryptDecrypt(t *testing.T) {
	hexKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	cfg := &config.Config{
		EncryptionKey: hexKey,
		Environment:   "test",
	}
	InitEncryption(cfg)

	t.Run("encrypt and decrypt simple text", func(t *testing.T) {
		plaintext := "secret password"

		encrypted, err := Encrypt(plaintext)
		require.NoError(t, err)
		assert.NotEmpty(t, encrypted)
		assert.NotEqual(t, plaintext, encrypted)

		decrypted, err := Decrypt(encrypted)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decrypted)
	})

	t.Run("encrypt and decrypt empty string", func(t *testing.T) {
		encrypted, err := Encrypt("")
		require.NoError(t, err)
		assert.Empty(t, encrypted)

		decrypted, err := Decrypt("")
		require.NoError(t, err)
		assert.Empty(t, decrypted)
	})

	t.Run("encrypt and decrypt long text", func(t *testing.T) {
		plaintext := "This is a much longer secret text with special characters: !@#$%^&*()_+-=[]{}|;:',.<>?/~`"

		encrypted, err := Encrypt(plaintext)
		require.NoError(t, err)
		assert.NotEmpty(t, encrypted)

		decrypted, err := Decrypt(encrypted)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decrypted)
	})

	t.Run("encrypt and decrypt unicode text", func(t *testing.T) {
		plaintext := "Unicode test: hello world with emoji"

		encrypted, err := Encrypt(plaintext)
		require.NoError(t, err)
		assert.NotEmpty(t, encrypted)

		decrypted, err := Decrypt(encrypted)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decrypted)
	})

	t.Run("encrypt same text twice produces different ciphertexts", func(t *testing.T) {
		plaintext := "secret"

		encrypted1, err := Encrypt(plaintext)
		require.NoError(t, err)

		encrypted2, err := Encrypt(plaintext)
		require.NoError(t, err)

		assert.NotEqual(t, encrypted1, encrypted2, "Same plaintext should produce different ciphertexts due to random nonce")

		decrypted1, err := Decrypt(encrypted1)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decrypted1)

		decrypted2, err := Decrypt(encrypted2)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decrypted2)
	})

	t.Run("decrypt invalid base64 fails", func(t *testing.T) {
		_, err := Decrypt("not-valid-base64!!!")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode base64")
	})

	t.Run("decrypt corrupted ciphertext fails", func(t *testing.T) {
		encrypted, err := Encrypt("test")
		require.NoError(t, err)

		corrupted := encrypted[:len(encrypted)-4] + "XXXX"

		_, err = Decrypt(corrupted)
		assert.Error(t, err)
	})

	t.Run("decrypt with wrong key fails", func(t *testing.T) {
		plaintext := "secret data"

		encrypted, err := Encrypt(plaintext)
		require.NoError(t, err)

		differentKey := "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
		differentCfg := &config.Config{
			EncryptionKey: differentKey,
			Environment:   "test",
		}
		InitEncryption(differentCfg)

		_, err = Decrypt(encrypted)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decrypt")
	})
}

func TestEncryptDecryptWithoutInit(t *testing.T) {
	encryptionKey = nil

	t.Run("encrypt without initialization fails", func(t *testing.T) {
		_, err := Encrypt("test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "encryption not initialized")
	})

	t.Run("decrypt without initialization fails", func(t *testing.T) {
		_, err := Decrypt("dGVzdA==")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "encryption not initialized")
	})
}

func TestDeriveDevKey(t *testing.T) {
	key := deriveDevKey()
	assert.NotNil(t, key)
	assert.Len(t, key, 32, "Derived dev key must be 32 bytes")

	key2 := deriveDevKey()
	assert.Equal(t, key, key2, "Derived dev key should be deterministic")
}
