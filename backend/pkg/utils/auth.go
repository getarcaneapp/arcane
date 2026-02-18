package utils

import "slices"

// UserHasRole reports whether the user's roles contains the given role.
func UserHasRole(roles []string, role string) bool {
	return slices.Contains(roles, role)
}
