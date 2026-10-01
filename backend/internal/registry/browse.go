package registry

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	utilsregistry "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/registryauth"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"golang.org/x/sync/errgroup"
)

const tagDetailsConcurrency = 4

// browseTargetInternal is a configured registry resolved for the distribution API.
type browseTargetInternal struct {
	registry name.Registry
	// prefix is the repository namespace configured in the registry URL, if any.
	prefix      string
	nameOptions []name.Option
	options     []remote.Option
}

// ListRepositories lists the repositories of a configured registry through the
// catalog API, limited to the namespace of the registry URL.
func (s *ContainerRegistryService) ListRepositories(ctx context.Context, id string, params pagination.QueryParams) ([]containerregistry.Repository, pagination.Response, error) {
	lookupCtx, cancel := s.tagLookupContextInternal(ctx)
	defer cancel()

	target, err := s.browseTargetInternal(lookupCtx, id)
	if err != nil {
		return nil, pagination.Response{}, err
	}

	names, err := remote.Catalog(lookupCtx, target.registry, target.options...)
	if err != nil {
		return nil, pagination.Response{}, classifyBrowseErrorInternal(err, "failed to list repositories (the registry may not support the catalog API)")
	}
	if target.prefix != "" {
		names = slices.DeleteFunc(names, func(repositoryName string) bool {
			return !strings.HasPrefix(repositoryName, target.prefix+"/")
		})
	}

	page, response := paginateNamesInternal(names, params)
	items := make([]containerregistry.Repository, 0, len(page))
	for _, repositoryName := range page {
		items = append(items, containerregistry.Repository{Name: repositoryName})
	}
	return items, response, nil
}

// ListRepositoryTags lists the tags of a repository. Manifest details are only
// fetched for the requested page, and a failure is recorded on the tag itself
// so one unreadable manifest does not hide the rest of the page.
func (s *ContainerRegistryService) ListRepositoryTags(ctx context.Context, id, repository string, params pagination.QueryParams) ([]containerregistry.RepositoryTag, pagination.Response, error) {
	lookupCtx, cancel := s.tagLookupContextInternal(ctx)
	defer cancel()

	target, err := s.browseTargetInternal(lookupCtx, id)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	repo, err := target.repositoryInternal(repository)
	if err != nil {
		return nil, pagination.Response{}, err
	}

	names, err := remote.List(repo, target.options...)
	if err != nil {
		return nil, pagination.Response{}, classifyBrowseErrorInternal(err, "failed to list tags")
	}

	page, response := paginateNamesInternal(names, params)
	items := make([]containerregistry.RepositoryTag, len(page))
	group := errgroup.Group{}
	group.SetLimit(tagDetailsConcurrency)
	for i, tagName := range page {
		group.Go(func() error {
			tag, err := tagDetailsInternal(repo, target.options, tagName)
			if err != nil {
				tag.Error = err.Error()
			}
			items[i] = tag
			return nil
		})
	}
	_ = group.Wait()

	return items, response, nil
}

// DeleteRepositoryTag deletes the manifest a tag points to. Registries delete
// manifests by digest, so every tag sharing that digest is removed too.
func (s *ContainerRegistryService) DeleteRepositoryTag(ctx context.Context, id, repository, tag string) (string, error) {
	lookupCtx, cancel := s.tagLookupContextInternal(ctx)
	defer cancel()

	target, err := s.browseTargetInternal(lookupCtx, id)
	if err != nil {
		return "", err
	}
	repo, err := target.repositoryInternal(repository)
	if err != nil {
		return "", err
	}
	tagRef, err := name.NewTag(repo.Name()+":"+strings.TrimSpace(tag), target.nameOptions...)
	if err != nil {
		return "", common.Classify(common.ErrValidation, fmt.Errorf("invalid tag %q: %w", tag, err))
	}

	descriptor, err := remote.Head(tagRef, target.options...)
	if err != nil {
		return "", classifyBrowseErrorInternal(err, "failed to resolve tag digest")
	}
	digest := descriptor.Digest.String()
	if err := remote.Delete(repo.Digest(digest), target.options...); err != nil {
		return "", classifyBrowseErrorInternal(err, "failed to delete manifest")
	}
	return digest, nil
}

// browseTargetInternal resolves a stored registry into a registry name, its
// namespace prefix, and the remote options carrying auth and transport.
func (s *ContainerRegistryService) browseTargetInternal(ctx context.Context, id string) (*browseTargetInternal, error) {
	reg, err := s.GetRegistryByID(ctx, id)
	if err != nil {
		return nil, err
	}

	host, prefix := utilsregistry.SplitRegistryURL(reg.URL)
	nameOptions := []name.Option{name.StrictValidation}
	if reg.Insecure {
		nameOptions = append(nameOptions, name.Insecure)
	}
	registryName, err := name.NewRegistry(host, nameOptions...)
	if err != nil {
		return nil, common.Classify(common.ErrValidation, fmt.Errorf("invalid registry URL %q: %w", reg.URL, err))
	}

	credential, err := s.credentialForRegistryInternal(ctx, reg)
	if err != nil {
		return nil, err
	}
	authenticator := authn.Anonymous
	if credential != nil {
		authenticator = authn.FromConfig(authn.AuthConfig{Username: credential.Username, Password: credential.Token})
	}
	options := []remote.Option{remote.WithContext(ctx), remote.WithAuth(authenticator)}
	if s.distributionHTTPClient.Transport != nil {
		options = append(options, remote.WithTransport(s.distributionHTTPClient.Transport))
	}

	return &browseTargetInternal{
		registry:    registryName,
		prefix:      prefix,
		nameOptions: nameOptions,
		options:     options,
	}, nil
}

