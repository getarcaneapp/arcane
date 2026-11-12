package appimages

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"slices"

	"golang.org/x/mod/semver"

	"kit.getarcane.app"
)

func isStableVersion(version string) bool {
	v := kit.EnsurePrefix(version, "v")
	return semver.IsValid(v) && semver.Prerelease(v) == ""
}

func detectImageMimeType(data []byte) string {
	if mimeType := http.DetectContentType(data); slices.Contains([]string{pngMimeType, jpegMimeType, webpMimeType, iconMimeType}, mimeType) {
		return mimeType
	}

	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		if start, ok := token.(xml.StartElement); ok {
			return kit.Ternary(start.Name.Local == "svg", svgMimeType, "")
		}
	}
}

func customImagesForSlot(slot ImageSlot, data []byte, mimeType string) ([]CustomImage, error) {
	if slot != ImageSlotPWA {
		return []CustomImage{{
			Slot:     string(slot),
			Size:     customImageSize,
			MimeType: mimeType,
			Data:     data,
		}}, nil
	}

	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != cfg.Height || cfg.Width < pwaIconMinSize || cfg.Width > pwaIconMaxSize {
		return nil, ErrPWAIconInvalid
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrPWAIconInvalid
	}

	images := make([]CustomImage, 0, len(pwaIconSizes))
	for _, size := range pwaIconSizes {
		resized, resizeErr := resizeSquarePNG(src, size)
		if resizeErr != nil {
			return nil, fmt.Errorf("failed to resize PWA icon to %dpx: %w", size, resizeErr)
		}
		images = append(images, CustomImage{
			Slot:     string(slot),
			Size:     size,
			MimeType: pngMimeType,
			Data:     resized,
		})
	}
	return images, nil
}

func resizeSquarePNG(src image.Image, size int) ([]byte, error) {
	bounds := src.Bounds()
	dst := image.NewRGBA64(image.Rect(0, 0, size, size))
	for y := range size {
		y0 := bounds.Min.Y + y*bounds.Dy()/size
		y1 := max(bounds.Min.Y+(y+1)*bounds.Dy()/size, y0+1)
		for x := range size {
			x0 := bounds.Min.X + x*bounds.Dx()/size
			x1 := max(bounds.Min.X+(x+1)*bounds.Dx()/size, x0+1)
			var r, g, b, a, n uint32
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := src.At(sx, sy).RGBA()
					r, g, b, a, n = r+cr, g+cg, b+cb, a+ca, n+1
				}
			}
			dst.SetRGBA64(x, y, color.RGBA64{
				R: uint16(r / n),
				G: uint16(g / n),
				B: uint16(b / n),
				A: uint16(a / n),
			})
		}
	}

	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
