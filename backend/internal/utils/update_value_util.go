package utils

// UpdateIfChanged updates the target value if it differs from the new value.
// It returns true if an update occurred.
// Supported types: *string, *bool, **string.
// For *string and *bool targets, if the value is a pointer of the same type,
// the update only happens if the value pointer is not nil.
func UpdateIfChanged(target any, value any) bool {
	switch t := target.(type) {
	case *string:
		if v, ok := value.(string); ok {
			if *t != v {
				*t = v
				return true
			}
		} else if v, ok := value.(*string); ok && v != nil {
			if *t != *v {
				*t = *v
				return true
			}
		}
	case *bool:
		if v, ok := value.(bool); ok {
			if *t != v {
				*t = v
				return true
			}
		} else if v, ok := value.(*bool); ok && v != nil {
			if *t != *v {
				*t = *v
				return true
			}
		}
	case **string:
		if v, ok := value.(*string); ok {
			if (*t == nil && v != nil) || (*t != nil && v == nil) || (*t != nil && v != nil && **t != *v) {
				*t = v
				return true
			}
		}
	}
	return false
}
