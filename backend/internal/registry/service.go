package registry

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/cenkalti/backoff/v5"
	"github.com/containerd/errdefs"
	"github.com/distribution/reference"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	dockerregistry "github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/samber/hot"
	"github.com/samber/mo"
	"go.getarcane.app/kit/normalization"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"
	"go.getarcane.app/updater/digest"
	"go.getarcane.app/updater/refs"
	"go.getarcane.app/updater/registry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry/children/browse"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/registryauth"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/tracing"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/validation"
)

const (
	registryCacheTTL                    = 30 * time.Minute
	RegistryTypeGeneric          string = "generic"
	RegistryTypeECR              string = "ecr"
	registryPullCountKeyPrefix          = "container_registry:pulls:"
	registryRateLimitKeyPrefix          = "container_registry:rate_limits:"
	dockerHubRateLimitRepository        = "ratelimitpreview/test"
	dockerHubRateLimitTag               = "latest"
	ecrTokenTTL                         = 12 * time.Hour
	ociImageVersionLabel                = "org.opencontainers.image.version"
	versionLabelCacheTTL                = 3 * time.Hour
)

// ErrNoVersionLabel is returned when the remote image config carries no org.opencontainers.image.version label.
var ErrNoVersionLabel = errors.New("image config has no version label")

type RegistryDaemonClient interface {
	RegistryLogin(ctx context.Context, options client.RegistryLoginOptions) (client.RegistryLoginResult, error)
	DistributionInspect(ctx context.Context, imageRef string, options client.DistributionInspectOptions) (client.DistributionInspectResult, error)
}

type registryDaemonGetter func(context.Context) (RegistryDaemonClient, error)

type resolvedRegistryCredential struct {
	Username      string
	Token         string
	ServerAddress string
}

type registryRateLimitCacheEntry struct {
	RateLimit registry.RateLimitInfo `json:"rateLimit"`
	CheckedAt time.Time              `json:"checkedAt"`
}

type rateLimitRoundTripFunc func(*http.Request) (*http.Response, error)

func (f rateLimitRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type ContainerRegistryService struct {
	db                     *database.DB
	dockerClient           registryDaemonGetter
	cache                  *hot.HotCache[string, string]
	labelCache             *hot.HotCache[string, string]
	ecrRefreshGroup        singleflight.Group
	distributionHTTPClient *http.Client
	kvService              *kv.KVService
	settingsService        *settings.SettingsService
	browse                 *browse.Service
}

// NewContainerRegistryService creates a registry service. kvService may be nil
// in tests that do not need pull tracking or rate-limit caching.
func NewContainerRegistryService(
	db *database.DB,
	dockerClient registryDaemonGetter,
	kvService *kv.KVService,
	settingsService *settings.SettingsService,
	distributionHTTPClients ...*http.Client,
) *ContainerRegistryService {
	rawHTTPClient := cmp.Or(slices.Concat(distributionHTTPClients, []*http.Client{nil})[0], &http.Client{Timeout: 30 * time.Second})
	// Trace a copy so the injected client keeps its raw transport.
	distributionHTTPClient := &http.Client{
		Transport:     otelhttp.NewTransport(rawHTTPClient.Transport),
		CheckRedirect: rawHTTPClient.CheckRedirect,
		Jar:           rawHTTPClient.Jar,
		Timeout:       rawHTTPClient.Timeout,
	}
	var service *ContainerRegistryService
	service = &ContainerRegistryService{
		db:           db,
		dockerClient: dockerClient,
		cache: hot.NewHotCache[string, string](hot.LRU, 4096).
			WithTTL(registryCacheTTL).
			WithRevalidation(registryCacheTTL, func(imageRefs []string) (map[string]string, error) {
				digests := make(map[string]string, len(imageRefs))
				var firstErr error
				for _, imageRef := range imageRefs {
					// Per-ref timeout so one slow registry cannot starve the rest of the batch.
					ctx, cancel := context.WithTimeout(context.Background(), timeouts.DefaultRegistry) //nolint:forbidigo // Cache revalidation runs independently of request cancellation.
					result, err := service.InspectImageDigest(ctx, imageRef, nil)
					cancel()
					if err != nil {
						slog.DebugContext(ctx, "registry revalidation failed for image", "imageRef", imageRef, "error", err)
						firstErr = cmp.Or(firstErr, err)
						continue
					}
					digests[imageRef] = result.Digest
				}
				// hot discards the whole map on error, so only a fully failed batch reports one (KeepOnError keeps the old digests).
				if len(digests) > 0 {
					return digests, nil
				}
				return nil, firstErr
			}).
			WithRevalidationErrorPolicy(hot.KeepOnError).
			WithJanitor().
			Build(),
		// No static loader: ImageVersionLabel uses GetWithLoaders so failed resolutions retry on the next request.
		labelCache: hot.NewHotCache[string, string](hot.LRU, 64).
			WithTTL(versionLabelCacheTTL).
			Build(),
		distributionHTTPClient: distributionHTTPClient,
		kvService:              kvService,
		settingsService:        settingsService,
		browse: browse.NewService(
			func(ctx context.Context, id string) (containerregistry.ContainerRegistry, *containerregistry.Credential, error) {
				reg, err := service.GetRegistryByID(ctx, id)
				if err != nil {
					return containerregistry.ContainerRegistry{}, nil, err
				}
				credential, err := service.credentialForRegistry(ctx, reg)
				switch {
				case err != nil:
					return containerregistry.ContainerRegistry{}, nil, err
				case credential == nil:
					return containerregistry.ContainerRegistry{ID: reg.ID, URL: reg.URL, Username: reg.Username, Insecure: reg.Insecure, Enabled: reg.Enabled}, nil, nil
				default:
					return containerregistry.ContainerRegistry{ID: reg.ID, URL: reg.URL, Username: reg.Username, Insecure: reg.Insecure, Enabled: reg.Enabled},
						&containerregistry.Credential{URL: reg.URL, Username: credential.Username, Token: credential.Token, Enabled: true}, nil
				}
			},
			func(ctx context.Context) (context.Context, context.CancelFunc) {
				return service.tagLookupContext(ctx)
			},
			distributionHTTPClient.Transport,
		),
	}
	return service
}

func (s *ContainerRegistryService) GetAllRegistries(ctx context.Context) ([]ContainerRegistry, error) {
	var registries []ContainerRegistry
	if err := s.db.WithContext(ctx).Find(&registries).Error; err != nil {
		return nil, fmt.Errorf("failed to get container registries: %w", err)
	}
	return registries, nil
}

func (s *ContainerRegistryService) GetRegistriesPaginated(ctx context.Context, params pagination.QueryParams) ([]containerregistry.ContainerRegistry, pagination.Response, error) {
	var registries []ContainerRegistry
	out, paginationResp, err := params.PaginateSortAndMapDB[ContainerRegistry, containerregistry.ContainerRegistry](
		pagination.ApplyBooleanFilter(
			pagination.ApplyBooleanFilter(
				pagination.ApplyLikeSearch(s.db.WithContext(ctx).Model(&ContainerRegistry{}), params.Search, "url LIKE ? OR username LIKE ? OR COALESCE(description, '') LIKE ?"),
				"enabled", params.Filters["enabled"],
			),
			"insecure", params.Filters["insecure"],
		),
		&registries,
	)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list container registries: %w", err)
	}

	return out, paginationResp, nil
}

