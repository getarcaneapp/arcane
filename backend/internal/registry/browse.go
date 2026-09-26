package registry

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"golang.org/x/sync/errgroup"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
)

const tagDetailsConcurrency = 4

// browseTargetInternal is a registry resolved for the distribution API.
type browseTargetInternal struct {
	registry name.Registry
	// prefix is the repository namespace configured in the registry URL, if any.
	prefix  string
	options []remote.Option
}

// ListRepositories lists the repositories of a configured registry through the
// catalog API, limited to the namespace of the registry URL.
func (s *ContainerRegistryService) ListRepositories(ctx context.Context, id string, params pagination.QueryParams) ([]containerregistry.Repository, pagination.Response, error) {
	lookupCtx, cancel := s.browseContextInternal(ctx)
	defer cancel()

	target, err := s.browseTargetInternal(lookupCtx, id)
	if err != nil {
		return nil, pagination.Response{}, err
	}

	names, err := remote.Catalog(lookupCtx, target.registry, target.options...)
	if err != nil {
		return nil, pagination.Response{}, classifyBrowseErrorInternal(err, "failed to list repositories (the registry may not support the catalog API)")
	}

	items := make([]containerregistry.Repository, 0, len(names))
	for _, repositoryName := range names {
		if target.prefix != "" && !strings.HasPrefix(repositoryName, target.prefix+"/") {
			continue
		}
		items = append(items, containerregistry.Repository{Name: repositoryName})
	}

	config := pagination.Config[containerregistry.Repository]{
		SearchAccessors: []pagination.SearchAccessor[containerregistry.Repository]{
			func(item containerregistry.Repository) (string, error) { return item.Name, nil },
		},
		SortBindings: []pagination.SortBinding[containerregistry.Repository]{
			{Key: "name", Fn: func(a, b containerregistry.Repository) int { return strings.Compare(a.Name, b.Name) }},
		},
	}
	result := config.SearchOrderAndPaginate(items, params)
	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
}

// ListRepositoryTags lists the tags of a repository. Manifest details are only
// fetched for the requested page.
func (s *ContainerRegistryService) ListRepositoryTags(ctx context.Context, id, repository string, params pagination.QueryParams) ([]containerregistry.RepositoryTag, pagination.Response, error) {
	lookupCtx, cancel := s.browseContextInternal(ctx)
	defer cancel()

	target, err := s.browseTargetInternal(lookupCtx, id)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	repo, err := target.repositoryInternal(repository)
	if err != nil {
		return nil, pagination.Response{}, err
	}

	tagNames, err := remote.List(repo, append(slices.Clip(target.options), remote.WithContext(lookupCtx))...)
	if err != nil {
		return nil, pagination.Response{}, classifyBrowseErrorInternal(err, "failed to list tags")
	}

	items := make([]containerregistry.RepositoryTag, 0, len(tagNames))
	for _, tagName := range tagNames {
		items = append(items, containerregistry.RepositoryTag{Name: tagName, Platforms: []containerregistry.TagPlatform{}})
	}

	config := pagination.Config[containerregistry.RepositoryTag]{
		SearchAccessors: []pagination.SearchAccessor[containerregistry.RepositoryTag]{
			func(item containerregistry.RepositoryTag) (string, error) { return item.Name, nil },
		},
		SortBindings: []pagination.SortBinding[containerregistry.RepositoryTag]{
			{Key: "name", Fn: func(a, b containerregistry.RepositoryTag) int { return strings.Compare(a.Name, b.Name) }},
		},
	}
	result := config.SearchOrderAndPaginate(items, params)

	s.loadTagDetailsInternal(lookupCtx, repo, target.options, result.Items)

	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
}

// DeleteRepositoryTag deletes the manifest a tag points to. Registries delete
// manifests by digest, so every tag sharing that digest is removed too.
func (s *ContainerRegistryService) DeleteRepositoryTag(ctx context.Context, id, repository, tag string) (string, error) {
	lookupCtx, cancel := s.browseContextInternal(ctx)
	defer cancel()

	target, err := s.browseTargetInternal(lookupCtx, id)
	if err != nil {
		return "", err
	}
	repo, err := target.repositoryInternal(repository)
	if err != nil {
		return "", err
	}
	tagRef := repo.Tag(strings.TrimSpace(tag))
	if tagRef.TagStr() != strings.TrimSpace(tag) {
		return "", common.Classify(common.ErrValidation, errors.Errorf("invalid tag %q", tag))
	}

	options := append(slices.Clip(target.options), remote.WithContext(lookupCtx))
	descriptor, err := remote.Head(tagRef, options...)
	if err != nil {
		return "", classifyBrowseErrorInternal(err, "failed to resolve tag digest")
	}

	digest := descriptor.Digest.String()
	if err := remote.Delete(repo.Digest(digest), options...); err != nil {
		return "", classifyBrowseErrorInternal(err, "failed to delete manifest")
	}
	return digest, nil
}

