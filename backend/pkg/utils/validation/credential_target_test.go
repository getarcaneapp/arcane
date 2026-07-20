package validation

import (
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateCredentialTargetChange(t *testing.T) {
	normalizeHost := func(value string) string {
		return strings.TrimPrefix(strings.ToLower(value), "https://")
	}

	tests := []struct {
		name               string
		currentTarget      string
		nextTarget         *string
		normalize          func(string) string
		storedCredentials  map[string]bool
		updatedCredentials map[string]bool
		wantField          string
		wantFields         []string
	}{
		{
			name:              "omitted target",
			currentTarget:     "registry.example.com",
			storedCredentials: map[string]bool{"token": true},
		},
		{
			name:              "identical target",
			currentTarget:     "registry.example.com",
			nextTarget:        new("registry.example.com"),
			storedCredentials: map[string]bool{"token": true},
		},
		{
			name:              "normalized target alias",
			currentTarget:     "https://registry.example.com",
			nextTarget:        new("REGISTRY.EXAMPLE.COM"),
			normalize:         normalizeHost,
			storedCredentials: map[string]bool{"token": true},
		},
		{
			name:          "changed target without stored credentials",
			currentTarget: "registry.example.com",
			nextTarget:    new("registry.example.net"),
		},
		{
			name:               "changed target with replacement",
			currentTarget:      "registry.example.com",
			nextTarget:         new("registry.example.net"),
			storedCredentials:  map[string]bool{"token": true},
			updatedCredentials: map[string]bool{"token": true},
		},
		{
			name:               "changed target with clearing",
			currentTarget:      "git.example.com/repo",
			nextTarget:         new("git.example.net/repo"),
			storedCredentials:  map[string]bool{"sshKey": true},
			updatedCredentials: map[string]bool{"sshKey": true},
		},
		{
			name:              "changed target with one missing credential",
			currentTarget:     "registry.example.com",
			nextTarget:        new("registry.example.net"),
			storedCredentials: map[string]bool{"token": true},
			wantField:         "token",
		},
		{
			name:              "missing fields are deterministic",
			currentTarget:     "git.example.com/repo",
			nextTarget:        new("git.example.net/repo"),
			storedCredentials: map[string]bool{"token": true, "sshKey": true},
			wantFields:        []string{"sshKey", "token"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCredentialTargetChange(
				"credential target",
				tt.currentTarget,
				tt.nextTarget,
				tt.normalize,
				tt.storedCredentials,
				tt.updatedCredentials,
			)

			switch {
			case tt.wantField != "":
				var validationErr *models.ValidationError
				require.ErrorAs(t, err, &validationErr)
				assert.Equal(t, tt.wantField, validationErr.Field)
				assert.Equal(t, "Changing credential target requires re-supplying or clearing the token", validationErr.Message)
			case len(tt.wantFields) > 0:
				var apiErr *models.APIError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, models.APIErrorCodeValidationError, apiErr.Code)
				assert.Equal(t, map[string]any{"fields": tt.wantFields}, apiErr.Details)
			default:
				require.NoError(t, err)
			}
		})
	}
}
