package browse

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/samber/hot"
	"go.getarcane.app/kit/pkg"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/registryauth"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/tracing"
)

const (
	tagDetailsConcurrency = 4
	maxBrowsePageSize     = 100
	browseListCacheTTL    = 30 * time.Second
)

// Service browses repositories and tags through the distribution API.
type Service struct {
	resolve       func(ctx context.Context, id string) (types.ContainerRegistry, *types.ContainerRegistryCredential, error)
	lookupContext func(ctx context.Context) (context.Context, context.CancelFunc)
	transport     http.RoundTripper
	names         *hot.HotCache[string, []string]
}

// target is a configured registry resolved for the distribution API.
type target struct {
	registry name.Registry
	// prefix is the repository namespace configured in the registry URL, if any.
	prefix      string
	nameOptions []name.Option
	options     []remote.Option
	cacheKey    string
}

func NewService(
	resolve func(ctx context.Context, id string) (types.ContainerRegistry, *types.ContainerRegistryCredential, error),
	lookupContext func(ctx context.Context) (context.Context, context.CancelFunc),
	roundTripper http.RoundTripper,
) *Service {
	return &Service{
		resolve:       resolve,
		lookupContext: lookupContext,
		transport:     roundTripper,
		names:         hot.NewHotCache[string, []string](hot.LRU, 256).WithTTL(browseListCacheTTL).Build(),
	}
}

func (s *Service) cachedNames(key string, list func() ([]string, error)) ([]string, error) {
	if cached, found, _ := s.names.Get(key); found {
		return slices.Clone(cached), nil
	}
	names, err := list()
	if err != nil {
		return nil, err
	}
	s.names.Set(key, slices.Clone(names))
	return names, nil
}

// ListRepositories lists the repositories of a configured registry through the
// catalog API, limited to the namespace of the registry URL.
func (s *Service) ListRepositories(ctx context.Context, id string, params pagination.QueryParams) (listed []types.ContainerRegistryRepository, paging pagination.Response, listErr error) {
	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "registry.list_repositories", trace.WithAttributes(attribute.String("arcane.registry.id", id)))
	defer func() {
		span.SetAttributes(attribute.Int("arcane.result.count", len(listed)))
		tracing.End(span, listErr)
	}()

	lookupCtx, cancel := s.lookupContext(ctx)
	defer cancel()

	target, err := s.target(lookupCtx, id)
	if err != nil {
		return nil, pagination.Response{}, err
	}

	names, err := s.cachedNames(target.cacheKey, func() ([]string, error) {
		return remote.Catalog(lookupCtx, target.registry, target.options...)
	})
	if err != nil {
		return nil, pagination.Response{}, classifyBrowseError(err, "failed to list repositories (the registry may not support the catalog API)")
	}
	page, response := paginateNames(slices.DeleteFunc(names, func(repositoryName string) bool {
		return target.prefix != "" && !strings.HasPrefix(repositoryName, target.prefix+"/")
	}), params)
	items := make([]types.ContainerRegistryRepository, 0, len(page))
	for _, repositoryName := range page {
		items = append(items, types.ContainerRegistryRepository{Name: repositoryName})
	}
	return items, response, nil
}

// ListRepositoryTags lists the tags of a repository, fetching manifest details only for the requested page.
// A manifest failure is recorded on its tag so it does not hide the rest of the page.
func (s *Service) ListRepositoryTags(
	ctx context.Context,
	id, repository string,
	params pagination.QueryParams,
) (listed []types.ContainerRegistryRepositoryTag, paging pagination.Response, listErr error) {
	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "registry.list_tags", trace.WithAttributes(
		attribute.String("arcane.registry.id", id),
		attribute.String("arcane.registry.repository", repository),
	))
	defer func() {
		span.SetAttributes(attribute.Int("arcane.result.count", len(listed)))
		tracing.End(span, listErr)
	}()

	lookupCtx, cancel := s.lookupContext(ctx)
	defer cancel()

	target, err := s.target(lookupCtx, id)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	repo, err := target.repository(repository)
	if err != nil {
		return nil, pagination.Response{}, err
	}

	names, err := s.cachedNames(target.cacheKey+"\x00"+repository, func() ([]string, error) {
		return remote.List(repo, target.options...)
	})
	if err != nil {
		return nil, pagination.Response{}, classifyBrowseError(err, "failed to list tags")
	}

	page, response := paginateNames(names, params)
	items := make([]types.ContainerRegistryRepositoryTag, len(page))
	group := errgroup.Group{}
	group.SetLimit(tagDetailsConcurrency)
	for i, tagName := range page {
		group.Go(func() error {
			items[i] = tagDetails(repo, target.options, tagName)
			return nil
		})
	}
	_ = group.Wait()

	return items, response, nil
}

