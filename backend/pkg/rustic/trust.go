package rustic

import (
	"bytes"
	"context"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"kit.getarcane.app/acfs"
	"kit.getarcane.app/docker"
	"kit.getarcane.app/fs"
	"kit.getarcane.app/sys/cgroup"

	"getarcane.dev/arcane/v3/pkg/volumes"
)

const (
	TrustBundlePath = "/etc/ssl/certs/ca-certificates.crt"
	TrustBundleName = ".arcane-rustic-ca.crt"
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

func TrustMounts(ctx context.Context, dockerClient *client.Client, directory string, arcaneMounts []container.MountPoint) ([]mount.Mount, error) {
	bundle := trustBundle(ctx)
	if len(bundle) == 0 {
		return nil, nil
	}
	bundlePath, err := filepath.Abs(filepath.Join(directory, TrustBundleName))
	if err != nil {
		return nil, fmt.Errorf("failed to write CA certificates for Rustic container: %w", err)
	}
	if current, readErr := (acfs.Host{}).ReadFile(ctx, bundlePath); readErr != nil || !bytes.Equal(current, bundle) {
		if err = (acfs.Host{}).MkdirAll(ctx, filepath.Dir(bundlePath), fs.DirPerm); err != nil {
			return nil, fmt.Errorf("failed to write CA certificates for Rustic container: %w", err)
		}
		if err = (acfs.Host{}).Write(ctx, bundlePath, bundle, acfs.WriteOptions{Mode: fs.FilePerm}); err != nil {
			return nil, fmt.Errorf("failed to write CA certificates for Rustic container: %w", err)
		}
	}
	if _, containerErr := cgroup.CurrentContainerID(); containerErr != nil {
		return []mount.Mount{{
			Type:     mount.TypeBind,
			Source:   bundlePath,
			Target:   TrustBundlePath,
			ReadOnly: true,
		}}, nil
	}
	resolved := docker.MountForSubpath(arcaneMounts, bundlePath, TrustBundlePath)
	if resolved == nil {
		slog.WarnContext(ctx, "Arcane's data directory is not a bind or named volume mount; Rustic runs without Arcane's CA certificates", "path", bundlePath)
		return nil, nil
	}
	mounts, err := volumes.BindSubpaths(ctx, dockerClient, []mount.Mount{{
		Type:          resolved.Type,
		Source:        resolved.Source,
		Target:        resolved.Target,
		ReadOnly:      true,
		BindOptions:   resolved.BindOptions,
		VolumeOptions: resolved.VolumeOptions,
	}})
	if err != nil {
		return nil, fmt.Errorf("failed to mount CA certificates for Rustic container: %w", err)
	}
	return mounts, nil
}
