package restore

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/upload"
)

func TestUploadAndRestoreReturnsNotFoundForUnknownSession(t *testing.T) {
	h := &Handler{service: &Service{}, uploadService: upload.NewUploadService(nil)}

	ctx := context.WithValue(t.Context(), types.CurrentUserContextKey{}, &types.Actor{ID: "u-1"})

	_, err := h.UploadAndRestore(ctx, &UploadAndRestoreInput{
		EnvironmentID: "0",
		VolumeName:    "vol-1",
		Body:          types.UploadConsumeRequest{UploadID: "missing"},
	})

	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusNotFound, statusErr.GetStatus())
}
