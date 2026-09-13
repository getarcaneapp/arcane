package handlerutil

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/features"
	"github.com/stretchr/testify/require"
)

func TestFeatureDisabledError(t *testing.T) {
	tests := []struct {
		name            string
		contentType     string
		wantContentType string
	}{
		{name: "JSON problem details", contentType: "application/json", wantContentType: "application/problem+json"},
		{name: "CBOR problem details", contentType: "application/cbor", wantContentType: "application/problem+cbor"},
		{name: "other content type preserved", contentType: "text/plain", wantContentType: "text/plain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := FeatureDisabledError(features.VulnerabilityManagement)
			var statusError huma.StatusError = err
			require.Equal(t, http.StatusForbidden, statusError.GetStatus())
			require.Equal(t, "feature vulnerabilityManagement is disabled", err.Error())
			require.Equal(t, tt.wantContentType, err.ContentType(tt.contentType))
			encoded, marshalErr := json.Marshal(err)
			require.NoError(t, marshalErr)
			require.JSONEq(t, `{"status":403,"title":"Forbidden","detail":"feature vulnerabilityManagement is disabled","code":"feature_disabled","feature":"vulnerabilityManagement"}`, string(encoded))
		})
	}
}
