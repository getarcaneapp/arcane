package handlers

import (
	"context"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUploadFileReturnsBadRequestWhenNoFileProvided(t *testing.T) {
	h := &VolumeHandler{volumeService: &services.VolumeService{}}

	_, err := h.UploadFile(context.Background(), &UploadFileInput{
		EnvironmentID: "0",
		VolumeName:    "vol-1",
		Path:          "/",
		RawBody:       multipart.Form{},
	})

	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
}

func TestUploadAndRestoreReturnsBadRequestWhenNoFileProvided(t *testing.T) {
	h := &VolumeHandler{volumeService: &services.VolumeService{}}

	ctx := context.WithValue(context.Background(), humamw.ContextKeyCurrentUser, &models.User{BaseModel: models.BaseModel{ID: "u-1"}})

	_, err := h.UploadAndRestore(ctx, &UploadAndRestoreInput{
		EnvironmentID: "0",
		VolumeName:    "vol-1",
		RawBody:       multipart.Form{},
	})

	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
}
