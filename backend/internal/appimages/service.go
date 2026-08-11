package appimages

import (
	"embed"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/image"
	settingstypes "github.com/getarcaneapp/arcane/types/v2/settings"
)

type ApplicationImagesService struct {
	mu              sync.RWMutex
	imageData       map[string][]byte
	mimeTypes       map[string]string
	settingsService *settings.SettingsService
}

func NewApplicationImagesService(embeddedFS embed.FS, settingsService *settings.SettingsService) *ApplicationImagesService {
	service := &ApplicationImagesService{
		imageData:       make(map[string][]byte),
		mimeTypes:       make(map[string]string),
		settingsService: settingsService,
	}

	imageDir := "images"
	entries, err := fs.ReadDir(embeddedFS, imageDir)
	if err != nil {
		return service
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		ext := strings.ToLower(filepath.Ext(filename))
		nameWithoutExt := strings.TrimSuffix(filename, ext)

		data, err := embeddedFS.ReadFile(filepath.Join(imageDir, filename))
		if err != nil {
			continue
		}

		extWithoutDot := strings.TrimPrefix(ext, ".")
		mimeType := image.GetImageMimeType(extWithoutDot)
		if mimeType == "" {
			continue
		}

		service.imageData[nameWithoutExt] = data
		service.mimeTypes[nameWithoutExt] = mimeType
	}

	return service
}

func (s *ApplicationImagesService) GetImageWithColor(name string, colorOverride string, loop bool) ([]byte, string, error) {
	s.mu.RLock()
	data, ok := s.imageData[name]
	mimeType := s.mimeTypes[name]
	s.mu.RUnlock()

	if !ok {
		return nil, "", errors.Errorf("image '%s' not found", name)
	}

	// Apply dynamic color replacement for logo SVGs
	if IsLogoVariant(name) && mimeType == "image/svg+xml" {
		data = s.applyAccentColorToSVGInternal(data, colorOverride)
	}

	// The animated mark ships both a one-shot and a looping keyframe set; loop
	// mode swaps the animation shorthand so the trace repeats indefinitely.
	if loop && name == "logo-animated" {
		data = []byte(strings.Replace(string(data),
			"animation: traceDraw 1.7s ease both;",
			"animation: traceDrawLoop 2.6s ease infinite;", 1))
	}

	return data, mimeType, nil
}

// IsLogoVariant reports whether the image name is one of the accent-colorable logo SVGs.
func IsLogoVariant(name string) bool {
	switch name {
	case "logo", "logo-full", "logo-animated", "logo-full-animated":
		return true
	}
	return false
}

func (s *ApplicationImagesService) applyAccentColorToSVGInternal(svgData []byte, colorOverride string) []byte {
	accentColor := settingstypes.DefaultAccentColor
	if settingstypes.SafeAccentColor.MatchString(colorOverride) {
		accentColor = colorOverride
	}

	svgStr := string(svgData)
	svgStr = strings.ReplaceAll(svgStr, "fill:#6D28D9", "fill:"+accentColor)
	svgStr = strings.ReplaceAll(svgStr, "fill:#6d28d9", "fill:"+accentColor)
	svgStr = strings.ReplaceAll(svgStr, "stroke:#6D28D9", "stroke:"+accentColor)
	svgStr = strings.ReplaceAll(svgStr, "stroke:#6d28d9", "stroke:"+accentColor)
	return []byte(svgStr)
}
