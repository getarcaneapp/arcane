package utils

// UpdateIfChanged sets *target to value when they differ.
// It returns true if an update occurred.
func UpdateIfChanged[T comparable](target *T, value T) bool {
	if *target == value {
		return false
	}
	*target = value
	return true
}

// UpdateIfChangedPtr sets *target to *value when value is non-nil and differs.
// It returns true if an update occurred.
func UpdateIfChangedPtr[T comparable](target *T, value *T) bool {
	if value == nil {
		return false
	}
	return UpdateIfChanged(target, *value)
}

// UpdatePtrIfChanged replaces the pointer field *target with value when the two
// differ by pointee (nil-ness or value). It returns true if an update occurred.
func UpdatePtrIfChanged[T comparable](target **T, value *T) bool {
	if PtrEqual(*target, value) {
		return false
	}
	*target = value
	return true
}
