package handlers

import (
	"context"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/resources"
	"github.com/stretchr/testify/require"
)

func TestGetPWAIconReturnsPNGFromEmbeddedBackendAssets(t *testing.T) {
	handler := &AppImagesHandler{
		appImagesService: services.NewApplicationImagesService(resources.FS, nil),
	}

	filenames := []string{
		"icon-72x72.png",
		"icon-96x96.png",
		"icon-128x128.png",
		"icon-144x144.png",
		"icon-152x152.png",
		"icon-192x192.png",
		"icon-384x384.png",
		"icon-512x512.png",
	}

	for _, filename := range filenames {
		t.Run(filename, func(t *testing.T) {
			resp, err := handler.GetPWAIcon(context.Background(), &GetPWAIconInput{
				Filename: filename,
			})
			require.NoError(t, err)
			require.Equal(t, "image/png", resp.ContentType)
			require.Equal(t, "public, max-age=900, stale-while-revalidate=86400", resp.CacheControl)
			require.NotEmpty(t, resp.Body)
		})
	}
}

func TestGetPWAIconRejectsNonPWAAssets(t *testing.T) {
	handler := &AppImagesHandler{
		appImagesService: services.NewApplicationImagesService(resources.FS, nil),
	}

	resp, err := handler.GetPWAIcon(context.Background(), &GetPWAIconInput{
		Filename: "logo.png",
	})
	require.Nil(t, resp)
	require.Error(t, err)

	statusErr := huma.Error400BadRequest("invalid PWA icon filename")
	require.Equal(t, statusErr.GetStatus(), err.(interface{ GetStatus() int }).GetStatus())
}