func (s *ContainerRegistryService) GetRegistryByID(ctx context.Context, id string) (*ContainerRegistry, error) {
	var registryRecord ContainerRegistry
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&registryRecord).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.ErrContainerRegistryNotFound
		}
		return nil, fmt.Errorf("failed to get container registry: %w", err)
	}
	return &registryRecord, nil
}

func (s *ContainerRegistryService) CreateRegistry(ctx context.Context, req containerregistry.CreateContainerRegistryRequest) (*ContainerRegistry, error) {
	if err := normalization.Normalize(&req); err != nil {
		return nil, err
	}
	registryType, err := NormalizeRegistryType(req.RegistryType)
	if err != nil {
		return nil, err
	}
	repositoryNames, err := normalizeRepositoryNames(req.RepositoryNames)
	if err != nil {
		return nil, err
	}

	if registryType == RegistryTypeECR {
		if endpointErr := validateECRRegistryEndpoint(req.URL, mo.PointerToOption(req.Insecure).OrElse(false)); endpointErr != nil {
			return nil, endpointErr
		}
		if strings.TrimSpace(req.AWSRegion) == "" {
			return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "awsRegion", Err: errors.New("AWS Region is required")})
		}
		// Static keys are optional but paired; with neither, the default AWS credential chain applies.
		if strings.TrimSpace(req.AWSSecretAccessKey) != "" && strings.TrimSpace(req.AWSAccessKeyID) == "" {
			return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "awsAccessKeyId", Err: errors.New("AWS Access Key ID is required")})
		}
		if strings.TrimSpace(req.AWSAccessKeyID) != "" && strings.TrimSpace(req.AWSSecretAccessKey) == "" {
			return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "awsSecretAccessKey", Err: errors.New("AWS Secret Access Key is required")})
		}
	} else {
		if strings.TrimSpace(req.Username) == "" {
			return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "username", Err: errors.New("username is required")})
		}
		if strings.TrimSpace(req.Token) == "" {
			return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "token", Err: errors.New("token is required")})
		}
	}

	encryptedToken, err := crypto.Encrypt(kit.Ternary(registryType == RegistryTypeECR, "", req.Token))
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt token: %w", err)
	}
	encryptedSecret, err := crypto.Encrypt(kit.Ternary(registryType == RegistryTypeECR && strings.TrimSpace(req.AWSSecretAccessKey) != "", req.AWSSecretAccessKey, ""))
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt AWS secret access key: %w", err)
	}

	registryRecord := &ContainerRegistry{
		URL:                req.URL,
		Username:           kit.Ternary(registryType == RegistryTypeECR, "", req.Username),
		Token:              encryptedToken,
		Description:        req.Description,
		Insecure:           mo.PointerToOption(req.Insecure).OrElse(false),
		Enabled:            mo.PointerToOption(req.Enabled).OrElse(true),
		RegistryType:       registryType,
		RepositoryNames:    repositoryNames,
		AWSAccessKeyID:     kit.Ternary(registryType == RegistryTypeECR, req.AWSAccessKeyID, ""),
		AWSSecretAccessKey: encryptedSecret,
		AWSRegion:          kit.Ternary(registryType == RegistryTypeECR, req.AWSRegion, ""),
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if createRegistryErr := s.db.WithContext(ctx).Create(registryRecord).Error; createRegistryErr != nil {
		return nil, fmt.Errorf("failed to create registry: %w", createRegistryErr)
	}
	return registryRecord, nil
}

func (s *ContainerRegistryService) UpdateRegistry(ctx context.Context, id string, req containerregistry.UpdateContainerRegistryRequest) (*ContainerRegistry, error) {
	if err := normalization.Normalize(&req); err != nil {
		return nil, err
	}
	registryRecord, err := s.GetRegistryByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.RegistryType != nil {
		nextType, typeErr := NormalizeRegistryType(*req.RegistryType)
		if typeErr != nil {
			return nil, typeErr
		}
		if nextType != registryRecord.RegistryType {
			return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "registryType", Err: errors.New("registry type cannot be changed after creation")})
		}
	}

	if targetErr := validation.ValidateCredentialTargetChange(
		"registry URL",
		registryRecord.URL,
		req.URL,
		normalizeRegistryServerAddress,
		kit.Ternary(registryRecord.RegistryType == RegistryTypeECR,
			map[string]bool{"awsAccessKeyId": registryRecord.AWSAccessKeyID != "", "awsSecretAccessKey": registryRecord.AWSSecretAccessKey != ""},
			map[string]bool{"token": registryRecord.Token != ""},
		),
		kit.Ternary(registryRecord.RegistryType == RegistryTypeECR,
			map[string]bool{"awsAccessKeyId": kit.FromPtr(req.AWSAccessKeyID) != "", "awsSecretAccessKey": kit.FromPtr(req.AWSSecretAccessKey) != ""},
			map[string]bool{"token": kit.FromPtr(req.Token) != ""},
		),
	); targetErr != nil {
		return nil, targetErr
	}

	// RepositoryNames: nil pointer means "don't touch"; empty slice means "clear".
	repositoryNames, err := normalizeRepositoryNames(kit.FromPtr(req.RepositoryNames))
	if err != nil {
		return nil, err
	}

	if registryRecord.RegistryType == RegistryTypeECR {
		if endpointErr := validateECRRegistryEndpoint(mo.PointerToOption(req.URL).OrElse(registryRecord.URL), mo.PointerToOption(req.Insecure).OrElse(registryRecord.Insecure)); endpointErr != nil {
			return nil, endpointErr
		}
		if kit.FromPtr(req.AWSSecretAccessKey) != "" && strings.TrimSpace(mo.PointerToOption(req.AWSAccessKeyID).OrElse(registryRecord.AWSAccessKeyID)) == "" {
			return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "awsAccessKeyId", Err: errors.New("AWS Access Key ID is required")})
		}
		if strings.TrimSpace(mo.PointerToOption(req.AWSRegion).OrElse(registryRecord.AWSRegion)) == "" {
			return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "awsRegion", Err: errors.New("AWS Region is required")})
		}
		if strings.TrimSpace(mo.PointerToOption(req.AWSAccessKeyID).OrElse(registryRecord.AWSAccessKeyID)) != "" &&
			kit.FromPtr(req.AWSSecretAccessKey) == "" && registryRecord.AWSSecretAccessKey == "" {
			return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "awsSecretAccessKey", Err: errors.New("AWS Secret Access Key is required")})
		}
	} else if strings.TrimSpace(mo.PointerToOption(req.Username).OrElse(registryRecord.Username)) == "" {
		return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "username", Err: errors.New("username is required")})
	}

	encryptedToken, err := crypto.Encrypt(kit.Ternary(registryRecord.RegistryType == RegistryTypeECR, "", kit.FromPtr(req.Token)))
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt token: %w", err)
	}
	encryptedSecret, err := crypto.Encrypt(kit.Ternary(
		registryRecord.RegistryType == RegistryTypeECR && strings.TrimSpace(mo.PointerToOption(req.AWSAccessKeyID).OrElse(registryRecord.AWSAccessKeyID)) != "",
		kit.FromPtr(req.AWSSecretAccessKey),
		"",
	))
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt AWS secret access key: %w", err)
	}

	updated := ContainerRegistry{ //exhaustruct:enforce
		BaseModel:       registryRecord.BaseModel,
		URL:             mo.PointerToOption(req.URL).OrElse(registryRecord.URL),
		Username:        kit.Ternary(registryRecord.RegistryType == RegistryTypeECR, registryRecord.Username, mo.PointerToOption(req.Username).OrElse(registryRecord.Username)),
		Token:           cmp.Or(encryptedToken, registryRecord.Token),
		Description:     req.Description,
		Insecure:        mo.PointerToOption(req.Insecure).OrElse(registryRecord.Insecure),
		Enabled:         mo.PointerToOption(req.Enabled).OrElse(registryRecord.Enabled),
		RegistryType:    registryRecord.RegistryType,
		RepositoryNames: kit.Ternary(req.RepositoryNames == nil, registryRecord.RepositoryNames, repositoryNames),
		AWSAccessKeyID: kit.Ternary(registryRecord.RegistryType == RegistryTypeECR,
			mo.PointerToOption(req.AWSAccessKeyID).OrElse(registryRecord.AWSAccessKeyID), registryRecord.AWSAccessKeyID),
		// No static key: the default AWS credential chain applies, so drop any stored secret.
		AWSSecretAccessKey: kit.Ternary(
			registryRecord.RegistryType == RegistryTypeECR && strings.TrimSpace(mo.PointerToOption(req.AWSAccessKeyID).OrElse(registryRecord.AWSAccessKeyID)) == "",
			"",
			cmp.Or(encryptedSecret, registryRecord.AWSSecretAccessKey),
		),
		AWSRegion: kit.Ternary(registryRecord.RegistryType == RegistryTypeECR, mo.PointerToOption(req.AWSRegion).OrElse(registryRecord.AWSRegion), registryRecord.AWSRegion),
		ECRToken: kit.Ternary(
			registryRecord.RegistryType == RegistryTypeECR && (req.AWSAccessKeyID != nil || req.AWSSecretAccessKey != nil || req.AWSRegion != nil),
			"",
			registryRecord.ECRToken,
		),
		ECRTokenGeneratedAt: kit.Ternary(
			registryRecord.RegistryType == RegistryTypeECR && (req.AWSAccessKeyID != nil || req.AWSSecretAccessKey != nil || req.AWSRegion != nil),
			nil,
			registryRecord.ECRTokenGeneratedAt,
		),
		CreatedAt: registryRecord.CreatedAt,
		UpdatedAt: time.Now(),
	}
	if updateRegistryErr := s.db.WithContext(ctx).Save(&updated).Error; updateRegistryErr != nil {
		return nil, fmt.Errorf("failed to update registry: %w", updateRegistryErr)
	}
	return &updated, nil
}

