package certs

import (
	"crypto/x509"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

func LoadCustomCertPool(certsDir string) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system cert pool: %w", err)
	}
	if pool == nil {
		pool = x509.NewCertPool()
	}

	certsDir = strings.TrimSpace(certsDir)
	if certsDir == "" {
		return pool, nil
	}

	stat, err := os.Stat(certsDir)
	if err != nil {
		return nil, fmt.Errorf("stat custom certs directory %q: %w", certsDir, err)
	}
	if !stat.IsDir() {
		return nil, fmt.Errorf("custom certs path %q is not a directory", certsDir)
	}

	loaded := 0
	scanned := 0

	err = filepath.WalkDir(certsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".pem" && ext != ".crt" && ext != ".cert" {
			return nil
		}

		scanned++
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read custom cert file %q: %w", path, err)
		}

		if ok := pool.AppendCertsFromPEM(content); !ok {
			return fmt.Errorf("no valid PEM certificate found in %q", path)
		}

		loaded++
		slog.Info("Loaded custom CA certificate", "path", path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load custom certs from %q: %w", certsDir, err)
	}

	slog.Info("Custom certificate loading complete", "directory", certsDir, "scanned_files", scanned, "loaded_files", loaded)
	return pool, nil
}