// DeleteRepositoryTag deletes the manifest a tag points to. Registries delete
// manifests by digest, so every tag sharing that digest is removed too.
func (s *Service) DeleteRepositoryTag(ctx context.Context, id, repository, tag string) (deleted string, opErr error) {
	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "registry.delete_tag", trace.WithAttributes(
		attribute.String("arcane.registry.id", id),
		attribute.String("arcane.registry.repository", repository),
		attribute.String("arcane.image.tag", tag),
	))
	defer func() {
		span.SetAttributes(attribute.String("arcane.image.digest", deleted))
		tracing.End(span, opErr)
	}()

	lookupCtx, cancel := s.lookupContext(ctx)
	defer cancel()

	target, err := s.target(lookupCtx, id)
	if err != nil {
		return "", err
	}
	repo, err := target.repository(repository)
	if err != nil {
		return "", err
	}
	tagRef, err := name.NewTag(repo.Name()+":"+strings.TrimSpace(tag), target.nameOptions...)
	if err != nil {
		return "", common.Classify(common.ErrValidation, fmt.Errorf("invalid tag %q: %w", tag, err))
	}

	descriptor, err := remote.Head(tagRef, target.options...)
	if err != nil {
		return "", classifyBrowseError(err, "failed to resolve tag digest")
	}
	if deleteErr := remote.Delete(repo.Digest(descriptor.Digest.String()), target.options...); deleteErr != nil {
		return "", classifyBrowseError(deleteErr, "failed to delete manifest")
	}
	s.names.Delete(target.cacheKey + "\x00" + repository)
	return descriptor.Digest.String(), nil
}

// target resolves a stored registry into a registry name, its
// namespace prefix, and the remote options carrying auth and transport.
func (s *Service) target(ctx context.Context, id string) (*target, error) {
	reg, credential, err := s.resolve(ctx, id)
	if err != nil {
		return nil, err
	}

	host, prefix := registryauth.SplitRegistryURL(reg.URL)
	registryName, err := name.NewRegistry(host, kit.Ternary(reg.Insecure, []name.Option{name.StrictValidation, name.Insecure}, []name.Option{name.StrictValidation})...)
	if err != nil {
		return nil, common.Classify(common.ErrValidation, fmt.Errorf("invalid registry URL %q: %w", reg.URL, err))
	}

	return &target{
		registry:    registryName,
		prefix:      prefix,
		cacheKey:    fmt.Sprintf("%s\x00%s\x00%s\x00%t", id, reg.URL, reg.Username, reg.Insecure),
		nameOptions: kit.Ternary(reg.Insecure, []name.Option{name.StrictValidation, name.Insecure}, []name.Option{name.StrictValidation}),
		options: append([]remote.Option{
			remote.WithContext(ctx),
			remote.WithAuth(kit.Ternary(credential == nil,
				authn.Anonymous,
				authn.FromConfig(authn.AuthConfig{Username: kit.FromPtr(credential).Username, Password: kit.FromPtr(credential).Token}),
			)),
		}, kit.Ternary(s.transport != nil, []remote.Option{remote.WithTransport(s.transport)}, nil)...),
	}, nil
}

// repository parses a repository name and keeps it inside the registry namespace.
func (t *target) repository(repository string) (name.Repository, error) {
	if strings.Trim(strings.TrimSpace(repository), "/") == "" {
		return name.Repository{}, common.Classify(common.ErrValidation, errors.New("repository is required"))
	}
	if t.prefix != "" && !strings.HasPrefix(strings.Trim(strings.TrimSpace(repository), "/"), t.prefix+"/") {
		return name.Repository{}, common.Classify(common.ErrValidation, fmt.Errorf("repository %q is outside the registry namespace %q", strings.Trim(strings.TrimSpace(repository), "/"), t.prefix))
	}
	repo, err := name.NewRepository(t.registry.Name()+"/"+strings.Trim(strings.TrimSpace(repository), "/"), t.nameOptions...)
	if err != nil {
		return name.Repository{}, common.Classify(common.ErrValidation, fmt.Errorf("invalid repository %q: %w", strings.Trim(strings.TrimSpace(repository), "/"), err))
	}
	return repo, nil
}

