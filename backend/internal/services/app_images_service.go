package services

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"github.com/getarcaneapp/arcane/backend/pkg/utils/image"
	"github.com/getarcaneapp/arcane/types/settings"
)

type ApplicationImagesService struct {
	mu              sync.RWMutex
	imageData       map[string][]byte
	mimeTypes       map[string]string
	settingsService *SettingsService
}

func NewApplicationImagesService(embeddedFS embed.FS, settingsService *SettingsService) *ApplicationImagesService {
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

func (s *ApplicationImagesService) GetImageWithColor(name string, colorOverride string) ([]byte, string, error) {
	s.mu.RLock()
	data, ok := s.imageData[name]
	mimeType := s.mimeTypes[name]
	s.mu.RUnlock()

	if !ok {
		return nil, "", fmt.Errorf("image '%s' not found", name)
	}

	// Apply dynamic color replacement for logo SVGs
	if (name == "logo" || name == "logo-full") && mimeType == "image/svg+xml" {
		data = s.applyAccentColorToSVGInternal(data, colorOverride)
	}

	return data, mimeType, nil
}

func (s *ApplicationImagesService) applyAccentColorToSVGInternal(svgData []byte, colorOverride string) []byte {
	accentColor := ""
	if settings.SafeAccentColor.MatchString(colorOverride) {
		accentColor = colorOverride
	} else if s.settingsService != nil {
		if cfg := s.settingsService.GetSettingsConfig(); cfg != nil {
			stored := cfg.AccentColor.Value
			if stored != "" && stored != "default" && settings.SafeAccentColor.MatchString(stored) {
				accentColor = stored
			}
		}
	}
	if accentColor == "" {
		accentColor = settings.DefaultAccentColor
	}

	svgStr := string(svgData)
	svgStr = strings.ReplaceAll(svgStr, "fill:#6D28D9", "fill:"+accentColor)
	svgStr = strings.ReplaceAll(svgStr, "fill:#6d28d9", "fill:"+accentColor)
	return []byte(svgStr)
}