func (s *ContainerRegistryService) DeleteRegistry(ctx context.Context, id string) error {
	if err := s.db.WithContext(ctx).Where("id = ?", id).Delete(&ContainerRegistry{}).Error; err != nil {
		return fmt.Errorf("failed to delete container registry: %w", err)
	}
	return nil
}

// GetDecryptedToken returns the decrypted token for a registry
func (s *ContainerRegistryService) GetDecryptedToken(ctx context.Context, id string) (string, error) {
	registryRecord, err := s.GetRegistryByID(ctx, id)
	if err != nil {
		return "", err
	}

	decryptedToken, err := crypto.Decrypt(registryRecord.Token)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt token: %w", err)
	}

	return decryptedToken, nil
}

// GetEnabledRegistries returns all enabled registries
func (s *ContainerRegistryService) GetEnabledRegistries(ctx context.Context) ([]ContainerRegistry, error) {
	var registries []ContainerRegistry
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&registries).Error; err != nil {
		return nil, fmt.Errorf("failed to get enabled container registries: %w", err)
	}
	return registries, nil
}

// GetRegistryAuthForImage returns X-Registry-Auth for the image's registry host.
// A nil receiver is allowed: build.BuildService may hold no registry service behind a RegistryAuthProvider.
func (s *ContainerRegistryService) GetRegistryAuthForImage(ctx context.Context, imageRef string) (string, error) {
	if s == nil {
		return "", nil
	}
	registryHost, err := registryauth.GetRegistryAddress(imageRef)
	if err != nil {
		return "", err
	}
	return s.GetRegistryAuthForHost(ctx, registryHost)
}

// GetRegistryAuthForHost returns X-Registry-Auth for a configured and enabled registry.
func (s *ContainerRegistryService) GetRegistryAuthForHost(ctx context.Context, registryHost string) (string, error) {
	if s == nil || registryauth.NormalizeRegistryForComparison(registryHost) == "" {
		return "", nil
	}

	authConfigs, err := s.GetAllRegistryAuthConfigs(ctx)
	if err != nil {
		return "", err
	}

	cfg, ok := authConfigs[registryauth.NormalizeRegistryForComparison(registryHost)]
	if !ok {
		return "", nil
	}

	return registryauth.EncodeAuthHeader(cfg.Username, cfg.Password, cfg.ServerAddress)
}

func (s *ContainerRegistryService) GetAllRegistryAuthConfigs(ctx context.Context) (map[string]dockerregistry.AuthConfig, error) {
	if s == nil {
		return nil, nil
	}
	registries, err := s.GetEnabledRegistries(ctx)
	if err != nil {
		return nil, err
	}

	authConfigs := make(map[string]dockerregistry.AuthConfig, len(registries))
	for i := range registries {
		credential, credErr := s.credentialForRegistry(ctx, &registries[i])
		if credErr != nil {
			slog.WarnContext(ctx, "failed to resolve registry credential, skipping", "registry", registries[i].URL, "error", credErr)
			continue
		}
		if credential == nil {
			continue
		}
		for _, key := range registryauth.LookupKeys(registries[i].URL) {
			authConfigs[key] = dockerregistry.AuthConfig{
				Username:      credential.Username,
				Password:      credential.Token,
				ServerAddress: credential.ServerAddress,
			}
		}
	}

	return kit.Ternary(len(authConfigs) == 0, nil, authConfigs), nil
}

// RecordImagePull increments Arcane's observed successful pull counter for an image registry.
func (s *ContainerRegistryService) RecordImagePull(ctx context.Context, imageRef string) error {
	if s.kvService == nil {
		return nil
	}

	registryHost, err := registryauth.GetRegistryAddress(imageRef)
	if err != nil {
		return fmt.Errorf("parse image registry for %q: %w", imageRef, err)
	}
	if registryauth.NormalizeRegistryForComparison(registryHost) == "" {
		return nil
	}

	_, err = s.kvService.IncrementInt64(ctx, registryPullCountKeyPrefix+registryauth.NormalizeRegistryForComparison(registryHost), 1)
	return err
}

// GetRegistryPullUsage returns pull usage visibility for configured registries.
func (s *ContainerRegistryService) GetRegistryPullUsage(ctx context.Context) (containerregistry.PullUsageResponse, error) {
	registries, err := s.GetAllRegistries(ctx)
	if err != nil {
		return containerregistry.PullUsageResponse{}, err
	}

	results := make([]containerregistry.PullUsage, 0, len(registries))
	for _, reg := range registries {
		results = append(results, s.buildRegistryPullUsage(ctx, reg))
	}

	return containerregistry.PullUsageResponse{Registries: results}, nil
}

