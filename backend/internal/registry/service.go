package registry

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/cenkalti/backoff/v5"
	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	mobyRegistry "github.com/moby/moby/api/types/registry"
	mobyClient "github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/samber/mo"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	kit "kit.getarcane.app"
	"kit.getarcane.app/cache"
	"kit.getarcane.app/docker/compat"
	"kit.getarcane.app/normalization"
	kitregistry "kit.getarcane.app/registry"
	"kit.getarcane.app/sys/crypto"
	"kit.getarcane.app/updater/digest"
	"kit.getarcane.app/updater/refs"
	"kit.getarcane.app/updater/registry"
	updatertypes "kit.getarcane.app/updater/types"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/kv"
	"getarcane.dev/arcane/v3/internal/registry/children/browse"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/logging"
	"getarcane.dev/arcane/v3/pkg/pagination"
	arcaneregistry "getarcane.dev/arcane/v3/pkg/registry"
	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/arcane/v3/pkg/update"
	"getarcane.dev/arcane/v3/pkg/validation"
	"getarcane.dev/types/v3"
)

type (
	RegistryDaemonClient interface {
		RegistryLogin(ctx context.Context, options mobyClient.RegistryLoginOptions) (mobyClient.RegistryLoginResult, error)
		DistributionInspect(ctx context.Context, imageRef string, options mobyClient.DistributionInspectOptions) (mobyClient.DistributionInspectResult, error)
	}

	registryDaemonGetter func(context.Context) (RegistryDaemonClient, error)

	engineCompatibilityGetter func(context.Context) (compat.EngineCompatibilityInfo, error)

	resolvedRegistryCredential struct {
		Username      string
		Token         string
		ServerAddress string
	}

	cachedEnabledRegistry struct {
		credErr    error
		credential *resolvedRegistryCredential
		registry   ContainerRegistry
	}

	registryRateLimitCacheEntry struct {
		CheckedAt time.Time              `json:"checkedAt"`
		RateLimit registry.RateLimitInfo `json:"rateLimit"`
	}

	rateLimitRoundTripFunc func(*http.Request) (*http.Response, error)

	freshImageDigestsKey struct{}

	ImageConfigOptions struct {
		Platform    v1.Platform
		Credentials []types.ContainerRegistryCredential
	}

	ContainerRegistryService struct {
		baseRegistryTransport   http.RoundTripper
		db                      *database.DB
		dockerClient            registryDaemonGetter
		engine                  engineCompatibilityGetter
		cache                   *cache.Cache[string, string]
		digestFailures          *cache.Cache[string, error]
		configCache             *cache.Cache[string, *v1.ConfigFile]
		enabledRegistries       *cache.Cache[struct{}, []cachedEnabledRegistry]
		ecrCredentials          *cache.Cache[string, resolvedRegistryCredential]
		registryPings           *cache.Cache[string, *transport.Challenge]
		authTransports          *cache.Cache[string, http.RoundTripper]
		distributionUnsupported *cache.Cache[any, bool]
		distributionHTTPClient  *http.Client
		kvService               *kv.KVService
		settingsService         *settings.SettingsService
		browse                  *browse.Service
		ecrRefreshGroup         singleflight.Group
	}

	ContainerRegistryServiceParams struct {
		DB              *database.DB
		DockerClient    registryDaemonGetter
		Engine          engineCompatibilityGetter
		KVService       *kv.KVService
		SettingsService *settings.SettingsService
		HTTPClient      *http.Client
	}
)

const (
	registryCacheTTL                     = 30 * time.Minute
	registryDigestFailureTTL             = time.Minute
	registryMaxIdleConnsPerHost          = 32
	registryConfigCacheCapacity          = 512
	registryEnabledCacheTTL              = 5 * time.Minute
	registryPingCacheTTL                 = 5 * time.Minute
	registryAuthTransportTTL             = 4 * time.Minute
	registryAuthTokenExpiryBuffer        = 15 * time.Second
	registryAuthMinTTL                   = 5 * time.Second
	distributionUnsupportedTTL           = 5 * time.Minute
	RegistryTypeGeneric           string = "generic"
	RegistryTypeECR               string = "ecr"
	registryPullCountKeyPrefix           = "container_registry:pulls:"
	registryRateLimitKeyPrefix           = "container_registry:rate_limits:"
	dockerHubRateLimitRepository         = "ratelimitpreview/test"
	dockerHubRateLimitTag                = "latest"
	ecrTokenTTL                          = 11 * time.Hour
	OCIImageVersionLabel                 = "org.opencontainers.image.version"
	OCIImageRevisionLabel                = "org.opencontainers.image.revision"
	OCIImageSourceLabel                  = "org.opencontainers.image.source"
	OCIImageURLLabel                     = "org.opencontainers.image.url"
	OCIImageDocumentationLabel           = "org.opencontainers.image.documentation"
	versionLabelCacheTTL                 = 3 * time.Hour
	digestRetryInitialInterval           = 500 * time.Millisecond
	digestRetryMaxInterval               = 10 * time.Second
)

