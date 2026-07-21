package utils

import "github.com/samber/mo"

// ApplyChanged updates target when value is present and differs from the current value.
func ApplyChanged[T comparable](target *T, value mo.Option[T]) bool {
	next, ok := value.Get()
	if !ok || *target == next {
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
