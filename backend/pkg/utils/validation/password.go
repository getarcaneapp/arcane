package validation

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/types/v2/base"
)

const (
	PasswordPolicyBasic    = "basic"
	PasswordPolicyStandard = "standard"
	PasswordPolicyStrong   = "strong"

	passwordPolicyProblemTypePrefix = "urn:arcane:problem:password-policy:" // #nosec G101 -- public problem type, not a credential
)

// PasswordPolicyProblemType returns the stable problem type for a policy tier.
// Unknown policy values mirror validation behavior and fall back to strong.
func PasswordPolicyProblemType(policy string) string {
	switch policy {
	case PasswordPolicyBasic, PasswordPolicyStandard:
	default:
		policy = PasswordPolicyStrong
	}
	return passwordPolicyProblemTypePrefix + policy
}

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

// ValidateCredentialTargetChange prevents stored credentials from being reused
// against a changed target unless the update explicitly handles them.
func ValidateCredentialTargetChange(
	targetName string,
	currentTarget string,
	nextTarget *string,
	normalize func(string) string,
	storedCredentials map[string]bool,
	updatedCredentials map[string]bool,
) error {
	if nextTarget == nil {
		return nil
	}
	if strings.TrimSpace(*nextTarget) == "" {
		return nil
	}

	if normalize == nil {
		normalize = func(value string) string { return value }
	}
	if normalize(currentTarget) == normalize(*nextTarget) {
		return nil
	}

	missingFields := make([]string, 0, len(storedCredentials))
	for field, stored := range storedCredentials {
		if stored && !updatedCredentials[field] {
			missingFields = append(missingFields, field)
		}
	}
	if len(missingFields) == 0 {
		return nil
	}

	slices.Sort(missingFields)
	if len(missingFields) == 1 {
		return common.Classify(common.ErrValidation, &base.FieldError{Field: missingFields[0], Err: fmt.Errorf("Changing %s requires re-entering the %s", targetName, missingFields[0])}) //nolint:staticcheck // Preserve the existing error message.
	}

	return common.NewAPIErrorWithDetails(
		fmt.Sprintf("Changing %s requires updating all stored credentials", targetName),
		common.APIErrorCodeValidationError,
		http.StatusBadRequest,
		map[string]any{"fields": missingFields},
	)
}