// repositoryInternal parses a repository name and keeps it inside the registry namespace.
func (t *browseTargetInternal) repositoryInternal(repository string) (name.Repository, error) {
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if repository == "" {
		return name.Repository{}, common.Classify(common.ErrValidation, errors.New("repository is required"))
	}
	if t.prefix != "" && !strings.HasPrefix(repository, t.prefix+"/") {
		return name.Repository{}, common.Classify(common.ErrValidation, fmt.Errorf("repository %q is outside the registry namespace %q", repository, t.prefix))
	}
	repo, err := name.NewRepository(t.registry.Name()+"/"+repository, t.nameOptions...)
	if err != nil {
		return name.Repository{}, common.Classify(common.ErrValidation, fmt.Errorf("invalid repository %q: %w", repository, err))
	}
	return repo, nil
}

func paginateNamesInternal(names []string, params pagination.QueryParams) ([]string, pagination.Response) {
	config := pagination.Config[string]{
		SearchAccessors: []pagination.SearchAccessor[string]{
			func(item string) (string, error) { return item, nil },
		},
		SortBindings: []pagination.SortBinding[string]{
			{Key: "name", Fn: strings.Compare},
		},
	}
	result := config.SearchOrderAndPaginate(names, params)
	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params)
}

// tagDetailsInternal resolves the manifest behind a tag. A single image yields
// one platform; an index yields one per platform image, skipping attestations.
func tagDetailsInternal(repo name.Repository, options []remote.Option, tagName string) (containerregistry.RepositoryTag, error) {
	tag := containerregistry.RepositoryTag{Name: tagName, Platforms: []containerregistry.TagPlatform{}}

	descriptor, err := remote.Get(repo.Tag(tagName), options...)
	if err != nil {
		return tag, err
	}
	tag.Digest = descriptor.Digest.String()
	tag.MediaType = string(descriptor.MediaType)

	if !descriptor.MediaType.IsIndex() {
		img, err := descriptor.Image()
		if err != nil {
			return tag, err
		}
		config, err := img.ConfigFile()
		if err != nil {
			return tag, err
		}
		platform, err := tagPlatformInternal(img, config.Platform(), descriptor.Digest)
		if err != nil {
			return tag, err
		}
		tag.Platforms = []containerregistry.TagPlatform{platform}
		tag.Size = platform.Size
		if !config.Created.IsZero() {
			created := config.Created.UTC()
			tag.Created = &created
		}
		return tag, nil
	}

	index, err := descriptor.ImageIndex()
	if err != nil {
		return tag, err
	}
	manifest, err := index.IndexManifest()
	if err != nil {
		return tag, err
	}

	var mu sync.Mutex
	group := errgroup.Group{}
	group.SetLimit(tagDetailsConcurrency)
	for _, child := range manifest.Manifests {
		if child.Platform == nil || child.Platform.OS == "unknown" || !child.MediaType.IsImage() {
			continue
		}
		group.Go(func() error {
			img, err := index.Image(child.Digest)
			if err != nil {
				return err
			}
			platform, err := tagPlatformInternal(img, child.Platform, child.Digest)
			if err != nil {
				return err
			}
			mu.Lock()
			tag.Platforms = append(tag.Platforms, platform)
			tag.Size += platform.Size
			mu.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return tag, err
	}

	slices.SortFunc(tag.Platforms, func(a, b containerregistry.TagPlatform) int {
		return cmp.Or(
			strings.Compare(a.OS, b.OS),
			strings.Compare(a.Architecture, b.Architecture),
			strings.Compare(a.Variant, b.Variant),
		)
	})
	return tag, nil
}

// tagPlatformInternal describes one platform image with its compressed config and layer size.
func tagPlatformInternal(img v1.Image, platform *v1.Platform, digest v1.Hash) (containerregistry.TagPlatform, error) {
	manifest, err := img.Manifest()
	if err != nil {
		return containerregistry.TagPlatform{}, err
	}
	result := containerregistry.TagPlatform{Digest: digest.String(), Size: manifest.Config.Size}
	for _, layer := range manifest.Layers {
		result.Size += layer.Size
	}
	if platform != nil {
		result.OS = platform.OS
		result.Architecture = platform.Architecture
		result.Variant = platform.Variant
	}
	return result, nil
}

// classifyBrowseErrorInternal maps distribution API failures to API error kinds.
func classifyBrowseErrorInternal(err error, message string) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return common.Classify(common.ErrTimeout, fmt.Errorf("%s: %w", message, err))
	}
	// Upstream auth failures are not Arcane permission failures, so they must not map to 401/403.
	if isUnauthorizedRegistryErrorInternal(err) {
		return common.Classify(common.ErrBadRequest, fmt.Errorf("%s: %w", message+": the registry denied access with the configured credentials", err))
	}

	if transportErr, ok := errors.AsType[*transport.Error](err); ok {
		switch transportErr.StatusCode {
		case http.StatusNotFound:
			return common.Classify(common.ErrNotFound, fmt.Errorf("%s: %w", message, err))
		case http.StatusMethodNotAllowed:
			return common.Classify(common.ErrBadRequest, fmt.Errorf("%s: %w", message+": the registry does not allow this operation", err))
		}
	}
	return common.Classify(common.ErrUnavailable, fmt.Errorf("%s: %w", message, err))
}