func (s *ContainerRegistryService) browseContextInternal(ctx context.Context) (context.Context, context.CancelFunc) {
	timeoutSeconds := 0
	if s.settingsService != nil {
		timeoutSeconds = s.settingsService.GetSettingsConfig().RegistryTagTimeout.AsInt()
	}
	return context.WithTimeout(ctx, timeouts.GetDuration(timeoutSeconds, timeouts.DefaultRegistryTags))
}

func (s *ContainerRegistryService) browseTargetInternal(ctx context.Context, id string) (*browseTargetInternal, error) {
	reg, err := s.GetRegistryByID(ctx, id)
	if err != nil {
		return nil, err
	}

	host, prefix := splitRegistryURLInternal(reg.URL)
	nameOptions := []name.Option{name.StrictValidation}
	if reg.Insecure {
		nameOptions = append(nameOptions, name.Insecure)
	}
	registryName, err := name.NewRegistry(host, nameOptions...)
	if err != nil {
		return nil, common.Classify(common.ErrValidation, errors.WrapIff(err, "invalid registry URL %q", reg.URL))
	}

	credential, err := s.credentialForRegistryInternal(ctx, reg)
	if err != nil {
		return nil, err
	}
	options := []remote.Option{remote.WithAuth(authn.Anonymous)}
	if credential != nil {
		options = []remote.Option{remote.WithAuth(&authn.Basic{Username: credential.Username, Password: credential.Token})}
	}
	if s.distributionHTTPClient != nil && s.distributionHTTPClient.Transport != nil {
		options = append(options, remote.WithTransport(s.distributionHTTPClient.Transport))
	}

	return &browseTargetInternal{registry: registryName, prefix: prefix, options: options}, nil
}

func (t *browseTargetInternal) repositoryInternal(repository string) (name.Repository, error) {
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if repository == "" {
		return name.Repository{}, common.Classify(common.ErrValidation, errors.New("repository is required"))
	}
	if t.prefix != "" && !strings.HasPrefix(repository, t.prefix+"/") {
		return name.Repository{}, common.Classify(common.ErrValidation, errors.Errorf("repository %q is outside the registry namespace %q", repository, t.prefix))
	}
	repo, err := name.NewRepository(t.registry.RegistryStr()+"/"+repository, name.StrictValidation)
	if err != nil {
		return name.Repository{}, common.Classify(common.ErrValidation, errors.WrapIff(err, "invalid repository %q", repository))
	}
	// NewRepository resets registry options, so keep the insecure flag.
	repo.Registry = t.registry
	return repo, nil
}

// splitRegistryURLInternal separates the registry host from an optional
// namespace path, e.g. "ghcr.io/acme" becomes ("ghcr.io", "acme").
func splitRegistryURLInternal(registryURL string) (host, prefix string) {
	value := strings.TrimSpace(registryURL)
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	value = strings.Trim(value, "/")
	host, prefix, _ = strings.Cut(value, "/")
	return host, strings.Trim(prefix, "/")
}

// loadTagDetailsInternal records per-tag failures on the tag itself so one
// unreadable manifest does not hide the rest of the page.
func (s *ContainerRegistryService) loadTagDetailsInternal(ctx context.Context, repo name.Repository, options []remote.Option, tags []containerregistry.RepositoryTag) {
	var wg sync.WaitGroup
	slots := make(chan struct{}, tagDetailsConcurrency)
	for i := range tags {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			if err := fillTagDetailsInternal(ctx, repo, options, &tags[i]); err != nil {
				tags[i].Error = err.Error()
			}
		})
	}
	wg.Wait()
}

