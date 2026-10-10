package appimages

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	settingstypes "github.com/getarcaneapp/arcane/types/v2/settings"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
)

type appImage struct {
	data     []byte
	mimeType string
}

type ApplicationImagesService struct {
	standard        map[string]appImage
	development     map[string]appImage
	settingsService *settings.SettingsService
}

func NewApplicationImagesService(embeddedFS embed.FS, settingsService *settings.SettingsService) *ApplicationImagesService {
	service := &ApplicationImagesService{
		standard:        make(map[string]appImage),
		development:     make(map[string]appImage),
		settingsService: settingsService,
	}

	// Only non-stable builds load the development artwork; its 512px icon also replaces the standalone marks.
	sets := map[string]map[string]appImage{"images": service.standard}
	if !isStableVersion(config.Version) {
		sets["images/development"] = service.development
	}
	aliases := map[string][]string{"images/development/icon-512x512": {"logo", "logo-animated"}}

	for dir, images := range sets {
		entries, err := fs.ReadDir(embeddedFS, dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			filename := entry.Name()
			ext := strings.ToLower(filepath.Ext(filename))
			mimeType := kit.GetImageMimeType(strings.TrimPrefix(ext, "."))
			if entry.IsDir() || mimeType == "" {
				continue
			}

			data, readErr := embeddedFS.ReadFile(path.Join(dir, filename))
			if readErr != nil {
				continue
			}

			name := strings.TrimSuffix(filename, ext)
			for _, key := range append([]string{name}, aliases[path.Join(dir, name)]...) {
				images[key] = appImage{data: data, mimeType: mimeType}
			}
		}
	}

	return service
}

func (s *ApplicationImagesService) GetImageWithColor(name, colorOverride string, loop bool) ([]byte, string, error) {
	img, ok := s.standard[name]
	if dev, hasDev := s.development[name]; hasDev && s.settingsService.GetSettingsConfig().DevelopmentBrandingEnabled.IsTrue() {
		img, ok = dev, true
	}
	if !ok {
		return nil, "", fmt.Errorf("image '%s' not found", name)
	}

	data, mimeType := img.data, img.mimeType
	if !IsLogoVariant(name) || mimeType != "image/svg+xml" {
		return data, mimeType, nil
	}

	accentColor := kit.Ternary(settingstypes.SafeAccentColor.MatchString(colorOverride), colorOverride, settingstypes.DefaultAccentColor)
	svg := strings.NewReplacer(
		"fill:#6D28D9", "fill:"+accentColor,
		"fill:#6d28d9", "fill:"+accentColor,
		"stroke:#6D28D9", "stroke:"+accentColor,
		"stroke:#6d28d9", "stroke:"+accentColor,
	).Replace(string(data))

	// Loop mode swaps the animated mark's one-shot trace for its looping keyframes.
	if loop && name == "logo-animated" {
		svg = strings.Replace(svg, "animation: traceDraw 1.7s ease both;", "animation: traceDrawLoop 2.6s ease infinite;", 1)
	}
	data = []byte(svg)

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
