package rustic

import (
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/arcane/v3/pkg/volumes"
)

const (
	// trustBundleDir is the directory the Rustic binary probes for system roots.
	trustBundleDir = "/etc/ssl/certs"
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

// trustBundle returns the PEM roots Arcane itself trusts, honoring
// SSL_CERT_FILE and SSL_CERT_DIR, so the Rustic image (which ships no CA
// store) can reach endpoints signed by a private CA.
func trustBundle() []byte {
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

func stageTrustBundle(ctx context.Context, dockerClient *client.Client, bundle []byte) (string, error) {
	created, err := dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{Labels: volumes.Labels()})
	if err != nil {
		return "", err
	}
	stager, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:  DefaultImage,
			Labels: volumes.Labels(),
		},
		HostConfig: &container.HostConfig{
			Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: created.Volume.Name, Target: trustBundleDir}},
		},
	})
	if err == nil {
		copyCtx, copyCancel := context.WithTimeout(ctx, timeouts.DefaultDockerAPI)
		err = docker.CopyToContainer(copyCtx, dockerClient, stager.ID, trustBundleDir, map[string][]byte{"ca-certificates.crt": bundle})
		copyCancel()
		removeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultDockerAPI)
		_, removeErr := dockerClient.ContainerRemove(removeCtx, stager.ID, client.ContainerRemoveOptions{Force: true})
		cancel()
		err = errors.Join(err, removeErr)
	}
	if err != nil {
		removeTrustBundle(ctx, dockerClient, created.Volume.Name)
		return "", err
	}
	return created.Volume.Name, nil
}

func removeTrustBundle(ctx context.Context, dockerClient *client.Client, volumeName string) {
	removeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultDockerAPI)
	defer cancel()
	if _, err := dockerClient.VolumeRemove(removeCtx, volumeName, client.VolumeRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
		slog.WarnContext(ctx, "failed to remove Rustic CA certificate volume", "volumeName", volumeName, "error", err)
	}
}
