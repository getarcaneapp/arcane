package registry

import (
	"context"
	"runtime"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
)

const (
	ociImageVersionLabel = "org.opencontainers.image.version"
	versionLabelCacheTTL = 3 * time.Hour
)

// ErrNoVersionLabel is returned when the remote image config carries no
// org.opencontainers.image.version label.
var ErrNoVersionLabel = errors.New("image config has no version label")

// ImageVersionLabel resolves the org.opencontainers.image.version label from the
// remote image config for a tag or digest reference, without pulling the image.
// Anonymous access only — this is used for Arcane's own public GHCR images.
func (s *ContainerRegistryService) ImageVersionLabel(ctx context.Context, imageRef string) (string, error) {
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return "", errors.New("empty image reference")
	}

	label, found, err := s.labelCache.GetWithLoaders(imageRef, func(_ []string) (map[string]string, error) {
		loadCtx, cancel := context.WithTimeout(ctx, timeouts.DefaultRegistry)
		defer cancel()

		value, loadErr := fetchImageVersionLabelInternal(loadCtx, imageRef)
		if loadErr != nil {
			return nil, loadErr
		}
		return map[string]string{imageRef: value}, nil
	})
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("registry version label cache loader returned no label")
	}
	return label, nil
}

func fetchImageVersionLabelInternal(ctx context.Context, imageRef string) (string, error) {
	parsedRef, err := name.ParseReference(imageRef)
	if err != nil {
		return "", errors.WrapIff(err, "invalid image reference %q", imageRef)
	}

	img, err := remote.Image(parsedRef,
		remote.WithContext(ctx),
		remote.WithPlatform(v1.Platform{OS: "linux", Architecture: runtime.GOARCH}))
	if err != nil {
		return "", errors.WrapIff(err, "version label fetch failed for %s", imageRef)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return "", errors.WrapIff(err, "version label fetch failed for %s", imageRef)
	}
	if cfg == nil || strings.TrimSpace(cfg.Config.Labels[ociImageVersionLabel]) == "" {
		return "", ErrNoVersionLabel
	}
	return strings.TrimSpace(cfg.Config.Labels[ociImageVersionLabel]), nil
}
