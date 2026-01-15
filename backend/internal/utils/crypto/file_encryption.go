package crypto

import (
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// EncryptToFile encrypts content and writes to file.
func EncryptToFile(content string, filePath string) error {
	encrypted, err := Encrypt(content)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, []byte(encrypted), 0o400)
}

// DecryptFromFile reads encrypted file and returns decrypted content.
func DecryptFromFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return Decrypt(string(data))
}

// DecryptToTempFile decrypts an encrypted file to a temp file, returns temp path.
func DecryptToTempFile(encryptedPath, tempDir string) (string, error) {
	content, err := DecryptFromFile(encryptedPath)
	if err != nil {
		return "", err
	}

	tmpPath := filepath.Join(tempDir, "secret-"+uuid.NewString())
	tmpFile, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(content); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}

	return tmpPath, nil
}