// classifyBrowseError maps distribution API failures to API error kinds.
func classifyBrowseError(err error, message string) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return common.Classify(common.ErrTimeout, fmt.Errorf("%s: %w", message, err))
	}
	// Upstream auth failures are not Arcane permission failures, so they must not map to 401/403.
	if IsUnauthorizedRegistryError(err) {
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

func IsUnauthorizedRegistryError(err error) bool {
	if err == nil {
		return false
	}

	// Prefer structured error type check from the Docker SDK / containerd.
	if errdefs.IsUnauthorized(err) || errdefs.IsPermissionDenied(err) {
		return true
	}
	if registryErr, ok := errors.AsType[*transport.Error](err); ok {
		return registryErr.StatusCode == http.StatusUnauthorized || registryErr.StatusCode == http.StatusForbidden
	}

	// Some Docker daemons return plain-text errors; these substrings cover Docker Hub, GHCR, and common OCI registries.
	return slices.ContainsFunc([]string{
		"unauthorized",
		"authentication required",
		"no basic auth credentials",
		"access denied",
		"incorrect username or password",
		"status: 401",
		"status 401",
		"status: 403",
		"status 403",
	}, func(indicator string) bool {
		return strings.Contains(strings.ToLower(err.Error()), indicator)
	})
}

// tagPlatform describes one platform image with its compressed config and layer size.
func tagPlatform(img v1.Image, platform *v1.Platform, digest v1.Hash) (types.ContainerRegistryTagPlatform, error) {
	manifest, err := img.Manifest()
	if err != nil {
		return types.ContainerRegistryTagPlatform{}, err
	}
	size := manifest.Config.Size
	for _, layer := range manifest.Layers {
		size += layer.Size
	}
	return types.ContainerRegistryTagPlatform{
		OS:           cmp.Or(platform, &v1.Platform{}).OS,
		Architecture: cmp.Or(platform, &v1.Platform{}).Architecture,
		Variant:      cmp.Or(platform, &v1.Platform{}).Variant,
		Digest:       digest.String(),
		Size:         size,
	}, nil
}

// tagDetails resolves the manifest behind a tag. A single image yields
// one platform; an index yields one per platform image, skipping attestations.
func tagDetails(repo name.Repository, options []remote.Option, tagName string) types.ContainerRegistryRepositoryTag {
	descriptor, err := remote.Get(repo.Tag(tagName), options...)
	if err != nil {
		return types.ContainerRegistryRepositoryTag{
			Name:      tagName,
			Platforms: []types.ContainerRegistryTagPlatform{},
			Error:     err.Error(),
		}
	}
	tag := func(platforms []types.ContainerRegistryTagPlatform, created *time.Time, detailsErr error) types.ContainerRegistryRepositoryTag {
		var size int64
		for _, platform := range platforms {
			size += platform.Size
		}
		return types.ContainerRegistryRepositoryTag{
			Name:      tagName,
			Digest:    descriptor.Digest.String(),
			MediaType: string(descriptor.MediaType),
			Size:      size,
			Created:   created,
			Platforms: platforms,
			Error:     kit.Ternary(detailsErr == nil, "", fmt.Sprint(detailsErr)),
		}
	}

	if !descriptor.MediaType.IsIndex() {
		img, imageErr := descriptor.Image()
		if imageErr != nil {
			return tag([]types.ContainerRegistryTagPlatform{}, nil, imageErr)
		}
		config, imageErr := img.ConfigFile()
		if imageErr != nil {
			return tag([]types.ContainerRegistryTagPlatform{}, nil, imageErr)
		}
		platform, imageErr := tagPlatform(img, config.Platform(), descriptor.Digest)
		if imageErr != nil {
			return tag([]types.ContainerRegistryTagPlatform{}, nil, imageErr)
		}
		return tag(
			[]types.ContainerRegistryTagPlatform{platform},
			kit.Ternary(config.Created.IsZero(), nil, new(config.Created.UTC())),
			nil,
		)
	}

	index, err := descriptor.ImageIndex()
	if err != nil {
		return tag([]types.ContainerRegistryTagPlatform{}, nil, err)
	}
	manifest, err := index.IndexManifest()
	if err != nil {
		return tag([]types.ContainerRegistryTagPlatform{}, nil, err)
	}

	var mu sync.Mutex
	platforms := []types.ContainerRegistryTagPlatform{}
	group := errgroup.Group{}
	group.SetLimit(tagDetailsConcurrency)
	for _, child := range manifest.Manifests {
		if child.Platform == nil || child.Platform.OS == "unknown" || !child.MediaType.IsImage() {
			continue
		}
		group.Go(func() error {
			img, imageErr := index.Image(child.Digest)
			if imageErr != nil {
				return imageErr
			}
			platform, imageErr := tagPlatform(img, child.Platform, child.Digest)
			if imageErr != nil {
				return imageErr
			}
			mu.Lock()
			platforms = append(platforms, platform)
			mu.Unlock()
			return nil
		})
	}
	if waitErr := group.Wait(); waitErr != nil {
		return tag(platforms, nil, waitErr)
	}

	slices.SortFunc(platforms, func(a, b types.ContainerRegistryTagPlatform) int {
		return cmp.Or(
			strings.Compare(a.OS, b.OS),
			strings.Compare(a.Architecture, b.Architecture),
			strings.Compare(a.Variant, b.Variant),
		)
	})
	return tag(platforms, nil, nil)
}

func paginateNames(names []string, requested pagination.QueryParams) ([]string, pagination.Response) {
	params := pagination.QueryParams{
		SearchQuery: requested.SearchQuery,
		SortParams:  requested.SortParams,
		Params: pagination.Params{
			Start:     requested.Start,
			Limit:     kit.Ternary(requested.Limit <= 0 || requested.Limit > maxBrowsePageSize, maxBrowsePageSize, requested.Limit),
			SkipCount: requested.SkipCount,
		},
		Filters: requested.Filters,
	}
	result := pagination.Config[string]{
		SearchAccessors: []pagination.SearchAccessor[string]{
			func(item string) (string, error) { return item, nil },
		},
		SortBindings: []pagination.SortBinding[string]{
			{Key: "name", Fn: strings.Compare},
		},
	}.SearchOrderAndPaginate(names, params)
	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params)
}