func (f rateLimitRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// NewContainerRegistryService allows a nil kvService in tests without pull tracking or rate-limit caching.
func NewContainerRegistryService(p ContainerRegistryServiceParams) *ContainerRegistryService {
	rawHTTPClient := p.HTTPClient
	if rawHTTPClient == nil {
		rawHTTPClient = httpx.NewHTTPClient(httpx.ClientConfig{
			TransportConfig: httpx.TransportConfig{
				MaxIdleConnsPerHost: registryMaxIdleConnsPerHost,
			},
			Timeout: timeouts.DefaultRegistry,
		})
	}
	// Trace a copy so the injected client keeps its raw transport.
	distributionHTTPClient := &http.Client{
		Transport:     otelhttp.NewTransport(rawHTTPClient.Transport),
		CheckRedirect: rawHTTPClient.CheckRedirect,
		Jar:           rawHTTPClient.Jar,
		Timeout:       rawHTTPClient.Timeout,
	}
	baseRegistryTransport := transport.NewUserAgent(
		transport.NewRetry(
			cmp.Or(distributionHTTPClient.Transport, http.DefaultTransport),
			transport.WithRetryPredicate(isRetryableRegistryTransportError),
			transport.WithRetryStatusCodes(registryRetryStatusCodes...),
		),
		"",
	)
	var service *ContainerRegistryService
	service = &ContainerRegistryService{
		db:           p.DB,
		dockerClient: p.DockerClient,
		engine:       p.Engine,
		cache: &cache.Cache[string, string]{
			Revalidate: func(imageRef string) (string, bool, error) {
				ctx, cancel := context.WithTimeout(context.Background(), timeouts.DefaultRegistry) //nolint:forbidigo // Cache revalidation runs independently of request cancellation.
				defer cancel()
				result, err := service.InspectImageDigest(ctx, imageRef, nil)
				if err != nil {
					slog.DebugContext(ctx, "registry revalidation failed for image", "imageRef", imageRef, "error", err)
					return "", false, err
				}
				return result.Digest, true, nil
			},
			Capacity:        4096,
			TTL:             registryCacheTTL,
			JanitorInterval: registryCacheTTL,
			RevalidateAfter: registryCacheTTL,
		},
		digestFailures: &cache.Cache[string, error]{
			Capacity:        1024,
			TTL:             registryDigestFailureTTL,
			JanitorInterval: registryDigestFailureTTL,
		},
		// No static loader: ImageConfig uses GetOrLoad so failed resolutions retry on the next request.
		configCache: &cache.Cache[string, *v1.ConfigFile]{
			Capacity: registryConfigCacheCapacity,
			TTL:      versionLabelCacheTTL,
		},
		enabledRegistries: &cache.Cache[struct{}, []cachedEnabledRegistry]{
			Capacity: 1,
			TTL:      registryEnabledCacheTTL,
		},
		// One AWS authorization token per registry per scan: the token outlives the cache, so a scan never asks AWS twice.
		ecrCredentials: &cache.Cache[string, resolvedRegistryCredential]{
			Capacity: 16,
			TTL:      ecrTokenTTL,
		},
		registryPings: &cache.Cache[string, *transport.Challenge]{
			Capacity: 128,
			TTL:      registryPingCacheTTL,
		},
		authTransports: &cache.Cache[string, http.RoundTripper]{
			Capacity: 1024,
			TTL:      registryAuthTransportTTL,
		},
		distributionUnsupported: &cache.Cache[any, bool]{
			Capacity: 8,
			TTL:      distributionUnsupportedTTL,
		},
		distributionHTTPClient: distributionHTTPClient,
		baseRegistryTransport:  baseRegistryTransport,
		kvService:              p.KVService,
		settingsService:        p.SettingsService,
		browse: browse.NewService(
			func(ctx context.Context, id string) (types.ContainerRegistry, *types.ContainerRegistryCredential, error) {
				reg, err := service.GetRegistryByID(ctx, id)
				if err != nil {
					return types.ContainerRegistry{}, nil, err
				}
				credential, err := service.credentialForRegistry(ctx, reg)
				switch {
				case err != nil:
					return types.ContainerRegistry{}, nil, err
				case credential == nil:
					return types.ContainerRegistry{ID: reg.ID, URL: reg.URL, Username: reg.Username, Insecure: reg.Insecure, Enabled: reg.Enabled}, nil, nil
				default:
					return types.ContainerRegistry{ID: reg.ID, URL: reg.URL, Username: reg.Username, Insecure: reg.Insecure, Enabled: reg.Enabled},
						&types.ContainerRegistryCredential{URL: reg.URL, Username: credential.Username, Token: credential.Token, Enabled: true}, nil
				}
			},
			func(ctx context.Context) (context.Context, context.CancelFunc) {
				return context.WithTimeout(ctx, timeouts.GetDuration(service.settingsService.GetSettingsConfig().RegistryTagTimeout.AsInt(), timeouts.DefaultRegistryTags))
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

func (s *ContainerRegistryService) GetRegistriesPaginated(ctx context.Context, params pagination.QueryParams) ([]types.ContainerRegistry, pagination.Response, error) {
	var registries []ContainerRegistry
	out, paginationResp, err := params.PaginateSortAndMapDB[ContainerRegistry, types.ContainerRegistry](
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

func (s *ContainerRegistryService) CreateRegistry(ctx context.Context, req types.ContainerRegistry, submitted map[string]bool) (*ContainerRegistry, error) {
	if err := normalization.Normalize(&req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.URL) == "" {
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "url", Err: errors.New("registry URL is required")})
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
		if endpointErr := validateECRRegistryEndpoint(req.URL, req.Insecure); endpointErr != nil {
			return nil, endpointErr
		}
		if strings.TrimSpace(req.AWSRegion) == "" {
			return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "awsRegion", Err: errors.New("AWS Region is required")})
		}
		// Static keys are optional but paired; with neither, the default AWS credential chain applies.
		if strings.TrimSpace(req.AWSSecretAccessKey) != "" && strings.TrimSpace(req.AWSAccessKeyID) == "" {
			return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "awsAccessKeyId", Err: errors.New("AWS Access Key ID is required")})
		}
		if strings.TrimSpace(req.AWSAccessKeyID) != "" && strings.TrimSpace(req.AWSSecretAccessKey) == "" {
			return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "awsSecretAccessKey", Err: errors.New("AWS Secret Access Key is required")})
		}
	} else {
		if strings.TrimSpace(req.Username) == "" {
			return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "username", Err: errors.New("username is required")})
		}
		if strings.TrimSpace(req.Token) == "" {
			return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "token", Err: errors.New("token is required")})
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
		Insecure:           req.Insecure,
		Enabled:            kit.Ternary(submitted["enabled"], req.Enabled, true),
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
	s.invalidateRegistryCaches()
	return registryRecord, nil
}

func (s *ContainerRegistryService) UpdateRegistry(ctx context.Context, id string, req types.ContainerRegistry, submitted map[string]bool) (*ContainerRegistry, error) {
	if err := normalization.Normalize(&req); err != nil {
		return nil, err
	}
	if submitted["url"] && strings.TrimSpace(req.URL) == "" {
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "url", Err: errors.New("registry URL is required")})
	}
	registryRecord, err := s.GetRegistryByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if submitted["registryType"] {
		nextType, typeErr := NormalizeRegistryType(req.RegistryType)
		if typeErr != nil {
			return nil, typeErr
		}
		if nextType != registryRecord.RegistryType {
			return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "registryType", Err: errors.New("registry type cannot be changed after creation")})
		}
	}

	if targetErr := validation.ValidateCredentialTargetChange(
		"registry URL",
		registryRecord.URL,
		kit.Ternary(submitted["url"], &req.URL, nil),
		normalizeRegistryServerAddress,
		kit.Ternary(registryRecord.RegistryType == RegistryTypeECR,
			map[string]bool{"awsAccessKeyId": registryRecord.AWSAccessKeyID != "", "awsSecretAccessKey": registryRecord.AWSSecretAccessKey != ""},
			map[string]bool{"token": registryRecord.Token != ""},
		),
		kit.Ternary(registryRecord.RegistryType == RegistryTypeECR,
			map[string]bool{"awsAccessKeyId": req.AWSAccessKeyID != "", "awsSecretAccessKey": req.AWSSecretAccessKey != ""},
			map[string]bool{"token": req.Token != ""},
		),
	); targetErr != nil {
		return nil, targetErr
	}

	repositoryNames, err := normalizeRepositoryNames(req.RepositoryNames)
	if err != nil {
		return nil, err
	}

	awsAccessKeyID := kit.Ternary(submitted["awsAccessKeyId"], req.AWSAccessKeyID, registryRecord.AWSAccessKeyID)
	if registryRecord.RegistryType == RegistryTypeECR {
		if endpointErr := validateECRRegistryEndpoint(
			kit.Ternary(submitted["url"], req.URL, registryRecord.URL),
			kit.Ternary(submitted["insecure"], req.Insecure, registryRecord.Insecure),
		); endpointErr != nil {
			return nil, endpointErr
		}
		if req.AWSSecretAccessKey != "" && strings.TrimSpace(awsAccessKeyID) == "" {
			return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "awsAccessKeyId", Err: errors.New("AWS Access Key ID is required")})
		}
		if strings.TrimSpace(kit.Ternary(submitted["awsRegion"], req.AWSRegion, registryRecord.AWSRegion)) == "" {
			return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "awsRegion", Err: errors.New("AWS Region is required")})
		}
		if strings.TrimSpace(awsAccessKeyID) != "" &&
			req.AWSSecretAccessKey == "" && registryRecord.AWSSecretAccessKey == "" {
			return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "awsSecretAccessKey", Err: errors.New("AWS Secret Access Key is required")})
		}
	} else if strings.TrimSpace(kit.Ternary(submitted["username"], req.Username, registryRecord.Username)) == "" {
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "username", Err: errors.New("username is required")})
	}

	encryptedToken, err := crypto.Encrypt(kit.Ternary(registryRecord.RegistryType == RegistryTypeECR, "", req.Token))
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt token: %w", err)
	}
	encryptedSecret, err := crypto.Encrypt(kit.Ternary(
		registryRecord.RegistryType == RegistryTypeECR && strings.TrimSpace(awsAccessKeyID) != "",
		req.AWSSecretAccessKey,
		"",
	))
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt AWS secret access key: %w", err)
	}

	updated := ContainerRegistry{ //exhaustruct:enforce
		BaseModel:       registryRecord.BaseModel,
		URL:             kit.Ternary(submitted["url"], req.URL, registryRecord.URL),
		Username:        kit.Ternary(registryRecord.RegistryType != RegistryTypeECR && submitted["username"], req.Username, registryRecord.Username),
		Token:           cmp.Or(encryptedToken, registryRecord.Token),
		Description:     kit.Ternary(submitted["description"], req.Description, registryRecord.Description),
		Insecure:        kit.Ternary(submitted["insecure"], req.Insecure, registryRecord.Insecure),
		Enabled:         kit.Ternary(submitted["enabled"], req.Enabled, registryRecord.Enabled),
		RegistryType:    registryRecord.RegistryType,
		RepositoryNames: kit.Ternary(submitted["repositoryNames"], repositoryNames, registryRecord.RepositoryNames),
		AWSAccessKeyID:  kit.Ternary(registryRecord.RegistryType == RegistryTypeECR, awsAccessKeyID, registryRecord.AWSAccessKeyID),
		// No static key: the default AWS credential chain applies, so drop any stored secret.
		AWSSecretAccessKey: kit.Ternary(
			registryRecord.RegistryType == RegistryTypeECR && strings.TrimSpace(awsAccessKeyID) == "",
			"",
			cmp.Or(encryptedSecret, registryRecord.AWSSecretAccessKey),
		),
		AWSRegion: kit.Ternary(registryRecord.RegistryType == RegistryTypeECR && submitted["awsRegion"], req.AWSRegion, registryRecord.AWSRegion),
		ECRToken: kit.Ternary(
			registryRecord.RegistryType == RegistryTypeECR && (submitted["awsAccessKeyId"] || submitted["awsSecretAccessKey"] || submitted["awsRegion"]),
			"",
			registryRecord.ECRToken,
		),
		ECRTokenGeneratedAt: kit.Ternary(
			registryRecord.RegistryType == RegistryTypeECR && (submitted["awsAccessKeyId"] || submitted["awsSecretAccessKey"] || submitted["awsRegion"]),
			nil,
			registryRecord.ECRTokenGeneratedAt,
		),
		CreatedAt: registryRecord.CreatedAt,
		UpdatedAt: time.Now(),
	}
	if updateRegistryErr := s.db.WithContext(ctx).Save(&updated).Error; updateRegistryErr != nil {
		return nil, fmt.Errorf("failed to update registry: %w", updateRegistryErr)
	}
	s.invalidateRegistryCaches()
	return &updated, nil
}

