package common

import (
	stderrors "errors"
	"testing"

	"emperror.dev/errors"
	"github.com/stretchr/testify/require"
)

func TestClassifyPreservesChainDetailsAndSingleStack(t *testing.T) {
	cause := errors.New("root cause")
	err := errors.WithDetails(cause, "field", "name")
	err = Classify(ErrInvalidEnvKey, err)
	err = errors.WrapIf(err, "validate input")

	require.EqualError(t, err, "validate input: root cause")
	require.ErrorIs(t, err, ErrInvalidEnvKey)
	require.ErrorIs(t, err, ErrValidation)
	require.ErrorIs(t, err, cause)
	require.Equal(t, []any{"field", "name"}, errors.GetDetails(err))

	stackCount := 0
	for current := err; current != nil; current = stderrors.Unwrap(current) {
		if _, ok := current.(interface{ StackTrace() errors.StackTrace }); ok {
			stackCount++
		}
	}
	require.Equal(t, 1, stackCount)
}
