package handlers

import (
	"context"
	"strings"
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

func TestGetLogoRejectsXSSPayloadInColorParam(t *testing.T) {
	handler := &AppImagesHandler{
		appImagesService: services.NewApplicationImagesService(resources.FS, nil),
	}

	resp, err := handler.GetLogo(context.Background(), &GetLogoInput{
		Color: "red}</style><script>alert(1)</script><style>x{",
	})
	require.NoError(t, err)
	require.Equal(t, "image/svg+xml", resp.ContentType)
	require.Equal(t, "nosniff", resp.XContentTypeOptions)

	body := string(resp.Body)
	require.NotContains(t, body, "<script>", "injected <script> tag must not appear in response body")
	require.NotContains(t, body, "</style><script", "style block must not be broken out of")
	require.Contains(t, body, "fill:oklch(0.606 0.25 292.717)", "should fall back to default accent color")
}

func TestGetLogoAcceptsValidOklchOverride(t *testing.T) {
	handler := &AppImagesHandler{
		appImagesService: services.NewApplicationImagesService(resources.FS, nil),
	}

	resp, err := handler.GetLogo(context.Background(), &GetLogoInput{
		Color: "oklch(0.65 0.2 150)",
	})
	require.NoError(t, err)
	require.Contains(t, string(resp.Body), "fill:oklch(0.65 0.2 150)")
}

func TestGetLogoAcceptsValidHexOverride(t *testing.T) {
	handler := &AppImagesHandler{
		appImagesService: services.NewApplicationImagesService(resources.FS, nil),
	}

	resp, err := handler.GetLogo(context.Background(), &GetLogoInput{
		Color: "#abcdef",
	})
	require.NoError(t, err)
	require.Contains(t, string(resp.Body), "fill:#abcdef")
}

func TestGetLogoSetsNoSniffHeader(t *testing.T) {
	handler := &AppImagesHandler{
		appImagesService: services.NewApplicationImagesService(resources.FS, nil),
	}

	resp, err := handler.GetLogo(context.Background(), &GetLogoInput{})
	require.NoError(t, err)
	require.Equal(t, "nosniff", resp.XContentTypeOptions)
	require.True(t, strings.HasPrefix(resp.ContentType, "image/svg"))
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
