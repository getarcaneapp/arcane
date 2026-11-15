package rustic

import (
	"bytes"
	"context"
	"encoding/pem"
	"os"
	"path/filepath"

	"kit.getarcane.app/acfs"
)

const (
	trustBundleFile = "etc/ssl/certs/ca-certificates.crt"
)

// Same lookup order as Go's crypto/x509 on Linux.
var (
	defaultCertFiles = []string{
		"/etc/ssl/certs/ca-certificates.crt",
		"/etc/pki/tls/certs/ca-bundle.crt",
		"/etc/ssl/ca-bundle.pem",
		"/etc/pki/tls/cacert.pem",
		"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem",
		"/etc/ssl/cert.pem",
	}
	defaultCertDirs = []string{"/etc/ssl/certs", "/etc/pki/tls/certs"}
)

func trustBundle(ctx context.Context) []byte {
	files := defaultCertFiles
	if file := os.Getenv("SSL_CERT_FILE"); file != "" {
		files = []string{file}
	}
	dirs := defaultCertDirs
	if dir := os.Getenv("SSL_CERT_DIR"); dir != "" {
		dirs = filepath.SplitList(dir)
	}

	var bundle bytes.Buffer
	seen := map[string]bool{}
	appendCerts := func(data []byte) {
		for {
			var block *pem.Block
			block, data = pem.Decode(data)
			if block == nil {
				return
			}
			if block.Type != "CERTIFICATE" || seen[string(block.Bytes)] {
				continue
			}
			seen[string(block.Bytes)] = true
			_ = pem.Encode(&bundle, block)
		}
	}

	for _, file := range files {
		path, err := filepath.Abs(file)
		if err != nil {
			continue
		}
		if data, readErr := (acfs.Host{}).ReadFile(ctx, path); readErr == nil {
			appendCerts(data)
			break
		}
	}
	for _, dir := range dirs {
		path, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		entries, err := acfs.Host{}.List(ctx, path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if data, readErr := (acfs.Host{}).ReadFile(ctx, entry.Path); readErr == nil {
				appendCerts(data)
			}
		}
	}
	return bundle.Bytes()
}
