package image

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
)

const maxImageArchiveMetadataBytes = 16 << 20

func recordImageDenial(ctx context.Context, perm string, err error) error {
	if errors.Is(err, authz.ErrResourceForbidden) {
		middleware.RecordDenial(ctx, types.AuthzDenial{
			Permission:    perm,
			EnvironmentID: authz.RequestEnvironmentFromContext(ctx),
			RemoteAddr:    middleware.GetRemoteAddrFromContext(ctx),
		})
	}
	return err
}

func authorizeImage(ctx context.Context, perm string, inspect image.InspectResponse, createdTags ...string) error {
	labels := kit.FromPtr(inspect.Config).Labels
	if err := recordImageDenial(ctx, perm, authz.CheckResource(ctx, perm, authz.PermImagesRead, authz.ImageAttributes(inspect.RepoTags, labels))); err != nil {
		return err
	}
	if len(createdTags) == 0 {
		return nil
	}
	return authorizeImageCreate(ctx, perm, authz.ImageAttributes(createdTags, labels))
}

func authorizeImageCreate(ctx context.Context, perm string, attrs authz.ResourceAttributes) error {
	if len(attrs.Names) == 0 {
		return recordImageDenial(ctx, perm, authz.CheckCreate(ctx, perm, attrs))
	}
	for _, name := range attrs.Names {
		if checkErr := recordImageDenial(ctx, perm, authz.CheckCreate(ctx, perm, authz.ResourceAttributes{
			Names:   []string{name},
			Labels:  attrs.Labels,
			Project: attrs.Project,
			OwnerID: attrs.OwnerID,
		})); checkErr != nil {
			return checkErr
		}
	}
	return nil
}

func (s *ImageService) authorizeImageRef(ctx context.Context, ref, perm string, createdTags ...string) error {
	if authz.ScopeFromContext(ctx, perm).IsUnrestricted() {
		return nil
	}
	inspect, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ImageInspectResult, error) {
		return dockerClient.ImageInspect(ctx, ref)
	})
	if err != nil {
		return fmt.Errorf("failed to inspect image: %w", err)
	}
	return authorizeImage(ctx, perm, inspect.InspectResponse, createdTags...)
}

func scopedImages(ctx context.Context, images []image.Summary) []image.Summary {
	scope := authz.ScopeFromContext(ctx, authz.PermImagesList)
	if scope.IsUnrestricted() {
		return images
	}
	return slices.DeleteFunc(slices.Clone(images), func(img image.Summary) bool {
		return !scope.Matches(authz.ImageAttributes(img.RepoTags, img.Labels))
	})
}

func authorizeImageArchive(ctx context.Context, archive io.ReadSeeker) error {
	if authz.ScopeFromContext(ctx, authz.PermImagesUpload).IsUnrestricted() {
		return nil
	}
	images, err := archiveImageAttributes(archive)
	if err != nil {
		return common.Classify(common.ErrValidation, fmt.Errorf("failed to read image archive: %w", err))
	}
	if len(images) == 0 {
		return authorizeImageCreate(ctx, authz.PermImagesUpload, authz.ResourceAttributes{})
	}
	for _, attrs := range images {
		if authorizeErr := authorizeImageCreate(ctx, authz.PermImagesUpload, attrs); authorizeErr != nil {
			return authorizeErr
		}
	}
	return nil
}

func archiveImageAttributes(archive io.ReadSeeker) ([]authz.ResourceAttributes, error) {
	var manifest []struct {
		Config   string
		RepoTags []string
	}
	if err := walkImageArchive(archive, func(name string, entry io.Reader) error {
		if name != "manifest.json" {
			return nil
		}
		return json.NewDecoder(io.LimitReader(entry, maxImageArchiveMetadataBytes)).Decode(&manifest)
	}); err != nil {
		return nil, err
	}

	labelsByConfig := make(map[string]map[string]string, len(manifest))
	for _, entry := range manifest {
		labelsByConfig[path.Clean(entry.Config)] = nil
	}
	if err := walkImageArchive(archive, func(name string, entry io.Reader) error {
		if _, ok := labelsByConfig[name]; !ok {
			return nil
		}
		var config struct {
			Config struct {
				Labels map[string]string
			} `json:"config"`
		}
		if err := json.NewDecoder(io.LimitReader(entry, maxImageArchiveMetadataBytes)).Decode(&config); err != nil {
			return err
		}
		labelsByConfig[name] = config.Config.Labels
		return nil
	}); err != nil {
		return nil, err
	}

	attrs := make([]authz.ResourceAttributes, 0, len(manifest))
	for _, entry := range manifest {
		attrs = append(attrs, authz.ImageAttributes(entry.RepoTags, labelsByConfig[path.Clean(entry.Config)]))
	}
	return attrs, nil
}

func walkImageArchive(archive io.ReadSeeker, visit func(name string, entry io.Reader) error) error {
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	defer func() {
		_, _ = archive.Seek(0, io.SeekStart)
	}()

	magic := make([]byte, 2)
	if _, err := io.ReadFull(archive, magic); err != nil {
		return err
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	source, err := func() (io.Reader, error) {
		if !bytes.Equal(magic, []byte{0x1f, 0x8b}) {
			return archive, nil
		}
		return gzip.NewReader(archive)
	}()
	if err != nil {
		return err
	}

	reader := tar.NewReader(source)
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			return nil
		}
		if nextErr != nil {
			return nextErr
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if visitErr := visit(path.Clean(header.Name), reader); visitErr != nil {
			return visitErr
		}
	}
}
