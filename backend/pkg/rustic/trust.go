package rustic

import (
	"bytes"
	"context"
	"encoding/pem"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"kit.getarcane.app/acfs"
	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/arcane/v3/pkg/volumes"
)

const (
	trustBundleDir      = "/etc/ssl/certs"
	trustBundleName     = "ca-certificates.crt"
	stagerRemoveRetries = 12
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

// stageTrustBundle copies the bundle into a volume through a stager that mounts
// only that volume; copying into the Rustic container fails when it has file
// mounts under read-only mounts. Run cleanup after Rustic exits: removing the
// stager right after the copy fails on ZFS while its dataset is busy.
func stageTrustBundle(ctx context.Context, dockerClient *client.Client, bundle []byte) (string, func(), error) {
	created, err := dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{Labels: volumes.Labels()})
	if err != nil {
		return "", nil, err
	}
	stagerID := ""
	cleanup := func() {
		cleanupCtx := context.WithoutCancel(ctx)
		if stagerID != "" {
			removeStager(cleanupCtx, dockerClient, stagerID)
		}
		removeCtx, cancel := context.WithTimeout(cleanupCtx, timeouts.DefaultDockerAPI)
		defer cancel()
		if _, removeErr := dockerClient.VolumeRemove(removeCtx, created.Volume.Name, client.VolumeRemoveOptions{Force: true}); removeErr != nil && !cerrdefs.IsNotFound(removeErr) {
			slog.WarnContext(cleanupCtx, "failed to remove Rustic CA certificate volume", "volumeName", created.Volume.Name, "error", removeErr)
		}
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
	if err != nil {
		cleanup()
		return "", nil, err
	}
	stagerID = stager.ID
	copyCtx, copyCancel := context.WithTimeout(ctx, timeouts.DefaultDockerAPI)
	err = docker.CopyToContainer(copyCtx, dockerClient, stager.ID, trustBundleDir, map[string][]byte{trustBundleName: bundle})
	copyCancel()
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return created.Volume.Name, cleanup, nil
}

func removeStager(ctx context.Context, dockerClient *client.Client, containerID string) {
	for attempt := 1; ; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, timeouts.DefaultDockerAPI)
		_, err := dockerClient.ContainerRemove(attemptCtx, containerID, volumes.RemoveOptions())
		cancel()
		if err == nil || cerrdefs.IsNotFound(err) {
			return
		}
		if attempt == stagerRemoveRetries {
			slog.WarnContext(ctx, "failed to remove Rustic CA certificate stager", "containerId", containerID, "error", err)
			return
		}
		time.Sleep(5 * time.Second)
	}
}
