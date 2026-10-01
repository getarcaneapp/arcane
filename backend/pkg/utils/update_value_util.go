package utils

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/samber/mo"
	"go.getarcane.app/sys/crypto"
)

// ApplyChanged updates target when value is present and differs from the current value.
func ApplyChanged[T comparable](target *T, value mo.Option[T]) bool {
	next, ok := value.Get()
	if !ok || *target == next {
		return false
	}

	*target = next
	return true
}

// ApplySliceChanged updates target when value is present and its elements differ
// from the current slice. Nil and empty slices are treated as equal.
func ApplySliceChanged[S ~[]E, E comparable](target *S, value mo.Option[S]) bool {
	next, ok := value.Get()
	if !ok || slices.Equal(*target, next) {
		return false
	}

	*target = next
	return true
}

// ApplyNullable updates target to the optional value when it differs from the current value.
func ApplyNullable[T comparable](target **T, value mo.Option[T]) bool {
	if mo.PointerToOption(*target) == value {
		return false
	}

	*target = value.ToPointer()
	return true
}

// ApplyEncrypted stores plaintext encrypted in field only when it differs from
// the stored value, so unchanged credentials keep their ciphertext. An empty
// plaintext clears the field. A stored value that no longer decrypts (for
// example after a key change) is replaced so the source of truth wins.
func ApplyEncrypted(field *string, plaintext string) (bool, error) {
	if plaintext == "" {
		if *field == "" {
			return false, nil
		}
		*field = ""
		return true, nil
	}
	if *field != "" {
		if current, err := crypto.Decrypt(*field); err == nil && current == plaintext {
			return false, nil
		}
	}
	encrypted, err := crypto.Encrypt(plaintext)
	if err != nil {
		return false, fmt.Errorf("failed to encrypt credential: %w", err)
	}
	*field = encrypted
	return true, nil
}

// LookupEnvOrFile is os.LookupEnv with Docker secret support: when NAME__FILE
// or NAME_FILE (checked in that order) names a readable file, its trimmed
// contents stand in for NAME. An unreadable file falls back to NAME.
// The booleans report whether a value was found and whether it came from a file.
func LookupEnvOrFile(name string) (value string, found, fromFile bool) {
	for _, suffix := range []string{"__FILE", "_FILE"} {
		filePath := os.Getenv(name + suffix)
		if filePath == "" {
			continue
		}

		// Secret paths are arbitrary host paths, so there is no acfs root to confine them to.
		content, err := os.ReadFile(filePath) //nolint:gosec // path intentionally comes from a *_FILE env var
		if err != nil {
			slog.Warn("Failed to read secret file, falling back to direct env var", "env", name+suffix, "error", err)
			break
		}

		return strings.TrimSpace(string(content)), true, true
	}

	value, found = os.LookupEnv(name)
	return value, found, false
}

// NormalizePasskeyAssertionExtensions drops an unrequested appid=false output, which Safari
// reports even though it means the legacy AppID was not used.
func NormalizePasskeyAssertionExtensions(session protocol.SessionExtensions, assertion *protocol.ParsedCredentialAssertionData) {
	if assertion == nil {
		return
	}
	appID := assertion.ClientExtensionResults.AppID
	if appID == nil || *appID {
		return
	}
	if session.AppID != "" || slices.Contains(session.Requested, protocol.ExtensionAppID) {
		return
	}
	assertion.ClientExtensionResults.AppID = nil
}
