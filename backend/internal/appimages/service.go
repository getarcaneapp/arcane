package appimages

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"gorm.io/gorm"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/types/v3"
)

type (
	appImage struct {
		mimeType string
		data     []byte
	}

	ImageSlot string

	imageSlotSpec struct {
		hashKey   string
		mimeTypes []string
	}

	customImageTarget struct {
		slot ImageSlot
		size int
	}

	ApplicationImagesService struct {
		standard        map[string]appImage
		development     map[string]appImage
		settingsService *settings.SettingsService
		db              *database.DB
	}
)

const (
	ImageSlotMark    ImageSlot = "mark"
	ImageSlotFull    ImageSlot = "full"
	ImageSlotFavicon ImageSlot = "favicon"
	ImageSlotPWA     ImageSlot = "pwa"

	ImageMaxBytes   = 2 << 20
	pwaIconMinSize  = 512
	pwaIconMaxSize  = 4096
	svgMimeType     = "image/svg+xml"
	pngMimeType     = "image/png"
	iconMimeType    = "image/x-icon"
	jpegMimeType    = "image/jpeg"
	webpMimeType    = "image/webp"
	customImageSize = 0
)

var (
	ErrImageSlotInvalid = common.Classify(common.ErrValidation, errors.New("image slot must be mark, full, favicon or pwa"))
	ErrImageTooLarge    = common.Classify(common.ErrValidation, fmt.Errorf("image must be %d MB or smaller", ImageMaxBytes>>20))
	ErrPWAIconInvalid   = common.Classify(common.ErrValidation, fmt.Errorf("PWA icon must be a square PNG between %[1]dx%[1]d and %[2]dx%[2]d pixels", pwaIconMinSize, pwaIconMaxSize))

	imageSlots = map[ImageSlot]imageSlotSpec{
		ImageSlotMark: {
			hashKey:   "customLogoHash",
			mimeTypes: []string{pngMimeType, jpegMimeType, webpMimeType, svgMimeType},
		},
		ImageSlotFull: {
			hashKey:   "customLogoFullHash",
			mimeTypes: []string{pngMimeType, jpegMimeType, webpMimeType, svgMimeType},
		},
		ImageSlotFavicon: {
			hashKey:   "customFaviconHash",
			mimeTypes: []string{pngMimeType, svgMimeType, iconMimeType},
		},
		ImageSlotPWA: {
			hashKey:   "customPwaIconHash",
			mimeTypes: []string{pngMimeType},
		},
	}

	pwaIconSizes = []int{72, 96, 128, 144, 152, 192, 384, 512}

	customImageTargets = map[string]customImageTarget{
		"logo":               {slot: ImageSlotMark, size: customImageSize},
		"logo-animated":      {slot: ImageSlotMark, size: customImageSize},
		"logo-full":          {slot: ImageSlotFull, size: customImageSize},
		"logo-full-animated": {slot: ImageSlotFull, size: customImageSize},
		"favicon":            {slot: ImageSlotFavicon, size: customImageSize},
		"icon-72x72":         {slot: ImageSlotPWA, size: 72},
		"icon-96x96":         {slot: ImageSlotPWA, size: 96},
		"icon-128x128":       {slot: ImageSlotPWA, size: 128},
		"icon-144x144":       {slot: ImageSlotPWA, size: 144},
		"icon-152x152":       {slot: ImageSlotPWA, size: 152},
		"icon-192x192":       {slot: ImageSlotPWA, size: 192},
		"icon-384x384":       {slot: ImageSlotPWA, size: 384},
		"icon-512x512":       {slot: ImageSlotPWA, size: 512},
	}
)

func NewApplicationImagesService(embeddedFS embed.FS, settingsService *settings.SettingsService, db *database.DB) *ApplicationImagesService {
	service := &ApplicationImagesService{
		standard:        make(map[string]appImage),
		development:     make(map[string]appImage),
		settingsService: settingsService,
		db:              db,
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

func (s *ApplicationImagesService) GetImageWithColor(ctx context.Context, name, colorOverride string, loop bool) ([]byte, string, error) {
	if target, isCustomizable := customImageTargets[name]; isCustomizable && s.settingsService.GetStringSetting(ctx, imageSlots[target.slot].hashKey) != "" {
		var custom CustomImage
		if err := s.db.WithContext(ctx).Where("slot = ? AND size = ?", target.slot, target.size).Limit(1).Find(&custom).Error; err != nil {
			return nil, "", fmt.Errorf("failed to load custom image: %w", err)
		}
		if len(custom.Data) > 0 {
			return custom.Data, custom.MimeType, nil
		}
	}

	img, ok := s.standard[name]
	if dev, hasDev := s.development[name]; hasDev && s.settingsService.GetSettingsConfig().DevelopmentBrandingEnabled.IsTrue() {
		img, ok = dev, true
	}
	if !ok {
		return nil, "", common.Classify(common.ErrNotFound, fmt.Errorf("image '%s' not found", name))
	}

	data, mimeType := img.data, img.mimeType
	if !IsLogoVariant(name) || mimeType != svgMimeType {
		return data, mimeType, nil
	}

	accentColor := kit.Ternary(types.SafeAccentColor.MatchString(colorOverride), colorOverride, types.DefaultAccentColor)
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

func (s *ApplicationImagesService) UploadImage(ctx context.Context, slot ImageSlot, r io.Reader) error {
	spec, ok := imageSlots[slot]
	if !ok {
		return ErrImageSlotInvalid
	}
	data, readErr := io.ReadAll(io.LimitReader(r, ImageMaxBytes+1))
	if readErr != nil {
		return fmt.Errorf("failed to read image: %w", readErr)
	}
	if len(data) > ImageMaxBytes {
		return ErrImageTooLarge
	}
	mimeType := detectImageMimeType(data)
	if !slices.Contains(spec.mimeTypes, mimeType) {
		return common.Classify(common.ErrValidation, fmt.Errorf("unsupported image format for %s: accepted types are %s", slot, strings.Join(spec.mimeTypes, ", ")))
	}
	images, buildErr := customImagesForSlot(slot, data, mimeType)
	if buildErr != nil {
		return buildErr
	}

	sum := sha256.Sum256(data)
	return s.settingsService.UpdateSettingWithChanges(ctx, spec.hashKey, hex.EncodeToString(sum[:8]), func(tx *gorm.DB) error {
		if deleteErr := tx.Where("slot = ?", slot).Delete(&CustomImage{}).Error; deleteErr != nil {
			return fmt.Errorf("failed to replace custom image: %w", deleteErr)
		}
		if createErr := tx.Create(&images).Error; createErr != nil {
			return fmt.Errorf("failed to save custom image: %w", createErr)
		}
		return nil
	})
}

func (s *ApplicationImagesService) DeleteImage(ctx context.Context, slot ImageSlot) error {
	spec, ok := imageSlots[slot]
	if !ok {
		return ErrImageSlotInvalid
	}
	return s.settingsService.UpdateSettingWithChanges(ctx, spec.hashKey, "", func(tx *gorm.DB) error {
		if err := tx.Where("slot = ?", slot).Delete(&CustomImage{}).Error; err != nil {
			return fmt.Errorf("failed to delete custom image: %w", err)
		}
		return nil
	})
}

func IsLogoVariant(name string) bool {
	switch name {
	case "logo", "logo-full", "logo-animated", "logo-full-animated":
		return true
	}
	return false
}
