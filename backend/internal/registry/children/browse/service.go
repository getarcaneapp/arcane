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

	"github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/registryauth"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/tracing"
)

const tagDetailsConcurrency = 4

// Service browses repositories and tags through the distribution API.
type Service struct {
	resolve       func(ctx context.Context, id string) (containerregistry.ContainerRegistry, *containerregistry.Credential, error)
	lookupContext func(ctx context.Context) (context.Context, context.CancelFunc)
	transport     http.RoundTripper
}

// target is a configured registry resolved for the distribution API.
type target struct {
	registry name.Registry
	// prefix is the repository namespace configured in the registry URL, if any.
	prefix      string
	nameOptions []name.Option
	options     []remote.Option
}

func NewService(
	resolve func(ctx context.Context, id string) (containerregistry.ContainerRegistry, *containerregistry.Credential, error),
	lookupContext func(ctx context.Context) (context.Context, context.CancelFunc),
	roundTripper http.RoundTripper,
) *Service {
	return &Service{resolve: resolve, lookupContext: lookupContext, transport: roundTripper}
}

// ListRepositories lists the repositories of a configured registry through the
// catalog API, limited to the namespace of the registry URL.
func (s *Service) ListRepositories(ctx context.Context, id string, params pagination.QueryParams) (listed []containerregistry.Repository, paging pagination.Response, listErr error) {
	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "registry.list_repositories", trace.WithAttributes(attribute.String("arcane.registry.id", id)))
	defer func() {
		span.SetAttributes(attribute.Int("arcane.result.count", len(listed)))
		tracing.End(span, listErr)
	}()

	lookupCtx, cancel := s.lookupContext(ctx)
	defer cancel()

	target, err := s.targetInternal(lookupCtx, id)
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
func (s *Service) ListRepositoryTags(ctx context.Context, id, repository string, params pagination.QueryParams) (listed []containerregistry.RepositoryTag, paging pagination.Response, listErr error) {
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

	target, err := s.targetInternal(lookupCtx, id)
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
			tag, tagDetailsErr := tagDetailsInternal(repo, target.options, tagName)
			if tagDetailsErr != nil {
				tag.Error = tagDetailsErr.Error()
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

	target, err := s.targetInternal(lookupCtx, id)
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
	if deleteErr := remote.Delete(repo.Digest(digest), target.options...); deleteErr != nil {
		return "", classifyBrowseErrorInternal(deleteErr, "failed to delete manifest")
	}
	return digest, nil
}

// targetInternal resolves a stored registry into a registry name, its
// namespace prefix, and the remote options carrying auth and transport.
func (s *Service) targetInternal(ctx context.Context, id string) (*target, error) {
	reg, credential, err := s.resolve(ctx, id)
	if err != nil {
		return nil, err
	}

	host, prefix := registryauth.SplitRegistryURL(reg.URL)
	nameOptions := []name.Option{name.StrictValidation}
	if reg.Insecure {
		nameOptions = append(nameOptions, name.Insecure)
	}
	registryName, err := name.NewRegistry(host, nameOptions...)
	if err != nil {
		return nil, common.Classify(common.ErrValidation, fmt.Errorf("invalid registry URL %q: %w", reg.URL, err))
	}

	authenticator := authn.Anonymous
	if credential != nil {
		authenticator = authn.FromConfig(authn.AuthConfig{Username: credential.Username, Password: credential.Token})
	}
	options := []remote.Option{remote.WithContext(ctx), remote.WithAuth(authenticator)}
	if s.transport != nil {
		options = append(options, remote.WithTransport(s.transport))
	}

	return &target{
		registry:    registryName,
		prefix:      prefix,
		nameOptions: nameOptions,
		options:     options,
	}, nil
}

// repositoryInternal parses a repository name and keeps it inside the registry namespace.
func (t *target) repositoryInternal(repository string) (name.Repository, error) {
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

// classifyBrowseErrorInternal maps distribution API failures to API error kinds.
func classifyBrowseErrorInternal(err error, message string) error {
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

	// Fallback: some Docker daemon versions return plain-text errors without
	// a typed wrapper. These known substrings cover Docker Hub, GHCR, and
	// other common OCI registries as of Docker Engine 27.x.
	errLower := strings.ToLower(err.Error())
	indicators := []string{
		"unauthorized",
		"authentication required",
		"no basic auth credentials",
		"access denied",
		"incorrect username or password",
		"status: 401",
		"status 401",
		"status: 403",
		"status 403",
	}

	for _, indicator := range indicators {
		if strings.Contains(errLower, indicator) {
			return true
		}
	}

	return false
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
		img, imageErr := descriptor.Image()
		if imageErr != nil {
			return tag, imageErr
		}
		config, imageErr := img.ConfigFile()
		if imageErr != nil {
			return tag, imageErr
		}
		platform, imageErr := tagPlatformInternal(img, config.Platform(), descriptor.Digest)
		if imageErr != nil {
			return tag, imageErr
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
			img, imageErr := index.Image(child.Digest)
			if imageErr != nil {
				return imageErr
			}
			platform, imageErr := tagPlatformInternal(img, child.Platform, child.Digest)
			if imageErr != nil {
				return imageErr
			}
			mu.Lock()
			tag.Platforms = append(tag.Platforms, platform)
			tag.Size += platform.Size
			mu.Unlock()
			return nil
		})
	}
	if waitErr := group.Wait(); waitErr != nil {
		return tag, waitErr
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
