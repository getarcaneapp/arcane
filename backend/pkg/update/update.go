package update

import (
	"fmt"
	"slices"

	"github.com/samber/mo"

	"kit.getarcane.app/sys/crypto"
)

func ApplyChanged[T comparable](target *T, value mo.Option[T]) bool {
	next, ok := value.Get()
	if !ok || *target == next {
		return false
	}

	*target = next
	return true
}

func ApplySliceChanged[S ~[]E, E comparable](target *S, value mo.Option[S]) bool {
	next, ok := value.Get()
	if !ok || slices.Equal(*target, next) {
		return false
	}

	*target = next
	return true
}

func ApplyNullable[T comparable](target **T, value mo.Option[T]) bool {
	if mo.PointerToOption(*target) == value {
		return false
	}

	*target = value.ToPointer()
	return true
}

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
