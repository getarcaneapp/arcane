package common

import (
	"net/http"
	"testing"

	"emperror.dev/errors"
	"github.com/stretchr/testify/require"
)

func TestToAPIErrorMapsClassifiedValidationDetails(t *testing.T) {
	err := errors.WithDetails(errors.New("Name is required"), "field", "name")
	err = Classify(ErrValidation, err)

	apiErr := ToAPIError(err)

	require.Equal(t, http.StatusBadRequest, apiErr.HTTPStatus())
	require.Equal(t, APIErrorCodeValidationError, apiErr.Code)
	require.Equal(t, "Name is required", apiErr.Message)
	require.Equal(t, map[string]any{"field": "name"}, apiErr.Details)
}