func (s *ContainerRegistryService) DeleteRegistry(ctx context.Context, id string) error {
	result := s.db.WithContext(ctx).Where("id = ?", id).Delete(&ContainerRegistry{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete container registry: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return common.ErrContainerRegistryNotFound
	}
	s.invalidateRegistryCaches()
	return nil
}

func (s *ContainerRegistryService) invalidateRegistryCaches() {
	s.enabledRegistries.Purge()
	s.authTransports.Purge()
	s.ecrCredentials.Purge()
}

func (s *ContainerRegistryService) loadEnabledRegistryEntries(ctx context.Context) ([]cachedEnabledRegistry, error) {
	entries, _, err := s.enabledRegistries.GetOrLoad(struct{}{}, func(struct{}) ([]cachedEnabledRegistry, bool, error) {
		var registries []ContainerRegistry
		if queryErr := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&registries).Error; queryErr != nil {
			return nil, false, fmt.Errorf("failed to get enabled container registries: %w", queryErr)
		}
		cached := make([]cachedEnabledRegistry, len(registries))
		for i := range registries {
			var credential *resolvedRegistryCredential
			var credErr error
			if registries[i].RegistryType != RegistryTypeECR {
				credential, credErr = decryptGenericRegistryCredential(&registries[i])
			}
			cached[i] = cachedEnabledRegistry{ //exhaustruct:enforce
				credErr:    credErr,
				credential: credential,
				registry:   registries[i],
			}
		}
		return cached, true, nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *ContainerRegistryService) resolveCachedCredential(ctx context.Context, entry cachedEnabledRegistry) (*resolvedRegistryCredential, error) {
	if entry.registry.RegistryType == RegistryTypeECR {
		regCopy := entry.registry
		return s.credentialForRegistry(ctx, &regCopy)
	}
	return entry.credential, entry.credErr
}

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

// GetEnabledRegistryCredentials returns the resolved credential of every enabled registry, ECR rows included:
// their username and token come from the ECR authorization-token flow instead of the stored row.
func (s *ContainerRegistryService) GetEnabledRegistryCredentials(ctx context.Context) ([]types.ContainerRegistryCredential, error) {
	entries, err := s.loadEnabledRegistryEntries(ctx)
	if err != nil {
		return nil, err
	}
	resolved := make([]types.ContainerRegistryCredential, 0, len(entries))
	for _, entry := range entries {
		credential, credErr := s.resolveCachedCredential(ctx, entry)
		if credErr != nil {
			slog.WarnContext(ctx, "failed to resolve registry credential", "registry", entry.registry.URL, "error", credErr)
			continue
		}
		if credential == nil {
			continue
		}
		resolved = append(resolved, types.ContainerRegistryCredential{
			URL:      entry.registry.URL,
			Username: credential.Username,
			Token:    credential.Token,
			Enabled:  entry.registry.Enabled,
		})
	}
	return resolved, nil
}

func (s *ContainerRegistryService) GetRegistryAuthForImage(ctx context.Context, imageRef string) (string, error) {
	entries, err := s.loadEnabledRegistryEntries(ctx)
	if err != nil {
		return "", err
	}
	candidates, err := arcaneregistry.MatchingForImage(imageRef, entries, func(entry cachedEnabledRegistry) string { return entry.registry.URL })
	if err != nil {
		return "", err
	}

	for _, candidate := range candidates {
		credential, credErr := s.resolveCachedCredential(ctx, candidate)
		if credErr != nil {
			slog.WarnContext(ctx, "failed to resolve registry credential, skipping", "registry", candidate.registry.URL, "error", credErr)
			continue
		}
		if credential != nil {
			return arcaneregistry.EncodeAuthHeader(credential.Username, credential.Token, credential.ServerAddress)
		}
	}
	return "", nil
}

func (s *ContainerRegistryService) GetAllRegistryAuthConfigs(ctx context.Context) (map[string]mobyRegistry.AuthConfig, error) {
	entries, err := s.loadEnabledRegistryEntries(ctx)
	if err != nil {
		return nil, err
	}

	authConfigs := make(map[string]mobyRegistry.AuthConfig, len(entries))
	for _, entry := range entries {
		credential, credErr := s.resolveCachedCredential(ctx, entry)
		if credErr != nil {
			slog.WarnContext(ctx, "failed to resolve registry credential, skipping", "registry", entry.registry.URL, "error", credErr)
			continue
		}
		if credential == nil {
			continue
		}
		for _, key := range kitregistry.LookupKeys(entry.registry.URL) {
			authConfigs[key] = mobyRegistry.AuthConfig{
				Username:      credential.Username,
				Password:      credential.Token,
				ServerAddress: credential.ServerAddress,
			}
		}
	}

	return kit.Ternary(len(authConfigs) == 0, nil, authConfigs), nil
}

func (s *ContainerRegistryService) RecordImagePull(ctx context.Context, imageRef string) error {
	registryHost, err := arcaneregistry.GetRegistryAddress(imageRef)
	if err != nil {
		return fmt.Errorf("parse image registry for %q: %w", imageRef, err)
	}
	if kitregistry.Normalize(registryHost) == "" {
		return nil
	}

	_, err = s.kvService.IncrementInt64(ctx, registryPullCountKeyPrefix+kitregistry.Normalize(registryHost), 1)
	return err
}

func (s *ContainerRegistryService) GetRegistryPullUsage(ctx context.Context) (types.ContainerRegistryPullUsageResponse, error) {
	registries, err := s.GetAllRegistries(ctx)
	if err != nil {
		return types.ContainerRegistryPullUsageResponse{}, err
	}

	results := make([]types.ContainerRegistryPullUsage, 0, len(registries))
	for _, reg := range registries {
		results = append(results, s.buildRegistryPullUsage(ctx, reg))
	}

	return types.ContainerRegistryPullUsageResponse{Registries: results}, nil
}

func (s *ContainerRegistryService) buildRegistryPullUsage(ctx context.Context, reg ContainerRegistry) types.ContainerRegistryPullUsage {
	credential, credErr := func() (*resolvedRegistryCredential, error) {
		if kitregistry.Normalize(reg.URL) != "docker.io" || !reg.Enabled || reg.RegistryType != RegistryTypeGeneric {
			return nil, nil
		}
		return s.credentialForRegistry(ctx, &reg)
	}()
	rateLimit, rateLimitErr := func() (registryRateLimitCacheEntry, error) {
		switch {
		case kitregistry.Normalize(reg.URL) != "docker.io" || !reg.Enabled:
			return registryRateLimitCacheEntry{CheckedAt: time.Now().UTC()}, nil
		case credErr != nil:
			return registryRateLimitCacheEntry{CheckedAt: time.Now().UTC()}, credErr
		}
		if cached, cachedAt, ok := s.getCachedRateLimit(ctx, reg.ID); ok {
			return registryRateLimitCacheEntry{RateLimit: *cached, CheckedAt: cachedAt}, nil
		}

		fetched, fetchErr := registry.FetchRegistryRateLimit(ctx, registry.Request{
			Credential: func() *authn.AuthConfig {
				if credential == nil {
					return nil
				}
				return &authn.AuthConfig{Username: credential.Username, Password: credential.Token}
			}(),
			HTTPClient: &http.Client{
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
			},
			Host:       "docker.io",
			Repository: dockerHubRateLimitRepository,
			Tag:        dockerHubRateLimitTag,
		})
		if fetchErr != nil {
			return registryRateLimitCacheEntry{CheckedAt: time.Now().UTC()}, fetchErr
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

	return types.ContainerRegistryPullUsage{
		RegistryID: reg.ID,
		Provider: func() string {
			switch {
			case reg.RegistryType == RegistryTypeECR:
				return "ecr"
			case kitregistry.Normalize(reg.URL) == "docker.io":
				return "dockerhub"
			default:
				return "generic"
			}
		}(),
		Registry: kitregistry.Normalize(reg.URL),
		DisplayName: func() string {
			switch {
			case reg.RegistryType == RegistryTypeECR:
				return "Amazon ECR"
			case kitregistry.Normalize(reg.URL) == "" || kitregistry.Normalize(reg.URL) == "docker.io":
				return "Docker Hub"
			case strings.Contains(kitregistry.Normalize(reg.URL), "ghcr.io"):
				return "GitHub Container Registry"
			case strings.Contains(kitregistry.Normalize(reg.URL), "gcr.io"):
				return "Google Container Registry"
			case strings.Contains(kitregistry.Normalize(reg.URL), "quay.io"):
				return "Quay.io Registry"
			default:
				return kitregistry.Normalize(reg.URL)
			}
		}(),
		Repository: kit.Ternary(kitregistry.Normalize(reg.URL) == "docker.io" && reg.Enabled, dockerHubRateLimitRepository, ""),
		Limit:      rateLimit.RateLimit.Limit,
		Remaining:  rateLimit.RateLimit.Remaining,
		Used: cmp.Or(rateLimit.RateLimit.Used, kit.Ternary(
			rateLimit.RateLimit.Limit != nil && rateLimit.RateLimit.Remaining != nil,
			new(max(kit.FromPtr(rateLimit.RateLimit.Limit)-kit.FromPtr(rateLimit.RateLimit.Remaining), 0)),
			nil,
		)),
		WindowSeconds: rateLimit.RateLimit.WindowSeconds,
		ObservedPulls: func() int64 {
			if kitregistry.Normalize(reg.URL) == "" {
				return 0
			}
			observedPulls, pullsErr := s.kvService.GetInt64(ctx, registryPullCountKeyPrefix+kitregistry.Normalize(reg.URL), 0)
			if pullsErr != nil {
				slog.WarnContext(ctx, "failed to read registry pull count", "registry", kitregistry.Normalize(reg.URL), "error", pullsErr)
			}
			return observedPulls
		}(),
		AuthMethod: func() string {
			switch {
			case kitregistry.Normalize(reg.URL) != "docker.io" || !reg.Enabled:
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
	if registryID == "" {
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
	ctx, span := otel.Tracer(logging.InstrumentationName).Start(ctx, "registry.test_credentials", trace.WithAttributes(
		attribute.String("arcane.registry.host", kitregistry.Normalize(registryURL)),
	))
	defer func() { logging.EndSpan(span, testErr) }()
	if strings.TrimSpace(username) == "" && strings.TrimSpace(token) == "" {
		return nil
	}

	dockerClient, err := s.getDockerClient(ctx)
	if err != nil {
		return err
	}

	_, err = dockerClient.RegistryLogin(ctx, mobyClient.RegistryLoginOptions{
		Username:      strings.TrimSpace(username),
		Password:      strings.TrimSpace(token),
		ServerAddress: normalizeRegistryServerAddress(registryURL),
	})
	if err != nil {
		return fmt.Errorf("registry login failed: %w", err)
	}

	return nil
}

func (s *ContainerRegistryService) TestECRRegistry(ctx context.Context, reg *ContainerRegistry) error {
	ecrUser, ecrPass, err := s.GetOrRefreshECRToken(ctx, reg)
	if err != nil {
		return fmt.Errorf("failed to obtain ECR token: %w", err)
	}
	return s.TestRegistry(ctx, reg.URL, ecrUser, ecrPass)
}

// WithFreshImageDigests makes ImageDigest calls under ctx read the registry instead of cached digests,
// lookups in flight, or recent failures, and store what they read for later lookups.
func WithFreshImageDigests(ctx context.Context) context.Context {
	return context.WithValue(ctx, freshImageDigestsKey{}, true)
}

func (s *ContainerRegistryService) ImageDigest(ctx context.Context, imageRef string) (string, error) {
	parts, err := refs.NormalizeReference(imageRef)
	if err != nil {
		return "", err
	}

	load := func(normalizedRef string) (string, bool, error) {
		loadCtx, cancel := context.WithTimeout(ctx, timeouts.DefaultRegistry)
		defer cancel()

		result, loadErr := s.InspectImageDigest(loadCtx, normalizedRef, nil)
		if loadErr != nil {
			return "", false, loadErr
		}
		return result.Digest, true, nil
	}
	var (
		digestValue string
		found       bool
	)
	if fresh, _ := ctx.Value(freshImageDigestsKey{}).(bool); fresh {
		digestValue, found, err = load(parts.NormalizedRef)
		if err == nil {
			// Delete first so a load already in flight does not overwrite the fresh digest.
			s.cache.Delete(parts.NormalizedRef)
			s.cache.Set(cache.Entry[string, string]{
				Key:   parts.NormalizedRef,
				Value: digestValue,
			})
			s.digestFailures.Delete(parts.NormalizedRef)
		}
	} else {
		if cachedErr, failed, _ := s.digestFailures.Get(parts.NormalizedRef); failed {
			return "", cachedErr
		}
		digestValue, found, err = s.cache.GetOrLoad(parts.NormalizedRef, load)
	}
	if err != nil {
		if ctx.Err() == nil {
			s.digestFailures.Set(cache.Entry[string, error]{
				Key:   parts.NormalizedRef,
				Value: err,
			})
		}
		return "", err
	}
	if !found {
		return "", errors.New("registry digest cache loader returned no digest")
	}
	return digestValue, nil
}

func (s *ContainerRegistryService) InspectImageDigest(
	ctx context.Context,
	imageRef string,
	externalCreds []types.ContainerRegistryCredential,
) (inspected *types.ContainerRegistryDigestResult, digestErr error) {
	ctx, span := otel.Tracer(logging.InstrumentationName).Start(ctx, "registry.inspect_digest", trace.WithAttributes(attribute.String("arcane.image.ref", imageRef)))
	defer func() {
		if inspected != nil && inspected.Digest != "" {
			span.SetAttributes(attribute.String("arcane.image.digest", inspected.Digest))
		}
		logging.EndSpan(span, digestErr)
	}()
	parts, err := refs.NormalizeReference(imageRef)
	if err != nil {
		return nil, common.Classify(common.ErrValidation, err)
	}

	var lastResult *types.ContainerRegistryDigestResult
	var lastErr error
	fallbackWarningLogged := false

	_, retryErr := backoff.Retry(ctx, func() (*types.ContainerRegistryDigestResult, error) {
		dockerClient, clientErr := s.getDockerClient(ctx)
		if clientErr == nil && s.shouldSkipDaemonDistributionInspect(ctx, dockerClient) {
			fallbackResult, fallbackErr := s.inspectDigestViaFallback(ctx, parts, externalCreds)
			lastResult, lastErr = fallbackResult, fallbackErr
			if fallbackErr == nil {
				return fallbackResult, nil
			}
			return nil, backoff.Permanent(fallbackErr)
		}

		result, daemonErr := func() (*types.ContainerRegistryDigestResult, error) {
			if clientErr != nil {
				return nil, clientErr
			}
			deadline, hasDeadline := ctx.Deadline()
			daemonCtx, cancelDaemon := context.WithTimeout(ctx, kit.Ternary(hasDeadline, time.Until(deadline), timeouts.DefaultRegistry)/2)
			defer cancelDaemon()
			return s.inspectDigestViaDaemon(daemonCtx, dockerClient, parts, externalCreds)
		}()
		lastResult, lastErr = result, daemonErr
		if daemonErr == nil {
			return result, nil
		}
		if clientErr == nil && isDistributionInspectUnsupportedError(daemonErr) && dockerClient != nil && reflect.TypeOf(dockerClient).Comparable() {
			s.distributionUnsupported.Set(cache.Entry[any, bool]{Key: dockerClient, Value: true})
		}
		if IsRateLimitErrorString(daemonErr.Error()) {
			slog.DebugContext(ctx, "rate limited by registry", "registry", parts.RegistryHost, "imageRef", parts.NormalizedRef)
			_, retryAfter, _ := strings.Cut(strings.ToLower(daemonErr.Error()), "retry-after:")
			retryAfter, _, _ = strings.Cut(retryAfter, ",")
			wait, parseErr := time.ParseDuration(strings.TrimSpace(retryAfter))
			if parseErr == nil && wait <= digestRetryMaxInterval {
				return nil, &backoff.RetryAfterError{Duration: max(wait, digestRetryInitialInterval)}
			}
			return nil, backoff.Permanent(daemonErr)
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

		fallbackResult, fallbackErr := s.inspectDigestViaFallback(ctx, parts, externalCreds)
		lastResult, lastErr = fallbackResult, fallbackErr
		if fallbackErr == nil {
			return fallbackResult, nil
		}
		if IsRateLimitErrorString(fallbackErr.Error()) {
			slog.DebugContext(ctx, "rate limited by registry on fallback", "registry", parts.RegistryHost, "imageRef", parts.NormalizedRef)
			return nil, backoff.Permanent(fallbackErr)
		}

		lastErr = fmt.Errorf("daemon digest lookup failed; registry fallback failed: %w", errors.Join(daemonErr, fallbackErr))
		if strings.Contains(strings.ToLower(daemonErr.Error()), "tls handshake timeout") {
			// A handshake timeout is transient; retry the daemon rather than giving up after one attempt.
			return nil, lastErr
		}
		return nil, backoff.Permanent(lastErr)
	}, backoff.WithBackOff(&backoff.ExponentialBackOff{
		InitialInterval:     digestRetryInitialInterval,
		RandomizationFactor: 0.3,
		Multiplier:          backoff.DefaultMultiplier,
		MaxInterval:         digestRetryMaxInterval,
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

func withDigestResult(value string, result *types.ContainerRegistryDigestResult, err error) (*types.ContainerRegistryDigestResult, error) {
	return &types.ContainerRegistryDigestResult{
		Digest:         value,
		AuthMethod:     result.AuthMethod,
		AuthUsername:   result.AuthUsername,
		AuthRegistry:   result.AuthRegistry,
		UsedCredential: result.UsedCredential,
	}, err
}

func (s *ContainerRegistryService) inspectDigestViaDaemon(
	ctx context.Context,
	dockerClient RegistryDaemonClient,
	parts *refs.Reference,
	externalCreds []types.ContainerRegistryCredential,
) (*types.ContainerRegistryDigestResult, error) {
	return withDigestResult(registryOperationWithCredentials(ctx, s, parts.RegistryHost, parts.NormalizedRef, "distribution inspect of "+parts.NormalizedRef, externalCreds,
		func(ctx context.Context, credential *resolvedRegistryCredential) (string, error) {
			authHeader, encodeErr := func() (string, error) {
				if credential == nil {
					return "", nil
				}
				return arcaneregistry.EncodeAuthHeader(credential.Username, credential.Token, credential.ServerAddress)
			}()
			if encodeErr != nil {
				return "", fmt.Errorf("encode registry auth header for %s: %w", parts.RegistryHost, encodeErr)
			}
			inspectResult, inspectErr := dockerClient.DistributionInspect(ctx, parts.NormalizedRef, mobyClient.DistributionInspectOptions{EncodedRegistryAuth: authHeader})
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

func (s *ContainerRegistryService) inspectDigestViaFallback(
	ctx context.Context,
	parts *refs.Reference,
	externalCreds []types.ContainerRegistryCredential,
) (*types.ContainerRegistryDigestResult, error) {
	return withDigestResult(registryOperationWithCredentials(ctx, s, parts.RegistryHost, parts.NormalizedRef,
		"registry manifest inspect of "+parts.RegistryHost+"/"+parts.Repository+":"+parts.Tag, externalCreds,
		func(ctx context.Context, credential *resolvedRegistryCredential) (string, error) {
			httpClient, transportErr := s.authorizedHTTPClient(ctx, parts.RegistryHost, parts.Repository, credential)
			if transportErr != nil {
				return "", transportErr
			}
			return registry.FetchDigest(ctx, registry.Request{
				Credential: func() *authn.AuthConfig {
					if credential == nil {
						return nil
					}
					return &authn.AuthConfig{Username: credential.Username, Password: credential.Token}
				}(),
				HTTPClient: httpClient,
				Host:       parts.RegistryHost,
				Repository: parts.Repository,
				Tag:        parts.Tag,
			})
		}))
}

// classifyDigestError checks retryable kinds first because fallback errors are joined.
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
	case IsRateLimitErrorString(err.Error()), errdefs.IsUnavailable(err), isNetErr,
		strings.Contains(strings.ToLower(err.Error()), "connection refused"), isTransport && transportErr.StatusCode >= http.StatusInternalServerError:
		return common.Classify(common.ErrUnavailable, err)
	case errors.Is(err, context.DeadlineExceeded), errdefs.IsDeadlineExceeded(err):
		return common.Classify(common.ErrTimeout, err)
	case browse.IsUnauthorizedRegistryError(err):
		return common.Classify(common.ErrBadRequest, err)
	case errdefs.IsNotFound(err), isTransport && transportErr.StatusCode == http.StatusNotFound, strings.Contains(strings.ToLower(err.Error()), "manifest unknown"):
		return common.Classify(common.ErrNotFound, err)
	default:
		return err
	}
}

func (s *ContainerRegistryService) shouldSkipDaemonDistributionInspect(ctx context.Context, dockerClient RegistryDaemonClient) bool {
	if dockerClient == nil || !reflect.TypeOf(dockerClient).Comparable() {
		return false
	}
	if s.engine == nil {
		unsupported, found, _ := s.distributionUnsupported.Get(dockerClient)
		return found && unsupported
	}
	unsupported, found, err := s.distributionUnsupported.GetOrLoad(dockerClient, func(any) (bool, bool, error) {
		engine, engineErr := s.engine(ctx)
		if engineErr != nil {
			return false, false, engineErr
		}
		return strings.EqualFold(strings.TrimSpace(engine.Name), "podman"), true, nil
	})
	return err == nil && found && unsupported
}

func (s *ContainerRegistryService) authorizedHTTPClient(
	ctx context.Context,
	registryHost, repository string,
	credential *resolvedRegistryCredential,
) (*http.Client, error) {
	host := kitregistry.Normalize(registryHost)
	if host == "docker.io" {
		host = defaultDockerRegistryHost
	}
	repo, err := name.NewRepository(host + "/" + strings.Trim(repository, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse registry repository: %w", err)
	}
	tr, err := s.authorizedTransport(ctx, repo, credential)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport:     tr,
		CheckRedirect: s.distributionHTTPClient.CheckRedirect,
		Jar:           s.distributionHTTPClient.Jar,
		Timeout:       s.distributionHTTPClient.Timeout,
	}, nil
}

func (s *ContainerRegistryService) authorizedTransport(
	ctx context.Context,
	repo name.Repository,
	credential *resolvedRegistryCredential,
) (http.RoundTripper, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pr, _, err := s.registryPings.GetOrLoad(repo.RegistryStr(), func(string) (*transport.Challenge, bool, error) {
		pingCtx, cancel := context.WithTimeout(ctx, timeouts.DefaultRegistry)
		defer cancel()
		challenge, pingErr := transport.Ping(pingCtx, repo.Registry, s.baseRegistryTransport)
		if pingErr != nil {
			return nil, false, pingErr
		}
		return challenge, true, nil
	})
	if err != nil {
		return nil, err
	}

	auth := func() authn.Authenticator {
		if credential == nil {
			return authn.Anonymous
		}
		return authn.FromConfig(authn.AuthConfig{Username: credential.Username, Password: credential.Token})
	}()
	schemeTransport := registrySchemeTransport{
		inner:  s.baseRegistryTransport,
		scheme: kit.Ternary(pr.Insecure, "http", "https"),
		host:   repo.RegistryStr(),
	}
	if strings.ToLower(pr.Scheme) != "bearer" {
		return transport.FromToken(repo.Registry, auth, schemeTransport, pr, nil)
	}

	credKey := "anon"
	if credential != nil {
		sum := sha256.Sum256([]byte(credential.Username + "\x00" + credential.Token))
		credKey = hex.EncodeToString(sum[:])
	}
	cacheKey := repo.RegistryStr() + "|" + repo.RepositoryStr() + "|" + credKey
	var tokenTTL time.Duration
	tr, _, err := s.authTransports.GetOrLoad(cacheKey, func(string) (http.RoundTripper, bool, error) {
		tokenCtx, cancel := context.WithTimeout(ctx, timeouts.DefaultRegistry)
		defer cancel()
		tok, exchangeErr := transport.Exchange(tokenCtx, repo.Registry, auth, schemeTransport, []string{repo.Scope(transport.PullScope)}, pr)
		if exchangeErr != nil {
			return nil, false, exchangeErr
		}
		normalizedToken := &transport.Token{ //exhaustruct:enforce
			Token:        cmp.Or(tok.Token, tok.AccessToken),
			AccessToken:  tok.AccessToken,
			RefreshToken: tok.RefreshToken,
			ExpiresIn:    tok.ExpiresIn,
		}
		if tok.ExpiresIn > 0 {
			ttl := max(time.Duration(tok.ExpiresIn)*time.Second-registryAuthTokenExpiryBuffer, registryAuthMinTTL)
			if ttl < registryAuthTransportTTL {
				tokenTTL = ttl
			}
		}
		wrapped, fromTokenErr := transport.FromToken(repo.Registry, auth, schemeTransport, pr, normalizedToken)
		if fromTokenErr != nil {
			return nil, false, fromTokenErr
		}
		return wrapped, true, nil
	})
	if err != nil {
		return nil, err
	}
	if tokenTTL > 0 {
		s.authTransports.Set(cache.Entry[string, http.RoundTripper]{
			Key:   cacheKey,
			Value: tr,
			TTL:   tokenTTL,
		})
	}
	return tr, nil
}

// Stored credentials go first: anonymous requests share a per-registry quota with every other unauthenticated client.
func registryOperationWithCredentials[T any](
	ctx context.Context,
	s *ContainerRegistryService,
	registryHost, imageRef, operation string,
	externalCreds []types.ContainerRegistryCredential,
	fetch func(context.Context, *resolvedRegistryCredential) (T, error),
) (T, *types.ContainerRegistryDigestResult, error) {
	entries, credErr := s.loadEnabledRegistryEntries(ctx)

	usableCreds := slices.DeleteFunc(slices.Clone(externalCreds), func(cred types.ContainerRegistryCredential) bool {
		return !cred.Enabled || strings.TrimSpace(cred.Username) == "" || strings.TrimSpace(cred.Token) == ""
	})
	matchingCreds, err := arcaneregistry.MatchingForImage(imageRef, usableCreds, func(cred types.ContainerRegistryCredential) string { return cred.URL })
	if err != nil {
		return *new(T), nil, fmt.Errorf("%s failed: %w", operation, err)
	}
	matchingEntries, err := arcaneregistry.MatchingForImage(imageRef, entries, func(entry cachedEnabledRegistry) string { return entry.registry.URL })
	if err != nil {
		return *new(T), nil, fmt.Errorf("%s failed: %w", operation, err)
	}

	matchedCredentials := make([]resolvedRegistryCredential, 0, len(matchingCreds)+len(matchingEntries))
	coveredRegistries := make(map[string]bool, len(matchingCreds))
	for _, cred := range matchingCreds {
		coveredRegistries[arcaneregistry.NormalizeRegistryURL(cred.URL)] = true
		matchedCredentials = append(matchedCredentials, resolvedRegistryCredential{
			Username:      strings.TrimSpace(cred.Username),
			Token:         strings.TrimSpace(cred.Token),
			ServerAddress: normalizeRegistryServerAddress(cred.URL),
		})
	}
	for _, entry := range matchingEntries {
		// A caller credential covers the registry it names, so the stored credential of that registry is not consulted;
		// the registries the caller left uncovered still need theirs.
		if coveredRegistries[arcaneregistry.NormalizeRegistryURL(entry.registry.URL)] {
			continue
		}
		credential, resolveErr := s.resolveCachedCredential(ctx, entry)
		if resolveErr != nil {
			slog.WarnContext(ctx, "failed to resolve registry credential", "registry", entry.registry.URL, "error", resolveErr)
		} else if credential != nil {
			matchedCredentials = append(matchedCredentials, *credential)
		}
	}

	var lastErr error
	var lastResult *types.ContainerRegistryDigestResult
	for _, credential := range matchedCredentials {
		lastResult = &types.ContainerRegistryDigestResult{AuthMethod: "credential", AuthUsername: credential.Username, AuthRegistry: registryHost, UsedCredential: true}
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
		if kitregistry.Normalize(registryHost) == "docker.io" {
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
		return value, &types.ContainerRegistryDigestResult{AuthMethod: "anonymous", AuthRegistry: registryHost}, nil
	case lastErr != nil && browse.IsUnauthorizedRegistryError(err):
		return *new(T), lastResult, fmt.Errorf("%s failed: the saved credential for %s was rejected: %w",
			operation, registryHost, errors.Join(lastErr, fmt.Errorf("anonymous retry failed: %w", err)))
	case credErr != nil && browse.IsUnauthorizedRegistryError(err):
		return *new(T), &types.ContainerRegistryDigestResult{AuthMethod: "anonymous", AuthRegistry: registryHost},
			fmt.Errorf("%s: anonymous access unauthorized; credential lookup failed: %w", operation, errors.Join(err, fmt.Errorf("failed to load enabled registries: %w", credErr)))
	default:
		return *new(T), &types.ContainerRegistryDigestResult{AuthMethod: "anonymous", AuthRegistry: registryHost}, fmt.Errorf("%s failed: %w", operation, err)
	}
}

func (s *ContainerRegistryService) getDockerClient(ctx context.Context) (RegistryDaemonClient, error) {
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get docker client: %w", err)
	}
	return dockerClient, nil
}

func (s *ContainerRegistryService) credentialForRegistry(ctx context.Context, reg *ContainerRegistry) (*resolvedRegistryCredential, error) {
	if reg.RegistryType == RegistryTypeECR {
		credential, err := s.ecrCredentialForRegistry(ctx, reg)
		if err != nil {
			return nil, err
		}
		return credential, nil
	}

	return decryptGenericRegistryCredential(reg)
}

// ecrCredentialForRegistry resolves the ECR authorization token once per registry and reuses it while it stays valid,
// so a scan over many images asks AWS for a token once instead of once per image.
func (s *ContainerRegistryService) ecrCredentialForRegistry(ctx context.Context, reg *ContainerRegistry) (*resolvedRegistryCredential, error) {
	if reg.ID != "" {
		if credential, found, _ := s.ecrCredentials.Get(reg.ID); found {
			return &credential, nil
		}
	}

	ecrUser, ecrPass, err := s.GetOrRefreshECRToken(ctx, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to get ECR token: %w", err)
	}
	credential := resolvedRegistryCredential{
		Username:      ecrUser,
		Token:         ecrPass,
		ServerAddress: normalizeRegistryServerAddress(reg.URL),
	}
	if reg.ID != "" {
		s.ecrCredentials.Set(cache.Entry[string, resolvedRegistryCredential]{
			Key:   reg.ID,
			Value: credential,
			TTL:   remainingECRTokenLife(reg),
		})
	}
	return &credential, nil
}

// remainingECRTokenLife is how long the token held by reg stays usable, so the cached credential never outlives it.
func remainingECRTokenLife(reg *ContainerRegistry) time.Duration {
	if reg.ECRTokenGeneratedAt == nil {
		return ecrTokenTTL
	}
	remaining := ecrTokenTTL - time.Since(reg.ECRTokenGeneratedAt.UTC())
	if remaining <= 0 {
		return time.Second
	}
	return remaining
}

func decryptGenericRegistryCredential(reg *ContainerRegistry) (*resolvedRegistryCredential, error) {
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

func (s *ContainerRegistryService) SyncRegistries(ctx context.Context, syncItems []types.ContainerRegistrySync) error {
	if err := normalization.Normalize(&syncItems); err != nil {
		return err
	}
	defer s.invalidateRegistryCaches()
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
		credentialChanged, applyErr := update.ApplyEncrypted(&credential, kit.Ternary(registryType == RegistryTypeGeneric, item.Token, item.AWSSecretAccessKey))
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
		if slices.ContainsFunc(syncItems, func(item types.ContainerRegistrySync) bool { return item.ID == id }) {
			continue
		}
		// Already gone is the desired state for sync.
		if deleteErr := s.DeleteRegistry(ctx, id); deleteErr != nil && !errors.Is(deleteErr, common.ErrNotFound) {
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
		return "", common.Classify(common.ErrValidation, &types.FieldError{Field: "registryType", Err: errors.New("registry type must be one of: generic, ecr")})
	}
}

// normalizeRepositoryNames returns a non-nil slice so GORM always serializes a JSON array.
func normalizeRepositoryNames(raw []string) (database.StringSlice, error) {
	for _, name := range kit.Unique(kit.TrimNonEmpty(raw)) {
		// A repository name is only a path, so placeholder domain and tag segments make it a full reference.
		if _, err := reference.ParseNormalizedNamed("registry.invalid/" + name + "/placeholder:latest"); err != nil {
			return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "repositoryNames", Err: fmt.Errorf("invalid repository name %q", name)})
		}
	}
	return append(database.StringSlice{}, kit.Unique(kit.TrimNonEmpty(raw))...), nil
}

func normalizeRegistryServerAddress(registryURL string) string {
	return kit.Ternary(
		kitregistry.Normalize(registryURL) == "docker.io",
		arcaneregistry.NormalizeRegistryURL(registryURL),
		kitregistry.Normalize(registryURL),
	)
}

func IsRateLimitErrorString(msg string) bool {
	return slices.ContainsFunc([]string{"toomanyrequests", "rate limit", "too many requests", "status: 429", "status 429", "retry-after"}, func(indicator string) bool {
		return strings.Contains(strings.ToLower(msg), indicator)
	})
}

func (s *ContainerRegistryService) ListImageTags(ctx context.Context, imageRef string, externalCreds []types.ContainerRegistryCredential) (tagList []string, tagsErr error) {
	ctx, span := otel.Tracer(logging.InstrumentationName).Start(ctx, "registry.list_tags", trace.WithAttributes(attribute.String("arcane.image.ref", imageRef)))
	defer func() {
		span.SetAttributes(attribute.Int("arcane.result.count", len(tagList)))
		logging.EndSpan(span, tagsErr)
	}()
	if refs.IsDigestPinnedReference(imageRef) || refs.IsImageIDLikeReference(imageRef) {
		return nil, errors.New("cannot discover tags for an immutable image reference")
	}
	parts, err := refs.NormalizeReference(imageRef)
	if err != nil {
		return nil, err
	}
	lookupCtx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(s.settingsService.GetSettingsConfig().RegistryTagTimeout.AsInt(), timeouts.DefaultRegistryTags))
	defer cancel()

	tags, _, err := registryOperationWithCredentials(lookupCtx, s, parts.RegistryHost, parts.NormalizedRef, "registry tag listing of "+parts.NormalizedRef, externalCreds,
		func(ctx context.Context, credential *resolvedRegistryCredential) ([]string, error) {
			httpClient, transportErr := s.authorizedHTTPClient(ctx, parts.RegistryHost, parts.Repository, credential)
			if transportErr != nil {
				return nil, transportErr
			}
			return registry.FetchTags(ctx, registry.Request{
				Credential: func() *authn.AuthConfig {
					if credential == nil {
						return nil
					}
					return &authn.AuthConfig{Username: credential.Username, Password: credential.Token}
				}(),
				HTTPClient: httpClient,
				Host:       parts.RegistryHost,
				Repository: parts.Repository,
			})
		})
	return tags, err
}

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
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultRegistry)
		defer cancel()
		// Without a static access key, the default AWS credential chain applies (env, shared config, ECS task role, EC2 instance profile).
		secretKey, decErr := crypto.Decrypt(kit.Ternary(strings.TrimSpace(reg.AWSAccessKeyID) != "", reg.AWSSecretAccessKey, ""))
		if decErr != nil {
			return nil, fmt.Errorf("failed to decrypt AWS secret key for registry %s: %w", reg.URL, decErr)
		}
		if strings.TrimSpace(reg.AWSAccessKeyID) != "" && strings.TrimSpace(secretKey) == "" {
			return nil, fmt.Errorf("AWS secret access key is empty for registry %s", reg.URL)
		}

		cfg, cfgErr := config.LoadDefaultConfig(refreshCtx,
			config.WithRegion(reg.AWSRegion),
			kit.Ternary(strings.TrimSpace(reg.AWSAccessKeyID) != "",
				config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(reg.AWSAccessKeyID, strings.TrimSpace(secretKey), "")),
				func(*config.LoadOptions) error { return nil },
			),
		)
		if cfgErr != nil {
			return nil, fmt.Errorf("failed to load AWS config for registry %s: %w", reg.URL, cfgErr)
		}
		tokenOutput, ecrErr := ecr.NewFromConfig(cfg).GetAuthorizationToken(refreshCtx, &ecr.GetAuthorizationTokenInput{})
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
		s.invalidateRegistryCaches()
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

func (s *ContainerRegistryService) ImageConfig(ctx context.Context, imageRef string, opts ImageConfigOptions) (*v1.ConfigFile, error) {
	if strings.TrimSpace(imageRef) == "" {
		return nil, errors.New("empty image reference")
	}

	platform := v1.Platform{
		OS:           cmp.Or(opts.Platform.OS, "linux"),
		Architecture: cmp.Or(opts.Platform.Architecture, runtime.GOARCH),
		Variant:      opts.Platform.Variant,
	}
	cacheKey := strings.TrimSpace(imageRef) + "|" + platform.String()
	cfg, found, err := s.configCache.GetOrLoad(cacheKey, func(string) (*v1.ConfigFile, bool, error) {
		loadCtx, cancel := context.WithTimeout(ctx, timeouts.DefaultRegistry)
		defer cancel()

		parsedRef, parseErr := normalizeImageConfigReference(imageRef)
		if parseErr != nil {
			return nil, false, parseErr
		}
		img, _, fetchErr := registryOperationWithCredentials(loadCtx, s, parsedRef.Context().RegistryStr(), parsedRef.Name(), "image config fetch for "+strings.TrimSpace(imageRef), opts.Credentials,
			func(ctx context.Context, credential *resolvedRegistryCredential) (v1.Image, error) {
				tr, transportErr := s.authorizedTransport(ctx, parsedRef.Context(), credential)
				if transportErr != nil {
					return nil, transportErr
				}
				return remote.Image(parsedRef,
					remote.WithContext(ctx),
					remote.WithPlatform(platform),
					remote.WithTransport(tr),
					remote.WithAuth(func() authn.Authenticator {
						if credential == nil {
							return authn.Anonymous
						}
						return authn.FromConfig(authn.AuthConfig{Username: credential.Username, Password: credential.Token})
					}()),
				)
			})
		if fetchErr != nil {
			return nil, false, fetchErr
		}
		cfg, configErr := img.ConfigFile()
		if configErr != nil {
			return nil, false, fmt.Errorf("image config fetch failed for %s: %w", strings.TrimSpace(imageRef), configErr)
		}
		if cfg == nil {
			return nil, false, fmt.Errorf("image config fetch failed for %s: missing config", strings.TrimSpace(imageRef))
		}
		return cfg, true, nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("registry image config cache loader returned no config")
	}
	return cfg, nil
}

func ImagePlatform(ctx context.Context, dockerClient *mobyClient.Client, candidate updatertypes.TagCandidate) (ocispec.Platform, error) {
	inspected, inspectErr := dockerClient.ImageInspect(ctx, cmp.Or(candidate.ImageID, candidate.CurrentRef))
	if inspectErr == nil && inspected.Os != "" && inspected.Architecture != "" {
		return ocispec.Platform{
			OS:           inspected.Os,
			Architecture: inspected.Architecture,
			Variant:      inspected.Variant,
		}, nil
	}
	version, err := dockerClient.ServerVersion(ctx, mobyClient.ServerVersionOptions{})
	if err != nil {
		return ocispec.Platform{}, errors.Join(inspectErr, fmt.Errorf("resolve daemon platform: %w", err))
	}
	if version.Os == "" || version.Arch == "" {
		return ocispec.Platform{}, errors.New("image and daemon platform are unknown")
	}
	return ocispec.Platform{
		OS:           version.Os,
		Architecture: version.Arch,
	}, nil
}

func (s *ContainerRegistryService) HasImagePlatform(ctx context.Context, imageRef string, platform ocispec.Platform, externalCreds []types.ContainerRegistryCredential) (bool, error) {
	parsedRef, err := normalizeImageConfigReference(imageRef)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(s.settingsService.GetSettingsConfig().RegistryTimeout.AsInt(), timeouts.DefaultRegistry))
	defer cancel()
	matcher := platforms.Only(platform)
	matched, _, err := registryOperationWithCredentials(ctx, s, parsedRef.Context().RegistryStr(), parsedRef.Name(), "manifest fetch for "+strings.TrimSpace(imageRef), externalCreds,
		func(ctx context.Context, credential *resolvedRegistryCredential) (bool, error) {
			tr, transportErr := s.authorizedTransport(ctx, parsedRef.Context(), credential)
			if transportErr != nil {
				return false, transportErr
			}
			desc, getErr := remote.Get(parsedRef, remote.WithContext(ctx), remote.WithTransport(tr))
			if getErr != nil {
				return false, getErr
			}
			if !desc.MediaType.IsIndex() {
				img, imageErr := desc.Image()
				if imageErr != nil {
					return false, imageErr
				}
				cfg, configErr := img.ConfigFile()
				if configErr != nil {
					return false, configErr
				}
				return cfg.Architecture == "" || matcher.Match(ocispec.Platform{
					OS:           cfg.OS,
					Architecture: cfg.Architecture,
					OSVersion:    cfg.OSVersion,
					Variant:      cfg.Variant,
				}), nil
			}
			index, parseErr := v1.ParseIndexManifest(bytes.NewReader(desc.Manifest))
			if parseErr != nil {
				return false, fmt.Errorf("parse image index for %s: %w", strings.TrimSpace(imageRef), parseErr)
			}
			return slices.ContainsFunc(index.Manifests, func(child v1.Descriptor) bool {
				return child.Platform != nil && matcher.Match(ocispec.Platform{
					OS:           child.Platform.OS,
					Architecture: child.Platform.Architecture,
					OSVersion:    child.Platform.OSVersion,
					Variant:      child.Platform.Variant,
				})
			}), nil
		})
	return matched, err
}
