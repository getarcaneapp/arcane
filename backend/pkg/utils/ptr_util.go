package utils

import "strings"

// Deref returns the value p points to, or T's zero value when p is nil.
func Deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// PtrEqual reports whether a and b are both nil or point to equal values.
func PtrEqual[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// CopyPtr returns a pointer to a shallow copy of *p, or nil when p is nil.
func CopyPtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func StringPtrFromTrimmed(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