func (s *ContainerRegistryService) buildRegistryPullUsage(ctx context.Context, reg ContainerRegistry) containerregistry.PullUsage {
	credential, credErr := func() (*resolvedRegistryCredential, error) {
		if registryauth.NormalizeRegistryForComparison(reg.URL) != "docker.io" || !reg.Enabled || reg.RegistryType != RegistryTypeGeneric {
			return nil, nil
		}
		return s.credentialForRegistry(ctx, &reg)
	}()
	rateLimit, rateLimitErr := func() (registryRateLimitCacheEntry, error) {
		switch {
		case registryauth.NormalizeRegistryForComparison(reg.URL) != "docker.io" || !reg.Enabled:
			return registryRateLimitCacheEntry{CheckedAt: time.Now().UTC()}, nil
		case credErr != nil:
			return registryRateLimitCacheEntry{CheckedAt: time.Now().UTC()}, credErr
		}
		if cached, cachedAt, ok := s.getCachedRateLimit(ctx, reg.ID); ok {
			return registryRateLimitCacheEntry{RateLimit: *cached, CheckedAt: cachedAt}, nil
		}

		fetched, fetchErr := registry.FetchRegistryRateLimit(ctx, "docker.io", dockerHubRateLimitRepository, dockerHubRateLimitTag, func() *authn.AuthConfig {
			if credential == nil {
				return nil
			}
			return &authn.AuthConfig{Username: credential.Username, Password: credential.Token}
		}(), &http.Client{
			Transport: rateLimitRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method == http.MethodGet &&
					req.URL.Host == "registry-1.docker.io" &&
					req.URL.Path == "/v2/"+dockerHubRateLimitRepository+"/manifests/"+dockerHubRateLimitTag {
					rewritten := req.Clone(req.Context())
					rewritten.Method = http.MethodHead // keep: http.Request is only copyable via Clone
					return cmp.Or(s.distributionHTTPClient.Transport, http.DefaultTransport).RoundTrip(rewritten)
				}
				return cmp.Or(s.distributionHTTPClient.Transport, http.DefaultTransport).RoundTrip(req)
			}),
			CheckRedirect: s.distributionHTTPClient.CheckRedirect,
			Jar:           s.distributionHTTPClient.Jar,
			Timeout:       s.distributionHTTPClient.Timeout,
		})
		if fetchErr != nil {
			return registryRateLimitCacheEntry{CheckedAt: time.Now().UTC()}, fetchErr
		}
		if s.kvService == nil {
			return registryRateLimitCacheEntry{RateLimit: *fetched, CheckedAt: time.Now().UTC()}, nil
		}
		payload, encodeErr := json.Marshal(registryRateLimitCacheEntry{RateLimit: *fetched, CheckedAt: time.Now().UTC()})
		if encodeErr == nil {
			encodeErr = s.kvService.Set(ctx, registryRateLimitKeyPrefix+reg.ID, string(payload))
		}
		if encodeErr != nil {
			slog.WarnContext(ctx, "failed to save registry rate limit cache", "registryId", reg.ID, "error", encodeErr)
		}
		return registryRateLimitCacheEntry{RateLimit: *fetched, CheckedAt: time.Now().UTC()}, nil
	}()

	return containerregistry.PullUsage{
		RegistryID: reg.ID,
		Provider: func() string {
			switch {
			case reg.RegistryType == RegistryTypeECR:
				return "ecr"
			case registryauth.NormalizeRegistryForComparison(reg.URL) == "docker.io":
				return "dockerhub"
			default:
				return "generic"
			}
		}(),
		Registry: registryauth.NormalizeRegistryForComparison(reg.URL),
		DisplayName: func() string {
			switch {
			case reg.RegistryType == RegistryTypeECR:
				return "Amazon ECR"
			case registryauth.NormalizeRegistryForComparison(reg.URL) == "" || registryauth.NormalizeRegistryForComparison(reg.URL) == "docker.io":
				return "Docker Hub"
			case strings.Contains(registryauth.NormalizeRegistryForComparison(reg.URL), "ghcr.io"):
				return "GitHub Container Registry"
			case strings.Contains(registryauth.NormalizeRegistryForComparison(reg.URL), "gcr.io"):
				return "Google Container Registry"
			case strings.Contains(registryauth.NormalizeRegistryForComparison(reg.URL), "quay.io"):
				return "Quay.io Registry"
			default:
				return registryauth.NormalizeRegistryForComparison(reg.URL)
			}
		}(),
		Repository: kit.Ternary(registryauth.NormalizeRegistryForComparison(reg.URL) == "docker.io" && reg.Enabled, dockerHubRateLimitRepository, ""),
		Limit:      rateLimit.RateLimit.Limit,
		Remaining:  rateLimit.RateLimit.Remaining,
		Used: cmp.Or(rateLimit.RateLimit.Used, kit.Ternary(
			rateLimit.RateLimit.Limit != nil && rateLimit.RateLimit.Remaining != nil,
			new(max(kit.FromPtr(rateLimit.RateLimit.Limit)-kit.FromPtr(rateLimit.RateLimit.Remaining), 0)),
			nil,
		)),
		WindowSeconds: rateLimit.RateLimit.WindowSeconds,
		ObservedPulls: func() int64 {
			if s.kvService == nil || registryauth.NormalizeRegistryForComparison(reg.URL) == "" {
				return 0
			}
			observedPulls, pullsErr := s.kvService.GetInt64(ctx, registryPullCountKeyPrefix+registryauth.NormalizeRegistryForComparison(reg.URL), 0)
			if pullsErr != nil {
				slog.WarnContext(ctx, "failed to read registry pull count", "registry", registryauth.NormalizeRegistryForComparison(reg.URL), "error", pullsErr)
			}
			return observedPulls
		}(),
		AuthMethod: func() string {
			switch {
			case registryauth.NormalizeRegistryForComparison(reg.URL) != "docker.io" || !reg.Enabled:
				return "unknown"
			case credential != nil || credErr != nil:
				return "credential"
			default:
				return "anonymous"
			}
		}(),
		AuthUsername: kit.Ternary(credential != nil || credErr != nil, strings.TrimSpace(reg.Username), ""),
		Source:       rateLimit.RateLimit.Source,
		CheckedAt:    rateLimit.CheckedAt,
		Error:        kit.Ternary(rateLimitErr == nil, "", fmt.Sprint(rateLimitErr)),
	}
}

func (s *ContainerRegistryService) getCachedRateLimit(ctx context.Context, registryID string) (*registry.RateLimitInfo, time.Time, bool) {
	if s.kvService == nil || registryID == "" {
		return nil, time.Time{}, false
	}

	raw, ok, err := s.kvService.Get(ctx, registryRateLimitKeyPrefix+strings.TrimSpace(registryID))
	if err != nil {
		slog.WarnContext(ctx, "failed to read registry rate limit cache", "registryId", registryID, "error", err)
		return nil, time.Time{}, false
	}
	if !ok {
		return nil, time.Time{}, false
	}

	var entry registryRateLimitCacheEntry
	if unmarshalErr := json.Unmarshal([]byte(raw), &entry); unmarshalErr != nil {
		slog.WarnContext(ctx, "failed to parse registry rate limit cache", "registryId", registryID, "error", unmarshalErr)
		return nil, time.Time{}, false
	}
	if time.Since(entry.CheckedAt) > registryCacheTTL {
		return nil, time.Time{}, false
	}

	return &entry.RateLimit, entry.CheckedAt, true
}

