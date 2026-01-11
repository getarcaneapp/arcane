package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/config"
)

var encryptionKey []byte

// InitEncryption initializes the global encryption key based on configuration
func InitEncryption(cfg *config.Config) {
	if keyStr := strings.TrimSpace(cfg.EncryptionKey); keyStr != "" {
		key, err := parseExplicitKey(keyStr)
		if err != nil {
			panic(err.Error())
		}
		encryptionKey = key
		if cfg.Environment != "production" {
			slog.Info("Encryption initialized from explicit ENCRYPTION_KEY", "env", cfg.Environment, "key_length_bytes", len(encryptionKey))
		}
		return
	}

	if cfg.AgentMode {
		key, err := loadOrCreateAgentKey()
		if err != nil {
			slog.Warn("Agent encryption key load/create returned an error; using in-memory key (may not persist)", "err", err)
		}
		encryptionKey = key
		return
	}

	if cfg.Environment == "production" {
		panic("ENCRYPTION_KEY is required in production environment")
	}

	encryptionKey = deriveDevKey()
	slog.Info("Encryption initialized (derived development key)", "env", cfg.Environment, "key_length_bytes", len(encryptionKey))
}

func parseExplicitKey(in string) ([]byte, error) {
	clean := strings.TrimSpace(in)

	// Try hex decoding first (most common: openssl rand -hex 32 produces 64 hex chars)
	if b, err := hex.DecodeString(clean); err == nil && len(b) >= 32 {
		return b[:32], nil
	}

	// Try base64 decoding (standard and raw)
	if b, err := base64.StdEncoding.DecodeString(clean); err == nil && len(b) >= 32 {
		return b[:32], nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(clean); err == nil && len(b) >= 32 {
		return b[:32], nil
	}

	// Finally, try raw bytes (at least 32 ASCII characters)
	if len(clean) >= 32 {
		return []byte(clean[:32]), nil
	}

	return nil, fmt.Errorf("ENCRYPTION_KEY must be at least 32 bytes (raw/base64/hex). Provided=%d chars", len(clean))
}

func loadOrCreateAgentKey() ([]byte, error) {
	dir := agentKeyDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		slog.Warn("Failed to create directory for agent encryption key; key may not persist", "dir", dir, "err", err)
	}

	keyPath := filepath.Join(dir, "agent_encryption_key")

	if data, err := os.ReadFile(keyPath); err == nil {
		if b, err := hex.DecodeString(strings.TrimSpace(string(data))); err == nil && len(b) == 32 {
			slog.Info("Loaded agent encryption key from disk", "path", keyPath)
			return b, nil
		}
		slog.Warn("Existing agent key invalid; will generate a new key", "path", keyPath)
	} else if !os.IsNotExist(err) {
		slog.Warn("Failed to read agent encryption key; will generate a new key", "path", keyPath, "err", err)
	}

	key := make([]byte, 32)
	if _, err := crand.Read(key); err != nil {
		slog.Warn("crypto/rand failed; falling back to deterministic key derivation (not ideal)", "err", err)
		sum := sha256.Sum256([]byte("arcane-agent-key"))
		key = sum[:]
		return key, nil
	}

	if err := atomicWriteHexFile(keyPath, key, 0o600); err != nil {
		slog.Warn("Failed to persist agent encryption key; using in-memory key (will regenerate next start)", "path", keyPath, "err", err)
		return key, err
	}

	slog.Info("Generated and persisted new agent encryption key", "path", keyPath)
	return key, nil
}

func agentKeyDir() string {
	if d, err := os.UserConfigDir(); err == nil && d != "" {
		return filepath.Join(d, "arcane")
	}
	if wd, err := os.Getwd(); err == nil && wd != "" {
		return filepath.Join(wd, ".arcane")
	}
	return os.TempDir()
}

func atomicWriteHexFile(path string, key []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "agent_key_")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmp.WriteString(hex.EncodeToString(key) + "\n"); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		// proceed, but return error if rename fails later
		slog.Warn("Failed to sync agent key temp file; key may not be fully persisted", "tmp", tmpPath, "err", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	if err := os.Chmod(path, mode); err != nil {
		// Non-fatal; permissions might be too permissive
		slog.Warn("Failed to set permissions on agent key file", "path", path, "err", err)
	}
	return nil
}

func deriveDevKey() []byte {
	sum := sha256.Sum256([]byte("arcane-dev-key"))
	return sum[:]
}

// Encrypt encrypts a plaintext string using AES-GCM
func Encrypt(plaintext string) (string, error) {
	if encryptionKey == nil {
		return "", fmt.Errorf("encryption not initialized - call InitEncryption first")
	}

	if plaintext == "" {
		return "", nil
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(crand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a base64 encoded ciphertext string using AES-GCM
func Decrypt(ciphertext string) (string, error) {
	if encryptionKey == nil {
		return "", fmt.Errorf("encryption not initialized - call InitEncryption first")
	}

	if ciphertext == "" {
		return "", nil
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}
