package services

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"path/filepath"
	"strings"
)

type FontService struct {
	fs embed.FS
}

func NewFontService(embeddedFS embed.FS) *FontService {
	return &FontService{
		fs: embeddedFS,
	}
}

func (s *FontService) GetSansFont() ([]byte, string, error) {
	return s.GetFont("Geist/geist.woff2")
}

func (s *FontService) GetMonoFont() ([]byte, string, error) {
	return s.GetFont("Geist/geist-mono.woff2")
}

func (s *FontService) GetSerifFont() ([]byte, string, error) {
	return s.GetFont("Calistoga/Calistoga-Regular.woff2")
}

func (s *FontService) GetFont(fontPath string) ([]byte, string, error) {
	// Prevent directory traversal
	if strings.Contains(fontPath, "..") {
		return nil, "", fmt.Errorf("invalid font path")
	}

	// The fonts are located in "fonts" directory in the embedded FS
	fullPath := filepath.Join("fonts", fontPath)

	data, err := s.fs.ReadFile(fullPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", fmt.Errorf("font not found")
		}
		return nil, "", err
	}

	ext := filepath.Ext(fontPath)
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		// Fallback for common font types if mime package doesn't detect them
		switch strings.ToLower(ext) {
		case ".woff2":
			mimeType = "font/woff2"
		case ".ttf":
			mimeType = "font/ttf"
		case ".otf":
			mimeType = "font/otf"
		default:
			mimeType = "application/octet-stream"
		}
	}

	return data, mimeType, nil
}
