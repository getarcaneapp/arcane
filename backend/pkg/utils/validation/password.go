package validation

import (
	"unicode"

	"emperror.dev/errors"
)

const (
	PasswordPolicyBasic    = "basic"
	PasswordPolicyStandard = "standard"
	PasswordPolicyStrong   = "strong"
)

// ValidatePasswordPolicy checks a password against the named policy tier.
// Unknown policy values are treated as strong so a corrupted setting fails closed.
func ValidatePasswordPolicy(password, policy string) error {
	var length int
	var hasUpper, hasLower, hasDigit, hasSymbol bool
	for _, r := range password {
		length++
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsSpace(r):
			hasSymbol = true
		}
	}

	switch policy {
	case PasswordPolicyBasic:
		if length < 8 {
			return errors.New("password must be at least 8 characters")
		}
	case PasswordPolicyStandard:
		if length < 10 || !hasUpper || !hasLower || !hasDigit {
			return errors.New("password must be at least 10 characters and include an uppercase letter, a lowercase letter, and a number")
		}
	default:
		if length < 12 || !hasUpper || !hasLower || !hasDigit || !hasSymbol {
			return errors.New("password must be at least 12 characters and include an uppercase letter, a lowercase letter, a number, and a symbol")
		}
	}
	return nil
}
