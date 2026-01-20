package services

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"github.com/getarcaneapp/arcane/backend/internal/utils/image"
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
		data = s.applyAccentColorToSVG(data, colorOverride)
	}

	return data, mimeType, nil
}

func (s *ApplicationImagesService) applyAccentColorToSVG(svgData []byte, colorOverride string) []byte {
	var accentColor string

	// Use color override if provided, otherwise get from settings
	if colorOverride != "" {
		accentColor = colorOverride
	} else {
		cfg := s.settingsService.GetSettingsConfig()
		if cfg != nil {
			accentColor = cfg.AccentColor.Value
		}
	}

	if accentColor == "" || accentColor == "default" {
		accentColor = "oklch(0.606 0.25 292.717)" // Default purple
	}

	// Replace the hardcoded purple color with the accent color
	// The SVG uses .st0{fill:#6d28d9} which we'll replace (case-insensitive)
	svgStr := string(svgData)

	// Replace hex color in style tag (handle both cases)
	svgStr = strings.ReplaceAll(svgStr, "fill:#6D28D9", fmt.Sprintf("fill:%s", accentColor))
	svgStr = strings.ReplaceAll(svgStr, "fill:#6d28d9", fmt.Sprintf("fill:%s", accentColor))

	return []byte(svgStr)
}
