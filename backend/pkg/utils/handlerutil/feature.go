package handlerutil

import (
	"fmt"
	"net/http"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/types/v2/features"
)

// FeatureDisabledError identifies a disabled runtime feature in problem details.
func FeatureDisabledError(id features.ID) *features.DisabledError {
	return &features.DisabledError{
		Status:  http.StatusForbidden,
		Title:   http.StatusText(http.StatusForbidden),
		Detail:  fmt.Sprintf("feature %s is disabled", id),
		Code:    string(common.APIErrorCodeFeatureDisabled),
		Feature: id,
	}
}
