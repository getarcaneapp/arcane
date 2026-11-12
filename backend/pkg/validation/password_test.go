package validation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPasswordPolicyValidate(t *testing.T) {
	t.Parallel()

	strong := PasswordPolicy{
		MinLength:        12,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireNumber:    true,
		RequireSymbol:    true,
	}

	tests := []struct {
		name     string
		password string
		wantErr  string
		policy   PasswordPolicy
	}{
		{name: "length only passes", policy: PasswordPolicy{MinLength: 8}, password: "12345678"},
		{name: "length only too short", policy: PasswordPolicy{MinLength: 8}, password: "1234567", wantErr: "password must be at least 8 characters"},
		{name: "min length below floor uses eight", policy: PasswordPolicy{MinLength: 3}, password: "1234567", wantErr: "password must be at least 8 characters"},
		{name: "missing min length uses eight", policy: PasswordPolicy{}, password: "12345678"},
		{name: "strong passes", policy: strong, password: "Abcdefghij1!"},
		{
			name:     "strong missing symbol",
			policy:   strong,
			password: "Abcdefghijk1",
			wantErr:  "password must be at least 12 characters and include an uppercase letter, a lowercase letter, a number, and a symbol",
		},
		{name: "space counts as symbol", policy: strong, password: "Abcdefghij1 "},
		{
			name: "two rules listed with and",
			policy: PasswordPolicy{
				MinLength:        10,
				RequireUppercase: true,
				RequireNumber:    true,
			},
			password: "abcdefghij1",
			wantErr:  "password must be at least 10 characters and include an uppercase letter and a number",
		},
		{
			name: "single rule",
			policy: PasswordPolicy{
				MinLength:     8,
				RequireSymbol: true,
			},
			password: "abcdefgh",
			wantErr:  "password must be at least 8 characters and include a symbol",
		},
		{
			name: "long enough but missing rule fails",
			policy: PasswordPolicy{
				MinLength:        8,
				RequireLowercase: true,
			},
			password: "ABCDEFGHIJ",
			wantErr:  "password must be at least 8 characters and include a lowercase letter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.policy.Validate(tt.password)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tt.wantErr)
		})
	}
}
