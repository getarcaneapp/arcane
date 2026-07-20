package validation

import (
	"fmt"
	"slices"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
)

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
		return &models.ValidationError{
			Field:   missingFields[0],
			Message: fmt.Sprintf("Changing %s requires re-supplying or clearing the %s", targetName, missingFields[0]),
		}
	}

	return models.NewValidationError(
		fmt.Sprintf("Changing %s requires updating all stored credentials", targetName),
		map[string]any{"fields": missingFields},
	)
}