func (s *ContainerRegistryService) TestRegistry(ctx context.Context, registryURL, username, token string) (testErr error) {
	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "registry.test_credentials", trace.WithAttributes(
		attribute.String("arcane.registry.host", registryauth.NormalizeRegistryForComparison(registryURL)),
	))
	defer func() { tracing.End(span, testErr) }()
	if strings.TrimSpace(username) == "" && strings.TrimSpace(token) == "" {
		// No credentials configured — skip the credential test.
		return nil
	}

	dockerClient, err := s.getDockerClient(ctx)
	if err != nil {
		return err
	}

	_, err = dockerClient.RegistryLogin(ctx, client.RegistryLoginOptions{
		Username:      strings.TrimSpace(username),
		Password:      strings.TrimSpace(token),
		ServerAddress: normalizeRegistryServerAddress(registryURL),
	})
	if err != nil {
		return fmt.Errorf("registry login failed: %w", err)
	}

	return nil
}

// TestECRRegistry tests connectivity for an ECR registry by generating an auth token
// and attempting a Docker login.
func (s *ContainerRegistryService) TestECRRegistry(ctx context.Context, reg *ContainerRegistry) error {
	ecrUser, ecrPass, err := s.GetOrRefreshECRToken(ctx, reg)
	if err != nil {
		return fmt.Errorf("failed to obtain ECR token: %w", err)
	}
	return s.TestRegistry(ctx, reg.URL, ecrUser, ecrPass)
}

// ImageDigest fetches the current digest for an image:tag from the registry
// This is used for digest-based update detection for non-semver tags
func (s *ContainerRegistryService) ImageDigest(ctx context.Context, imageRef string) (string, error) {
	parts, err := refs.NormalizeReference(imageRef)
	if err != nil {
		return "", err
	}

	digestValue, found, err := s.cache.GetWithLoaders(parts.NormalizedRef, func(_ []string) (map[string]string, error) {
		loadCtx, cancel := context.WithTimeout(ctx, timeouts.DefaultRegistry)
		defer cancel()

		result, loadErr := s.InspectImageDigest(loadCtx, parts.NormalizedRef, nil)
		if loadErr != nil {
			return nil, loadErr
		}
		return map[string]string{parts.NormalizedRef: result.Digest}, nil
	})
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("registry digest cache loader returned no digest")
	}
	return digestValue, nil
}

func (s *ContainerRegistryService) InspectImageDigest(ctx context.Context, imageRef string, externalCreds []containerregistry.Credential) (inspected *containerregistry.DigestResult, digestErr error) {
	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "registry.inspect_digest", trace.WithAttributes(attribute.String("arcane.image.ref", imageRef)))
	defer func() {
		if inspected != nil && inspected.Digest != "" {
			span.SetAttributes(attribute.String("arcane.image.digest", inspected.Digest))
		}
		tracing.End(span, digestErr)
	}()
	parts, err := refs.NormalizeReference(imageRef)
	if err != nil {
		return nil, common.Classify(common.ErrValidation, err)
	}

	withDigest := func(value string, result *containerregistry.DigestResult, err error) (*containerregistry.DigestResult, error) {
		return &containerregistry.DigestResult{
			Digest:         value,
			AuthMethod:     result.AuthMethod,
			AuthUsername:   result.AuthUsername,
			AuthRegistry:   result.AuthRegistry,
			UsedCredential: result.UsedCredential,
		}, err
	}
	inspectViaDaemon := func() (*containerregistry.DigestResult, error) {
		dockerClient, clientErr := s.getDockerClient(ctx)
		if clientErr != nil {
			return nil, clientErr
		}
		return withDigest(registryOperationWithCredentials(ctx, s, parts.RegistryHost, "distribution inspect of "+parts.NormalizedRef, externalCreds,
			func(ctx context.Context, credential *resolvedRegistryCredential) (string, error) {
				authHeader, encodeErr := func() (string, error) {
					if credential == nil {
						return "", nil
					}
					return registryauth.EncodeAuthHeader(credential.Username, credential.Token, credential.ServerAddress)
				}()
				if encodeErr != nil {
					return "", fmt.Errorf("encode registry auth header for %s: %w", parts.RegistryHost, encodeErr)
				}
				inspectResult, inspectErr := dockerClient.DistributionInspect(ctx, parts.NormalizedRef, client.DistributionInspectOptions{EncodedRegistryAuth: authHeader})
				if inspectErr != nil {
					return "", inspectErr
				}
				digestValue, normalizeErr := digest.Normalize(inspectResult.Descriptor.Digest.String())
				if normalizeErr != nil {
					return "", fmt.Errorf("distribution inspect returned invalid digest for %s: %w", parts.NormalizedRef, normalizeErr)
				}
				return digestValue, nil
			}))
	}

	var lastResult *containerregistry.DigestResult
	var lastErr error
	fallbackWarningLogged := false

	_, retryErr := backoff.Retry(ctx, func() (*containerregistry.DigestResult, error) {
		result, daemonErr := inspectViaDaemon()
		lastResult, lastErr = result, daemonErr
		if daemonErr == nil {
			return result, nil
		}
		if isRateLimitError(daemonErr) {
			slog.DebugContext(ctx, "rate limited by registry, will retry", "registry", parts.RegistryHost, "imageRef", parts.NormalizedRef)
			return nil, daemonErr
		}
		if !registry.IsFallbackEligibleDaemonError(daemonErr) &&
			(browse.IsUnauthorizedRegistryError(daemonErr) || !slices.ContainsFunc([]string{
				"context deadline exceeded", "client.timeout exceeded", "i/o timeout", "tls handshake timeout",
			}, func(indicator string) bool {
				return strings.Contains(strings.ToLower(daemonErr.Error()), indicator)
			})) {
			return nil, backoff.Permanent(daemonErr)
		}

		if !fallbackWarningLogged {
			fallbackWarningLogged = true
			slog.WarnContext(ctx, "distribution inspect unavailable, falling back to direct registry digest lookup",
				"imageRef", parts.NormalizedRef,
				"registry", parts.RegistryHost,
				"error", daemonErr.Error())
		}

		fallbackResult, fallbackErr := withDigest(registryOperationWithCredentials(ctx, s, parts.RegistryHost,
			"registry manifest inspect of "+parts.RegistryHost+"/"+parts.Repository+":"+parts.Tag, externalCreds,
			func(ctx context.Context, credential *resolvedRegistryCredential) (string, error) {
				return registry.FetchDigest(ctx, parts.RegistryHost, parts.Repository, parts.Tag, func() *authn.AuthConfig {
					if credential == nil {
						return nil
					}
					return &authn.AuthConfig{Username: credential.Username, Password: credential.Token}
				}(), s.distributionHTTPClient)
			}))
		lastResult, lastErr = fallbackResult, fallbackErr
		if fallbackErr == nil {
			return fallbackResult, nil
		}
		if isRateLimitError(fallbackErr) {
			slog.DebugContext(ctx, "rate limited by registry on fallback, will retry", "registry", parts.RegistryHost, "imageRef", parts.NormalizedRef)
			return nil, fallbackErr
		}

		lastErr = fmt.Errorf("daemon digest lookup failed; registry fallback failed: %w", errors.Join(daemonErr, fallbackErr))
		if strings.Contains(strings.ToLower(daemonErr.Error()), "tls handshake timeout") {
			// A handshake timeout is transient; retry the daemon rather than giving up after one attempt.
			return nil, lastErr
		}
		return nil, backoff.Permanent(lastErr)
	}, backoff.WithBackOff(&backoff.ExponentialBackOff{
		InitialInterval:     500 * time.Millisecond,
		RandomizationFactor: 0.3,
		Multiplier:          backoff.DefaultMultiplier,
		MaxInterval:         10 * time.Second,
	}), backoff.WithMaxTries(5))

	switch {
	case retryErr == nil:
		return lastResult, nil
	case errors.Is(retryErr, context.Canceled):
		return lastResult, retryErr
	case errors.Is(retryErr, context.DeadlineExceeded):
		return lastResult, common.Classify(common.ErrTimeout, retryErr)
	default:
		return lastResult, classifyDigestError(lastErr)
	}
}

