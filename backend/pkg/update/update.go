package update

import (
	"fmt"
	"slices"

	"github.com/samber/mo"

	"kit.getarcane.app/sys/crypto"
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
