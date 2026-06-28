package handlers

import (
	"context"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUploadFileReturnsBadRequestWhenNoFileProvided(t *testing.T) {
	h := &volumeHandler{volumeService: &services.VolumeService{}}

	_, err := h.uploadFileInternal(adminTestContextInternal(), &uploadFileInput{
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
	h := &volumeHandler{volumeService: &services.VolumeService{}}

	ctx := context.WithValue(adminTestContextInternal(), humamw.ContextKeyCurrentUser, &models.User{BaseModel: models.BaseModel{ID: "u-1"}})

	_, err := h.uploadAndRestoreInternal(ctx, &uploadAndRestoreInput{
		EnvironmentID: "0",
		VolumeName:    "vol-1",
		RawBody:       multipart.Form{},
	})

	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
}