func fillTagDetailsInternal(ctx context.Context, repo name.Repository, options []remote.Option, tag *containerregistry.RepositoryTag) error {
	options = append(slices.Clip(options), remote.WithContext(ctx))
	descriptor, err := remote.Get(repo.Tag(tag.Name), options...)
	if err != nil {
		return err
	}
	tag.Digest = descriptor.Digest.String()
	tag.MediaType = string(descriptor.MediaType)

	if descriptor.MediaType.IsIndex() {
		return fillIndexTagDetailsInternal(descriptor, tag)
	}

	img, err := descriptor.Image()
	if err != nil {
		return err
	}
	platform, created, err := imagePlatformInternal(img, tag.Digest)
	if err != nil {
		return err
	}
	tag.Platforms = []containerregistry.TagPlatform{platform}
	tag.Size = platform.Size
	tag.Created = created
	return nil
}

func fillIndexTagDetailsInternal(descriptor *remote.Descriptor, tag *containerregistry.RepositoryTag) error {
	index, err := descriptor.ImageIndex()
	if err != nil {
		return err
	}
	manifest, err := index.IndexManifest()
	if err != nil {
		return err
	}

	var (
		mu        sync.Mutex
		platforms []containerregistry.TagPlatform
	)
	group := errgroup.Group{}
	group.SetLimit(tagDetailsConcurrency)
	for _, child := range manifest.Manifests {
		// Skip attestation manifests and nested indexes.
		if child.Platform == nil || child.Platform.OS == "unknown" || !child.MediaType.IsImage() {
			continue
		}
		group.Go(func() error {
			platform := containerregistry.TagPlatform{
				OS:           child.Platform.OS,
				Architecture: child.Platform.Architecture,
				Variant:      child.Platform.Variant,
				Digest:       child.Digest.String(),
			}
			img, err := index.Image(child.Digest)
			if err != nil {
				return err
			}
			size, err := imageSizeInternal(img)
			if err != nil {
				return err
			}
			platform.Size = size

			mu.Lock()
			platforms = append(platforms, platform)
			mu.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}

	sortTagPlatformsInternal(platforms)
	tag.Platforms = platforms
	for _, platform := range platforms {
		tag.Size += platform.Size
	}
	return nil
}

func imagePlatformInternal(img v1.Image, digest string) (containerregistry.TagPlatform, *time.Time, error) {
	size, err := imageSizeInternal(img)
	if err != nil {
		return containerregistry.TagPlatform{}, nil, err
	}
	config, err := img.ConfigFile()
	if err != nil {
		return containerregistry.TagPlatform{}, nil, err
	}
	platform := containerregistry.TagPlatform{
		OS:           config.OS,
		Architecture: config.Architecture,
		Variant:      config.Variant,
		Digest:       digest,
		Size:         size,
	}
	if config.Created.IsZero() {
		return platform, nil, nil
	}
	created := config.Created.UTC()
	return platform, &created, nil
}

// imageSizeInternal sums the compressed config and layer sizes of an image manifest.
func imageSizeInternal(img v1.Image) (int64, error) {
	manifest, err := img.Manifest()
	if err != nil {
		return 0, err
	}
	size := manifest.Config.Size
	for _, layer := range manifest.Layers {
		size += layer.Size
	}
	return size, nil
}

func sortTagPlatformsInternal(platforms []containerregistry.TagPlatform) {
	slices.SortFunc(platforms, func(a, b containerregistry.TagPlatform) int {
		return cmp.Or(
			strings.Compare(a.OS, b.OS),
			strings.Compare(a.Architecture, b.Architecture),
			strings.Compare(a.Variant, b.Variant),
		)
	})
}

// classifyBrowseErrorInternal maps distribution API failures to API error kinds.
func classifyBrowseErrorInternal(err error, message string) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return common.Classify(common.ErrTimeout, errors.WrapIf(err, message))
	}

	var transportErr *transport.Error
	if !errors.As(err, &transportErr) {
		return common.Classify(common.ErrUnavailable, errors.WrapIf(err, message))
	}

	// Upstream auth failures are not Arcane permission failures, so they must not map to 401/403.
	switch transportErr.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return common.Classify(common.ErrBadRequest, errors.WrapIf(err, message+": the registry denied access with the configured credentials"))
	case http.StatusNotFound:
		return common.Classify(common.ErrNotFound, errors.WrapIf(err, message))
	case http.StatusMethodNotAllowed:
		return common.Classify(common.ErrBadRequest, errors.WrapIf(err, message+": the registry does not allow this operation"))
	default:
		return common.Classify(common.ErrUnavailable, errors.WrapIf(err, message))
	}
}