// classifyDigestError tags digest lookup failures so callers can retry
// transient ones. Retryable kinds are checked first because fallback errors are joined.
func classifyDigestError(err error) error {
	if err == nil {
		return nil
	}
	transportErr, isTransport := errors.AsType[*transport.Error](err)
	_, isNetErr := errors.AsType[net.Error](err)
	dnsErr, isDNSErr := errors.AsType[*net.DNSError](err)
	switch {
	case strings.Contains(strings.ToLower(err.Error()), "tls handshake timeout"):
		// Checked before the net.Error case so a joined daemon timeout is not reported as unavailable.
		return common.Classify(common.ErrTimeout, err)
	case isDNSErr && dnsErr.IsNotFound:
		// A registry host that does not resolve is a bad reference, not an outage worth retrying.
		return err
	case isRateLimitError(err), errdefs.IsUnavailable(err), isNetErr,
		strings.Contains(strings.ToLower(err.Error()), "connection refused"), isTransport && transportErr.StatusCode >= http.StatusInternalServerError:
		return common.Classify(common.ErrUnavailable, err)
	case errors.Is(err, context.DeadlineExceeded), errdefs.IsDeadlineExceeded(err):
		return common.Classify(common.ErrTimeout, err)
	case browse.IsUnauthorizedRegistryError(err):
		return common.Classify(common.ErrUnauthorized, err)
	case errdefs.IsNotFound(err), isTransport && transportErr.StatusCode == http.StatusNotFound, strings.Contains(strings.ToLower(err.Error()), "manifest unknown"):
		return common.Classify(common.ErrNotFound, err)
	default:
		return err
	}
}

// Stored credentials go first: anonymous requests share a per-registry quota with every other unauthenticated client.
func registryOperationWithCredentials[T any](
	ctx context.Context,
	s *ContainerRegistryService,
	registryHost, operation string,
	externalCreds []containerregistry.Credential,
	fetch func(context.Context, *resolvedRegistryCredential) (T, error),
) (T, *containerregistry.DigestResult, error) {
	registries, credErr := func() ([]ContainerRegistry, error) {
		if len(externalCreds) > 0 || s == nil || s.db == nil {
			return nil, nil
		}
		return s.GetEnabledRegistries(ctx)
	}()

	var matchedCredentials []resolvedRegistryCredential
	for _, cred := range externalCreds {
		if cred.Enabled && strings.TrimSpace(cred.Username) != "" && strings.TrimSpace(cred.Token) != "" && registryauth.IsRegistryMatch(cred.URL, registryHost) {
			matchedCredentials = append(matchedCredentials, resolvedRegistryCredential{
				Username:      strings.TrimSpace(cred.Username),
				Token:         strings.TrimSpace(cred.Token),
				ServerAddress: normalizeRegistryServerAddress(cred.URL),
			})
		}
	}
	for i := range registries {
		if !registryauth.IsRegistryMatch(registries[i].URL, registryHost) {
			continue
		}
		credential, resolveErr := s.credentialForRegistry(ctx, &registries[i])
		if resolveErr != nil {
			slog.WarnContext(ctx, "failed to resolve registry credential", "registry", registries[i].URL, "error", resolveErr)
		} else if credential != nil {
			matchedCredentials = append(matchedCredentials, *credential)
		}
	}

	var lastErr error
	var lastResult *containerregistry.DigestResult
	for _, credential := range matchedCredentials {
		lastResult = &containerregistry.DigestResult{AuthMethod: "credential", AuthUsername: credential.Username, AuthRegistry: registryHost, UsedCredential: true}
		value, fetchErr := fetch(ctx, &credential)
		if fetchErr == nil {
			return value, lastResult, nil
		}
		lastErr = fetchErr
		if !browse.IsUnauthorizedRegistryError(fetchErr) {
			return *new(T), lastResult, fmt.Errorf("%s failed with credentials: %w", operation, fetchErr)
		}
	}
	if lastErr != nil {
		// Docker Hub anonymous quotas are too small to be worth a retry; elsewhere a stale credential must not break public images.
		if registryauth.NormalizeRegistryForComparison(registryHost) == "docker.io" {
			return *new(T), lastResult, fmt.Errorf("%s failed: %w", operation, lastErr)
		}
		slog.DebugContext(ctx, "credentialed registry lookup failed, retrying anonymously",
			"registry", registryHost,
			"operation", operation,
			"error", lastErr.Error())
	}

	value, err := fetch(ctx, nil)
	switch {
	case err == nil:
		return value, &containerregistry.DigestResult{AuthMethod: "anonymous", AuthRegistry: registryHost}, nil
	case credErr != nil && browse.IsUnauthorizedRegistryError(err):
		return *new(T), &containerregistry.DigestResult{AuthMethod: "anonymous", AuthRegistry: registryHost},
			fmt.Errorf("%s: anonymous access unauthorized; credential lookup failed: %w", operation, errors.Join(err, fmt.Errorf("failed to load enabled registries: %w", credErr)))
	default:
		return *new(T), &containerregistry.DigestResult{AuthMethod: "anonymous", AuthRegistry: registryHost}, fmt.Errorf("%s failed: %w", operation, err)
	}
}

func (s *ContainerRegistryService) getDockerClient(ctx context.Context) (RegistryDaemonClient, error) {
	if s.dockerClient == nil {
		return nil, errors.New("docker client unavailable")
	}
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get docker client: %w", err)
	}
	if dockerClient == nil {
		return nil, errors.New("docker client unavailable")
	}
	return dockerClient, nil
}

// credentialForRegistry resolves the stored credential of one registry.
// It returns nil when the registry is configured for anonymous access.
func (s *ContainerRegistryService) credentialForRegistry(ctx context.Context, reg *ContainerRegistry) (*resolvedRegistryCredential, error) {
	if reg.RegistryType == RegistryTypeECR {
		ecrUser, ecrPass, err := s.GetOrRefreshECRToken(ctx, reg)
		if err != nil {
			return nil, fmt.Errorf("failed to get ECR token: %w", err)
		}
		return &resolvedRegistryCredential{
			Username:      ecrUser,
			Token:         ecrPass,
			ServerAddress: normalizeRegistryServerAddress(reg.URL),
		}, nil
	}

	if strings.TrimSpace(reg.Username) == "" || reg.Token == "" {
		return nil, nil
	}
	token, err := crypto.Decrypt(reg.Token)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt registry token: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		return nil, nil
	}

	return &resolvedRegistryCredential{
		Username:      strings.TrimSpace(reg.Username),
		Token:         strings.TrimSpace(token),
		ServerAddress: normalizeRegistryServerAddress(reg.URL),
	}, nil
}

