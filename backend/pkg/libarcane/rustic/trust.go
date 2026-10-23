package rustic

import (
	"bytes"
	"encoding/pem"
	"os"
	"path/filepath"
)

// trustBundlePath is a path the Rustic binary probes for system roots.
const trustBundlePath = "etc/ssl/certs/ca-certificates.crt"

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

// trustBundle returns the PEM roots Arcane itself trusts, honoring
// SSL_CERT_FILE and SSL_CERT_DIR, so the Rustic image (which ships no CA
// store) can reach endpoints signed by a private CA.
func trustBundle() []byte {
	files := defaultCertFiles
	if file := os.Getenv("SSL_CERT_FILE"); file != "" {
		files = []string{file}
	}
	dirs := defaultCertDirs
	if dir, ok := os.LookupEnv("SSL_CERT_DIR"); ok {
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
		if data, err := os.ReadFile(file); err == nil {
			appendCerts(data)
			break
		}
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if data, readErr := os.ReadFile(filepath.Join(dir, entry.Name())); readErr == nil {
				appendCerts(data)
			}
		}
	}
	return bundle.Bytes()
}
