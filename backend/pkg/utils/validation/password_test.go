package validation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePasswordPolicy(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidatePasswordPolicy("12345678", PasswordPolicyBasic))
	require.Error(t, ValidatePasswordPolicy("1234567", PasswordPolicyBasic))

	require.NoError(t, ValidatePasswordPolicy("Abcdefghi1", PasswordPolicyStandard))
	require.Error(t, ValidatePasswordPolicy("abcdefghij1", PasswordPolicyStandard))

	require.NoError(t, ValidatePasswordPolicy("Abcdefghij1!", PasswordPolicyStrong))
	require.Error(t, ValidatePasswordPolicy("Abcdefghijk1", PasswordPolicyStrong))
	require.Error(t, ValidatePasswordPolicy("Abcdefgh1!", "bogus-policy"))
}
