package validation

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/types/v3"
)

type (
	PasswordPolicy struct {
		MinLength        int  `json:"minLength"`
		RequireUppercase bool `json:"requireUppercase"`
		RequireLowercase bool `json:"requireLowercase"`
		RequireNumber    bool `json:"requireNumber"`
		RequireSymbol    bool `json:"requireSymbol"`
	}

	PasswordPolicyError struct {
		message string
		Policy  PasswordPolicy
	}
)

const (
	PasswordPolicyProblemType = "urn:arcane:problem:password-policy" // #nosec G101 -- public problem type, not a credential
	PasswordMinLength         = 8
)

func (e *PasswordPolicyError) Error() string {
	return e.message
}

func (p PasswordPolicy) Validate(password string) error {
	minLength := max(p.MinLength, PasswordMinLength)
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

	rules := []struct {
		label     string
		required  bool
		satisfied bool
	}{
		{label: "an uppercase letter", required: p.RequireUppercase, satisfied: hasUpper},
		{label: "a lowercase letter", required: p.RequireLowercase, satisfied: hasLower},
		{label: "a number", required: p.RequireNumber, satisfied: hasDigit},
		{label: "a symbol", required: p.RequireSymbol, satisfied: hasSymbol},
	}
	valid := length >= minLength
	labels := make([]string, 0, len(rules))
	for _, rule := range rules {
		if rule.required {
			labels = append(labels, rule.label)
			valid = valid && rule.satisfied
		}
	}
	if valid {
		return nil
	}

	message := fmt.Sprintf("password must be at least %d characters", minLength)
	if len(labels) > 2 {
		message = fmt.Sprintf("%s and include %s, and %s", message, strings.Join(labels[:len(labels)-1], ", "), labels[len(labels)-1])
	} else if len(labels) > 0 {
		message = fmt.Sprintf("%s and include %s", message, strings.Join(labels, " and "))
	}
	return &PasswordPolicyError{
		message: message,
		Policy: PasswordPolicy{
			MinLength:        minLength,
			RequireUppercase: p.RequireUppercase,
			RequireLowercase: p.RequireLowercase,
			RequireNumber:    p.RequireNumber,
			RequireSymbol:    p.RequireSymbol,
		},
	}
}

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
		//nolint:staticcheck // Preserve the existing error message.
		return common.Classify(common.ErrValidation,
			&types.FieldError{
				Field: missingFields[0],
				Err: fmt.Errorf("Changing %s requires re-entering the %s",
					targetName,
					missingFields[0]),
			})
	}

	return common.NewAPIErrorWithDetails(
		fmt.Sprintf("Changing %s requires updating all stored credentials", targetName),
		common.APIErrorCodeValidationError,
		http.StatusBadRequest,
		map[string]any{"fields": missingFields},
	)
}
