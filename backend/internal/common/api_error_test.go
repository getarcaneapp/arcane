package common

import (
	"errors"
	"net/http"
	"testing"

	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/stretchr/testify/require"
)

func TestToAPIErrorMapsClassifiedValidationDetails(t *testing.T) {
	var err error = &base.FieldError{Field: "name", Err: errors.New("Name is required")}
	err = Classify(ErrValidation, err)

	apiErr := ToAPIError(err)

	require.Equal(t, http.StatusBadRequest, apiErr.HTTPStatus())
	require.Equal(t, APIErrorCodeValidationError, apiErr.Code)
	require.Equal(t, "Name is required", apiErr.Message)
	require.Equal(t, map[string]any{"field": "name"}, apiErr.Details)
}