// SyncRegistries syncs registries from a manager to this agent instance
// It creates, updates, or deletes registries to match the provided list
func (s *ContainerRegistryService) SyncRegistries(ctx context.Context, syncItems []containerregistry.Sync) error {
	if err := normalization.Normalize(&syncItems); err != nil {
		return err
	}
	existingRegistries, err := s.GetAllRegistries(ctx)
	if err != nil {
		return err
	}
	existingMap := make(map[string]ContainerRegistry, len(existingRegistries))
	for _, existing := range existingRegistries {
		existingMap[existing.ID] = existing
	}

	for _, item := range syncItems {
		registryType, typeErr := NormalizeRegistryType(item.RegistryType)
		if typeErr != nil {
			return typeErr
		}
		// Normalizing first gives the manager and the local copy the same representation, so only real changes are saved.
		repositoryNames, namesErr := normalizeRepositoryNames(item.RepositoryNames)
		if namesErr != nil {
			return namesErr
		}

		existing, exists := existingMap[item.ID]
		credential := kit.Ternary(registryType == RegistryTypeGeneric, existing.Token, existing.AWSSecretAccessKey)
		credentialChanged, applyErr := utils.ApplyEncrypted(&credential, kit.Ternary(registryType == RegistryTypeGeneric, item.Token, item.AWSSecretAccessKey))
		if applyErr != nil {
			return fmt.Errorf("failed to apply credential for registry %s: %w", item.ID, applyErr)
		}

		if exists && registryType == existing.RegistryType && !credentialChanged &&
			kit.Ternary(registryType == RegistryTypeGeneric,
				existing.Username == item.Username,
				existing.AWSAccessKeyID == item.AWSAccessKeyID && existing.AWSRegion == item.AWSRegion,
			) &&
			existing.URL == item.URL &&
			mo.PointerToOption(existing.Description) == mo.PointerToOption(item.Description) &&
			existing.Insecure == item.Insecure &&
			existing.Enabled == item.Enabled &&
			slices.Equal(existing.RepositoryNames, repositoryNames) {
			continue
		}

		if syncErr := kit.Ternary(exists, s.db.WithContext(ctx).Save, s.db.WithContext(ctx).Create)(&ContainerRegistry{ //exhaustruct:enforce
			BaseModel:          database.BaseModel{ID: item.ID, CreatedAt: existing.BaseModel.CreatedAt, UpdatedAt: existing.BaseModel.UpdatedAt},
			URL:                item.URL,
			Username:           kit.Ternary(registryType == RegistryTypeGeneric, item.Username, ""),
			Token:              kit.Ternary(registryType == RegistryTypeGeneric, credential, ""),
			Description:        item.Description,
			Insecure:           item.Insecure,
			Enabled:            item.Enabled,
			RegistryType:       registryType,
			RepositoryNames:    repositoryNames,
			AWSAccessKeyID:     kit.Ternary(registryType == RegistryTypeGeneric, "", item.AWSAccessKeyID),
			AWSSecretAccessKey: kit.Ternary(registryType == RegistryTypeGeneric, "", credential),
			AWSRegion:          kit.Ternary(registryType == RegistryTypeGeneric, "", item.AWSRegion),
			ECRToken: kit.Ternary(
				registryType == RegistryTypeGeneric || credentialChanged || existing.AWSAccessKeyID != item.AWSAccessKeyID || existing.AWSRegion != item.AWSRegion,
				"",
				existing.ECRToken,
			),
			ECRTokenGeneratedAt: kit.Ternary(
				registryType == RegistryTypeGeneric || credentialChanged || existing.AWSAccessKeyID != item.AWSAccessKeyID || existing.AWSRegion != item.AWSRegion,
				nil,
				existing.ECRTokenGeneratedAt,
			),
			CreatedAt: cmp.Or(existing.CreatedAt, time.Now()),
			UpdatedAt: time.Now(),
		}).Error; syncErr != nil {
			return fmt.Errorf("failed to sync registry %s: %w", item.ID, syncErr)
		}
	}

	for id := range existingMap {
		if slices.ContainsFunc(syncItems, func(item containerregistry.Sync) bool { return item.ID == id }) {
			continue
		}
		if deleteErr := s.DeleteRegistry(ctx, id); deleteErr != nil {
			return deleteErr
		}
	}
	return nil
}

func NormalizeRegistryType(value string) (string, error) {
	switch cmp.Or(strings.ToLower(strings.TrimSpace(value)), RegistryTypeGeneric) {
	case RegistryTypeGeneric:
		return RegistryTypeGeneric, nil
	case RegistryTypeECR:
		return RegistryTypeECR, nil
	default:
		return "", common.Classify(common.ErrValidation, &base.FieldError{Field: "registryType", Err: errors.New("registry type must be one of: generic, ecr")})
	}
}

// normalizeRepositoryNames trims, deduplicates and validates entries in order.
// It returns a non-nil slice so that GORM always serializes to a JSON array.
func normalizeRepositoryNames(raw []string) (database.StringSlice, error) {
	for _, name := range kit.Unique(kit.TrimNonEmpty(raw)) {
		// A repository name is only a path, so placeholder domain and tag segments make it a full reference.
		if _, err := reference.ParseNormalizedNamed("registry.invalid/" + name + "/placeholder:latest"); err != nil {
			return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "repositoryNames", Err: fmt.Errorf("invalid repository name %q", name)})
		}
	}
	return append(database.StringSlice{}, kit.Unique(kit.TrimNonEmpty(raw))...), nil
}

func normalizeRegistryServerAddress(registryURL string) string {
	return kit.Ternary(
		registryauth.NormalizeRegistryForComparison(registryURL) == "docker.io",
		registryauth.NormalizeRegistryURL(registryURL),
		registryauth.NormalizeRegistryForComparison(registryURL),
	)
}

func isRateLimitError(err error) bool {
	return err != nil && IsRateLimitErrorString(err.Error())
}

func IsRateLimitErrorString(msg string) bool {
	return slices.ContainsFunc([]string{"toomanyrequests", "rate limit", "too many requests", "status: 429", "status 429", "retry-after"}, func(indicator string) bool {
		return strings.Contains(strings.ToLower(msg), indicator)
	})
}

// ListImageTags discovers repository tags with the same credential precedence as
// digest checks. External credentials replace local credentials when supplied.
func (s *ContainerRegistryService) ListImageTags(ctx context.Context, imageRef string, externalCreds []containerregistry.Credential) (tagList []string, tagsErr error) {
	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "registry.list_tags", trace.WithAttributes(attribute.String("arcane.image.ref", imageRef)))
	defer func() {
		span.SetAttributes(attribute.Int("arcane.result.count", len(tagList)))
		tracing.End(span, tagsErr)
	}()
	if refs.IsDigestPinnedReference(imageRef) || refs.IsImageIDLikeReference(imageRef) {
		return nil, errors.New("cannot discover tags for an immutable image reference")
	}
	parts, err := refs.NormalizeReference(imageRef)
	if err != nil {
		return nil, err
	}
	lookupCtx, cancel := s.tagLookupContext(ctx)
	defer cancel()

	tags, _, err := registryOperationWithCredentials(lookupCtx, s, parts.RegistryHost, "registry tag listing of "+parts.NormalizedRef, externalCreds,
		func(ctx context.Context, credential *resolvedRegistryCredential) ([]string, error) {
			return registry.FetchTags(ctx, parts.RegistryHost, parts.Repository, func() *authn.AuthConfig {
				if credential == nil {
					return nil
				}
				return &authn.AuthConfig{Username: credential.Username, Password: credential.Token}
			}(), s.distributionHTTPClient)
		})
	return tags, err
}

