package common

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

func TestClassifyPreservesChainAndField(t *testing.T) {
	cause := errors.New("root cause")
	var err error = &types.FieldError{Field: "name", Err: cause}
	err = Classify(ErrInvalidEnvKey, err)
	err = fmt.Errorf("validate input: %w", err)

	require.EqualError(t, err, "validate input: root cause")
	require.ErrorIs(t, err, ErrInvalidEnvKey)
	require.ErrorIs(t, err, ErrValidation)
	require.ErrorIs(t, err, cause)
	fieldErr, ok := errors.AsType[*types.FieldError](err)
	require.True(t, ok)
	require.Equal(t, "name", fieldErr.Field)
}