// tagLookupContext bounds tag and manifest walks by the configured registry tag timeout.
func (s *ContainerRegistryService) tagLookupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.settingsService == nil {
		return context.WithTimeout(ctx, timeouts.DefaultRegistryTags)
	}
	return context.WithTimeout(ctx, timeouts.GetDuration(s.settingsService.GetSettingsConfig().RegistryTagTimeout.AsInt(), timeouts.DefaultRegistryTags))
}

// GetOrRefreshECRToken returns a valid ECR auth token, reusing the encrypted cached one within its 12-hour window.
// Otherwise it fetches a new token from AWS and persists it; concurrent refreshes per registry share one singleflight call.
func (s *ContainerRegistryService) GetOrRefreshECRToken(ctx context.Context, reg *ContainerRegistry) (username, password string, err error) {
	if endpointErr := validateECRRegistryEndpoint(reg.URL, reg.Insecure); endpointErr != nil {
		return "", "", endpointErr
	}
	if reg.ECRTokenGeneratedAt != nil && time.Since(reg.ECRTokenGeneratedAt.UTC()) < ecrTokenTTL && reg.ECRToken != "" {
		if decrypted, decErr := crypto.Decrypt(reg.ECRToken); decErr == nil && strings.TrimSpace(decrypted) != "" {
			return "AWS", decrypted, nil
		}
	}

	result, refreshErr, _ := s.ecrRefreshGroup.Do(reg.ID, func() (any, error) {
		// Without a static access key, the default AWS credential chain applies (env, shared config, ECS task role, EC2 instance profile).
		secretKey, decErr := crypto.Decrypt(kit.Ternary(strings.TrimSpace(reg.AWSAccessKeyID) != "", reg.AWSSecretAccessKey, ""))
		if decErr != nil {
			return nil, fmt.Errorf("failed to decrypt AWS secret key for registry %s: %w", reg.URL, decErr)
		}
		if strings.TrimSpace(reg.AWSAccessKeyID) != "" && strings.TrimSpace(secretKey) == "" {
			return nil, fmt.Errorf("AWS secret access key is empty for registry %s", reg.URL)
		}

		cfg, cfgErr := config.LoadDefaultConfig(context.WithoutCancel(ctx),
			config.WithRegion(reg.AWSRegion),
			kit.Ternary(strings.TrimSpace(reg.AWSAccessKeyID) != "",
				config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(reg.AWSAccessKeyID, strings.TrimSpace(secretKey), "")),
				func(*config.LoadOptions) error { return nil },
			),
		)
		if cfgErr != nil {
			return nil, fmt.Errorf("failed to load AWS config for registry %s: %w", reg.URL, cfgErr)
		}
		tokenOutput, ecrErr := ecr.NewFromConfig(cfg).GetAuthorizationToken(context.WithoutCancel(ctx), &ecr.GetAuthorizationTokenInput{})
		if ecrErr != nil {
			return nil, fmt.Errorf("failed to get ECR authorization token for registry %s: %w", reg.URL, ecrErr)
		}
		if len(tokenOutput.AuthorizationData) == 0 || tokenOutput.AuthorizationData[0].AuthorizationToken == nil {
			return nil, fmt.Errorf("ECR returned empty authorization data for registry %s", reg.URL)
		}

		// The token decodes to "AWS:<password>".
		decoded, decodeErr := base64.StdEncoding.DecodeString(*tokenOutput.AuthorizationData[0].AuthorizationToken)
		if decodeErr != nil {
			return nil, fmt.Errorf("failed to decode ECR token for registry %s: %w", reg.URL, decodeErr)
		}
		_, ecrPassword, found := strings.Cut(string(decoded), ":")
		if !found || ecrPassword == "" {
			return nil, fmt.Errorf("unexpected ECR token format for registry %s", reg.URL)
		}

		encryptedToken, encErr := crypto.Encrypt(ecrPassword)
		if encErr != nil {
			return nil, fmt.Errorf("failed to encrypt ECR token for registry %s: %w", reg.URL, encErr)
		}
		*reg = ContainerRegistry{ //exhaustruct:enforce
			BaseModel:           reg.BaseModel,
			URL:                 reg.URL,
			Username:            reg.Username,
			Token:               reg.Token,
			Description:         reg.Description,
			Insecure:            reg.Insecure,
			Enabled:             reg.Enabled,
			RegistryType:        reg.RegistryType,
			RepositoryNames:     reg.RepositoryNames,
			AWSAccessKeyID:      reg.AWSAccessKeyID,
			AWSSecretAccessKey:  reg.AWSSecretAccessKey,
			AWSRegion:           reg.AWSRegion,
			ECRToken:            encryptedToken,
			ECRTokenGeneratedAt: new(time.Now().UTC()),
			CreatedAt:           reg.CreatedAt,
			UpdatedAt:           reg.UpdatedAt,
		}
		if saveErr := s.db.WithContext(context.WithoutCancel(ctx)).Model(reg).Updates(map[string]any{
			"ecr_token":              reg.ECRToken,
			"ecr_token_generated_at": *reg.ECRTokenGeneratedAt,
		}).Error; saveErr != nil {
			// Non-fatal: the token is still usable for this call.
			slog.WarnContext(ctx, "failed to persist ECR token to database", "registry", reg.URL, "error", saveErr)
		}
		return ecrPassword, nil
	})
	if refreshErr != nil {
		return "", "", refreshErr
	}
	ecrPassword, ok := result.(string)
	if !ok {
		return "", "", errors.New("unexpected ECR token result type")
	}
	return "AWS", ecrPassword, nil
}

// ImageVersionLabel resolves the org.opencontainers.image.version label from the remote image config without pulling.
// Anonymous access only — this is used for Arcane's own public GHCR images.
func (s *ContainerRegistryService) ImageVersionLabel(ctx context.Context, imageRef string) (string, error) {
	if strings.TrimSpace(imageRef) == "" {
		return "", errors.New("empty image reference")
	}

	label, found, err := s.labelCache.GetWithLoaders(strings.TrimSpace(imageRef), func(_ []string) (map[string]string, error) {
		loadCtx, cancel := context.WithTimeout(ctx, timeouts.DefaultRegistry)
		defer cancel()

		parsedRef, parseErr := name.ParseReference(strings.TrimSpace(imageRef))
		if parseErr != nil {
			return nil, fmt.Errorf("invalid image reference %q: %w", strings.TrimSpace(imageRef), parseErr)
		}
		img, fetchErr := remote.Image(parsedRef, remote.WithContext(loadCtx), remote.WithPlatform(v1.Platform{OS: "linux", Architecture: runtime.GOARCH}))
		if fetchErr != nil {
			return nil, fmt.Errorf("version label fetch failed for %s: %w", strings.TrimSpace(imageRef), fetchErr)
		}
		cfg, configErr := img.ConfigFile()
		if configErr != nil {
			return nil, fmt.Errorf("version label fetch failed for %s: %w", strings.TrimSpace(imageRef), configErr)
		}
		if cfg == nil || strings.TrimSpace(cfg.Config.Labels[ociImageVersionLabel]) == "" {
			return nil, ErrNoVersionLabel
		}
		return map[string]string{strings.TrimSpace(imageRef): strings.TrimSpace(cfg.Config.Labels[ociImageVersionLabel])}, nil
	})
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("registry version label cache loader returned no label")
	}
	return label, nil
}
