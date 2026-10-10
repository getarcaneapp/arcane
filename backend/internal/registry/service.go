package registry

import (
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/cenkalti/backoff/v5"
	"github.com/distribution/reference"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
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
)

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

type registryRateLimitCacheEntryInternal struct {
	RateLimit registry.RateLimitInfo `json:"rateLimit"`
	CheckedAt time.Time              `json:"checkedAt"`
}

type rateLimitRoundTripFuncInternal func(*http.Request) (*http.Response, error)

func (f rateLimitRoundTripFuncInternal) RoundTrip(req *http.Request) (*http.Response, error) {
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
	distributionHTTPClient := &http.Client{Timeout: 30 * time.Second}
	if len(distributionHTTPClients) > 0 && distributionHTTPClients[0] != nil {
		distributionHTTPClient = distributionHTTPClients[0]
	}
	service := &ContainerRegistryService{
		db:                     db,
		dockerClient:           dockerClient,
		distributionHTTPClient: distributionHTTPClient,
		kvService:              kvService,
		settingsService:        settingsService,
	}
	service.browse = browse.NewService(service.browseRegistryInternal, service.tagLookupContextInternal, distributionHTTPClient.Transport)
	backgroundLoader := func(imageRefs []string) (map[string]string, error) {
		digests := make(map[string]string, len(imageRefs))
		var firstErr error
		for _, imageRef := range imageRefs {
			// Per-ref timeout: a single deadline shared across the whole
			// sequential batch left later refs with whatever the earlier ones
			// had not already spent, so one slow registry starved the tail.
			ctx, cancel := context.WithTimeout(context.Background(), timeouts.DefaultRegistry) //nolint:forbidigo // Cache revalidation runs independently of request cancellation.
			result, err := service.InspectImageDigest(ctx, imageRef, nil)
			cancel()
			if err != nil {
				slog.DebugContext(ctx, "registry revalidation failed for image", "imageRef", imageRef, "error", err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			digests[imageRef] = result.Digest
		}

		// Returning an error alongside results makes hot discard the whole map,
		// so report success whenever anything resolved: one unreachable registry
		// used to throw away every digest already fetched, forcing the entire
		// batch to be refetched on the next tick. Refs that failed are simply
		// left uncached (this cache has no missing-key cache) and retried on
		// next access; only a fully failed batch surfaces the error, which
		// KeepOnError turns into "retain the previous digests".
		if len(digests) > 0 {
			return digests, nil
		}
		return nil, firstErr
	}
	service.cache = hot.NewHotCache[string, string](hot.LRU, 4096).
		WithTTL(registryCacheTTL).
		WithRevalidation(registryCacheTTL, backgroundLoader).
		WithRevalidationErrorPolicy(hot.KeepOnError).
		WithJanitor().
		Build()
	// No static loader: ImageVersionLabel uses GetWithLoaders so failed
	// resolutions are not cached and retry on the next request.
	service.labelCache = hot.NewHotCache[string, string](hot.LRU, 64).
		WithTTL(versionLabelCacheTTL).
		Build()
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
	q := s.db.WithContext(ctx).Model(&ContainerRegistry{})

	q = pagination.ApplyLikeSearch(q, params.Search, "url LIKE ? OR username LIKE ? OR COALESCE(description, '') LIKE ?")

	q = pagination.ApplyBooleanFilter(q, "enabled", params.Filters["enabled"])
	q = pagination.ApplyBooleanFilter(q, "insecure", params.Filters["insecure"])

	out, paginationResp, err := params.PaginateSortAndMapDB[ContainerRegistry, containerregistry.ContainerRegistry](q, &registries)
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
	repositoryNames, err := normalizeRepositoryNamesInternal(req.RepositoryNames)
	if err != nil {
		return nil, err
	}

	registryRecord := &ContainerRegistry{
		URL:             req.URL,
		Description:     req.Description,
		Insecure:        req.Insecure != nil && *req.Insecure,
		Enabled:         req.Enabled == nil || *req.Enabled,
		RegistryType:    registryType,
		RepositoryNames: repositoryNames,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if registryType == RegistryTypeECR {
		if endpointErr := validateECRRegistryEndpoint(registryRecord.URL, registryRecord.Insecure); endpointErr != nil {
			return nil, endpointErr
		}
		if strings.TrimSpace(req.AWSRegion) == "" {
			return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "awsRegion", Err: errors.New("AWS Region is required")})
		}
		// Static keys are optional, but must be supplied as a pair. With neither,
		// the default AWS credential chain (e.g. an EC2 instance profile) is used.
		hasKeyID := strings.TrimSpace(req.AWSAccessKeyID) != ""
		hasSecret := strings.TrimSpace(req.AWSSecretAccessKey) != ""
		if hasSecret && !hasKeyID {
			return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "awsAccessKeyId", Err: errors.New("AWS Access Key ID is required")})
		}
		if hasKeyID && !hasSecret {
			return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "awsSecretAccessKey", Err: errors.New("AWS Secret Access Key is required")})
		}
		if hasSecret {
			encryptedSecret, encryptErr := crypto.Encrypt(req.AWSSecretAccessKey)
			if encryptErr != nil {
				return nil, fmt.Errorf("failed to encrypt AWS secret access key: %w", encryptErr)
			}
			registryRecord.AWSSecretAccessKey = encryptedSecret
		}
		registryRecord.AWSAccessKeyID = req.AWSAccessKeyID
		registryRecord.AWSRegion = req.AWSRegion
	} else {
		if strings.TrimSpace(req.Username) == "" {
			return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "username", Err: errors.New("username is required")})
		}
		if strings.TrimSpace(req.Token) == "" {
			return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "token", Err: errors.New("token is required")})
		}
		encryptedToken, encryptTokenErr := crypto.Encrypt(req.Token)
		if encryptTokenErr != nil {
			return nil, fmt.Errorf("failed to encrypt token: %w", encryptTokenErr)
		}
		registryRecord.Username = req.Username
		registryRecord.Token = encryptedToken
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

	if applyRegistryTypeUpdateErr := s.applyRegistryTypeUpdateInternal(registryRecord, req.RegistryType); applyRegistryTypeUpdateErr != nil {
		return nil, applyRegistryTypeUpdateErr
	}

	storedCredentials := map[string]bool{"token": registryRecord.Token != ""}
	updatedCredentials := map[string]bool{"token": req.Token != nil && *req.Token != ""}
	if registryRecord.RegistryType == RegistryTypeECR {
		storedCredentials = map[string]bool{
			"awsAccessKeyId":     registryRecord.AWSAccessKeyID != "",
			"awsSecretAccessKey": registryRecord.AWSSecretAccessKey != "",
		}
		updatedCredentials = map[string]bool{
			"awsAccessKeyId":     req.AWSAccessKeyID != nil && *req.AWSAccessKeyID != "",
			"awsSecretAccessKey": req.AWSSecretAccessKey != nil && *req.AWSSecretAccessKey != "",
		}
	}
	if validateCredentialTargetChangeErr := validation.ValidateCredentialTargetChange(
		"registry URL",
		registryRecord.URL,
		req.URL,
		normalizeRegistryServerAddressInternal,
		storedCredentials,
		updatedCredentials,
	); validateCredentialTargetChangeErr != nil {
		return nil, validateCredentialTargetChangeErr
	}

	// Update common fields
	utils.ApplyChanged(&registryRecord.URL, mo.PointerToOption(req.URL))
	utils.ApplyNullable(&registryRecord.Description, mo.PointerToOption(req.Description))
	utils.ApplyChanged(&registryRecord.Insecure, mo.PointerToOption(req.Insecure))
	utils.ApplyChanged(&registryRecord.Enabled, mo.PointerToOption(req.Enabled))

	// RepositoryNames: nil pointer means "don't touch"; empty slice means "clear".
	if req.RepositoryNames != nil {
		repositoryNames, normalizeRepositoryNamesErr := normalizeRepositoryNamesInternal(*req.RepositoryNames)
		if normalizeRepositoryNamesErr != nil {
			return nil, normalizeRepositoryNamesErr
		}
		registryRecord.RepositoryNames = repositoryNames
	}

	if registryRecord.RegistryType == RegistryTypeECR {
		if updateECRRegistryFieldsErr := s.updateECRRegistryFieldsInternal(registryRecord, req); updateECRRegistryFieldsErr != nil {
			return nil, updateECRRegistryFieldsErr
		}
	} else if updateGenericRegistryFieldsErr := s.updateGenericRegistryFieldsInternal(registryRecord, req); updateGenericRegistryFieldsErr != nil {
		return nil, updateGenericRegistryFieldsErr
	}

	registryRecord.UpdatedAt = time.Now()
	if updateRegistryErr := s.db.WithContext(ctx).Save(registryRecord).Error; updateRegistryErr != nil {
		return nil, fmt.Errorf("failed to update registry: %w", updateRegistryErr)
	}
	return registryRecord,
		nil
}

func (s *ContainerRegistryService) applyRegistryTypeUpdateInternal(localRegistry *ContainerRegistry, registryType *string) error {
	if registryType == nil {
		return nil
	}

	nextType, err := NormalizeRegistryType(*registryType)
	if err != nil {
		return err
	}

	if nextType != localRegistry.RegistryType {
		return common.Classify(
			common.ErrValidation,
			&base.FieldError{
				Field: "registryType",
				Err: errors.New(
					"registry type cannot be changed after creation",
				),
			},
		)
	}

	return nil
}

func (s *ContainerRegistryService) updateECRRegistryFieldsInternal(localRegistry *ContainerRegistry, req containerregistry.UpdateContainerRegistryRequest) error {
	if err := validateECRRegistryEndpoint(localRegistry.URL, localRegistry.Insecure); err != nil {
		return err
	}
	utils.ApplyChanged(&localRegistry.AWSAccessKeyID, mo.PointerToOption(req.AWSAccessKeyID))
	utils.ApplyChanged(&localRegistry.AWSRegion, mo.PointerToOption(req.AWSRegion))

	hasKeyID := strings.TrimSpace(localRegistry.AWSAccessKeyID) != ""
	providedSecret := req.AWSSecretAccessKey != nil && *req.AWSSecretAccessKey != ""

	switch {
	case !hasKeyID && providedSecret:
		return common.Classify(common.ErrValidation, &base.FieldError{Field: "awsAccessKeyId", Err: errors.New("AWS Access Key ID is required")})
	case !hasKeyID:
		// No static key: the default AWS credential chain applies, so drop any stored secret.
		localRegistry.AWSSecretAccessKey = ""
	case providedSecret:
		encryptedSecret, err := crypto.Encrypt(*req.AWSSecretAccessKey)
		if err != nil {
			return fmt.Errorf("failed to encrypt AWS secret access key: %w", err)
		}
		utils.ApplyChanged(&localRegistry.AWSSecretAccessKey, mo.Some(encryptedSecret))
	}

	if strings.TrimSpace(localRegistry.AWSRegion) == "" {
		return common.Classify(common.ErrValidation, &base.FieldError{Field: "awsRegion", Err: errors.New("AWS Region is required")})
	}
	if hasKeyID && strings.TrimSpace(localRegistry.AWSSecretAccessKey) == "" {
		return common.Classify(common.ErrValidation, &base.FieldError{Field: "awsSecretAccessKey", Err: errors.New("AWS Secret Access Key is required")})
	}

	if req.AWSAccessKeyID != nil || req.AWSSecretAccessKey != nil || req.AWSRegion != nil {
		localRegistry.ECRToken = ""
		localRegistry.ECRTokenGeneratedAt = nil
	}

	return nil
}

func (s *ContainerRegistryService) updateGenericRegistryFieldsInternal(localRegistry *ContainerRegistry, req containerregistry.UpdateContainerRegistryRequest) error {
	utils.ApplyChanged(&localRegistry.Username, mo.PointerToOption(req.Username))

	if req.Token != nil && *req.Token != "" {
		encryptedToken, err := crypto.Encrypt(*req.Token)
		if err != nil {
			return fmt.Errorf("failed to encrypt token: %w", err)
		}
		utils.ApplyChanged(&localRegistry.Token, mo.Some(encryptedToken))
	}

	if strings.TrimSpace(localRegistry.Username) == "" {
		return common.Classify(common.ErrValidation, &base.FieldError{Field: "username", Err: errors.New("username is required")})
	}

	return nil
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
//
// The registry-auth methods tolerate a nil receiver: callers such as build.BuildService may
// hold no registry service, and are wired in as a buildtypes.RegistryAuthProvider where a
// typed-nil pointer would otherwise satisfy the interface's nil checks and panic on use.
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
	if s == nil {
		return "", nil
	}
	normalizedRegistryHost := registryauth.NormalizeRegistryForComparison(registryHost)
	if normalizedRegistryHost == "" {
		return "", nil
	}

	authConfigs, err := s.GetAllRegistryAuthConfigs(ctx)
	if err != nil {
		return "", err
	}
	if len(authConfigs) == 0 {
		return "", nil
	}

	cfg, ok := authConfigs[normalizedRegistryHost]
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
		reg := &registries[i]
		if !reg.Enabled {
			continue
		}

		normalizedHost := strings.TrimSpace(registryauth.NormalizeRegistryForComparison(reg.URL))
		if normalizedHost == "" {
			continue
		}

		serverAddress := normalizedHost
		if normalizedHost == "docker.io" {
			serverAddress = registryauth.NormalizeRegistryURL(reg.URL)
		}
		if serverAddress == "" {
			continue
		}

		var username, token string

		if reg.RegistryType == "ecr" {
			ecrUser, ecrPass, ecrErr := s.GetOrRefreshECRToken(ctx, reg)
			if ecrErr != nil {
				slog.WarnContext(ctx, "failed to get ECR token for auth configs", "registry", reg.URL, "error", ecrErr)
				continue
			}
			username = ecrUser
			token = ecrPass
		} else {
			username = strings.TrimSpace(reg.Username)
			if username == "" || reg.Token == "" {
				continue
			}
			decryptedToken, decryptErr := crypto.Decrypt(reg.Token)
			if decryptErr != nil {
				slog.WarnContext(ctx, "failed to decrypt token for registry, skipping", "registry", reg.URL, "error", decryptErr)
				continue
			}
			token = strings.TrimSpace(decryptedToken)
			if token == "" {
				continue
			}
		}

		authConfig := dockerregistry.AuthConfig{
			Username:      username,
			Password:      token,
			ServerAddress: serverAddress,
		}
		for _, key := range registryauth.LookupKeys(normalizedHost) {
			authConfigs[key] = authConfig
		}
	}

	return kit.Ternary(len(authConfigs) == 0, nil, authConfigs), nil
}

// RecordImagePull increments Arcane's observed successful pull counter for an image registry.
func (s *ContainerRegistryService) RecordImagePull(ctx context.Context, imageRef string) error {
	if s.kvService == nil {
		return nil
	}

	registryHost, err := normalizePullRegistryHostInternal(imageRef)
	if err != nil {
		return err
	}
	if registryHost == "" {
		return nil
	}

	if _, incrementInt64Err := s.kvService.IncrementInt64(ctx, registryPullCountKeyInternal(registryHost), 1); incrementInt64Err != nil {
		return incrementInt64Err
	}

	return nil
}

// GetRegistryPullUsage returns pull usage visibility for configured registries.
func (s *ContainerRegistryService) GetRegistryPullUsage(ctx context.Context) (containerregistry.PullUsageResponse, error) {
	registries, err := s.GetAllRegistries(ctx)
	if err != nil {
		return containerregistry.PullUsageResponse{}, err
	}

	results := make([]containerregistry.PullUsage, 0, len(registries))
	for i := range registries {
		results = append(results, s.buildRegistryPullUsageInternal(ctx, registries[i]))
	}

	return containerregistry.PullUsageResponse{Registries: results}, nil
}

func (s *ContainerRegistryService) buildRegistryPullUsageInternal(ctx context.Context, reg ContainerRegistry) containerregistry.PullUsage {
	registryHost := registryauth.NormalizeRegistryForComparison(reg.URL)
	usage := containerregistry.PullUsage{
		RegistryID:    reg.ID,
		Provider:      registryProviderInternal(registryHost, reg.RegistryType),
		Registry:      registryHost,
		DisplayName:   registryDisplayNameInternal(registryHost, reg.RegistryType),
		ObservedPulls: s.getObservedPullsInternal(ctx, registryHost),
		AuthMethod:    "unknown",
		CheckedAt:     time.Now().UTC(),
	}

	if registryHost != "docker.io" || !reg.Enabled {
		return usage
	}

	credential, authMethod, authUsername, err := s.dockerHubCredentialForRegistryInternal(reg)
	usage.AuthMethod = authMethod
	usage.AuthUsername = authUsername
	usage.Repository = "ratelimitpreview/test"
	if err != nil {
		usage.Error = err.Error()
		return usage
	}

	if cachedRateLimit, checkedAt, ok := s.getCachedRateLimitInternal(ctx, reg.ID); ok {
		ensureRateLimitUsedInternal(cachedRateLimit)
		usage.Limit = cachedRateLimit.Limit
		usage.Remaining = cachedRateLimit.Remaining
		usage.Used = cachedRateLimit.Used
		usage.WindowSeconds = cachedRateLimit.WindowSeconds
		usage.Source = cachedRateLimit.Source
		usage.CheckedAt = checkedAt
		return usage
	}

	rateLimit, err := s.fetchDockerHubRateLimitInternal(ctx, credential)
	usage.CheckedAt = time.Now().UTC()
	if err != nil {
		usage.Error = err.Error()
		return usage
	}
	ensureRateLimitUsedInternal(rateLimit)

	usage.Limit = rateLimit.Limit
	usage.Remaining = rateLimit.Remaining
	usage.Used = rateLimit.Used
	usage.WindowSeconds = rateLimit.WindowSeconds
	usage.Source = rateLimit.Source
	s.setCachedRateLimitInternal(ctx, reg.ID, rateLimit, usage.CheckedAt)

	return usage
}

func ensureRateLimitUsedInternal(rateLimit *registry.RateLimitInfo) {
	if rateLimit == nil || rateLimit.Used != nil || rateLimit.Limit == nil || rateLimit.Remaining == nil {
		return
	}
	rateLimit.Used = new(max(*rateLimit.Limit-*rateLimit.Remaining, 0))
}

func (s *ContainerRegistryService) getObservedPullsInternal(ctx context.Context, registryHost string) int64 {
	if s.kvService == nil || registryHost == "" {
		return 0
	}

	value, err := s.kvService.GetInt64(ctx, registryPullCountKeyInternal(registryHost), 0)
	if err != nil {
		slog.WarnContext(ctx, "failed to read registry pull count", "registry", registryHost, "error", err)
		return 0
	}

	return value
}

func (s *ContainerRegistryService) dockerHubCredentialForRegistryInternal(reg ContainerRegistry) (*authn.AuthConfig, string, string, error) {
	if reg.RegistryType != RegistryTypeGeneric {
		return nil, "anonymous", "", nil
	}

	username := strings.TrimSpace(reg.Username)
	if username == "" || strings.TrimSpace(reg.Token) == "" {
		return nil, "anonymous", "", nil
	}

	token, err := crypto.Decrypt(reg.Token)
	if err != nil {
		return nil, "credential", username, fmt.Errorf("failed to decrypt Docker Hub credential: %w", err)
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return nil, "anonymous", "", nil
	}

	return &authn.AuthConfig{
		Username: username,
		Password: token,
	}, "credential", username, nil
}

func (s *ContainerRegistryService) fetchDockerHubRateLimitInternal(ctx context.Context, credential *authn.AuthConfig) (*registry.RateLimitInfo, error) {
	return registry.FetchRegistryRateLimit(ctx, "docker.io", dockerHubRateLimitRepository, dockerHubRateLimitTag, credential, dockerHubRateLimitHTTPClientInternal(s.distributionHTTPClient))
}

func dockerHubRateLimitHTTPClientInternal(httpClient *http.Client) *http.Client {
	if httpClient == nil {
		return nil
	}

	cloned := *httpClient
	baseTransport := cloned.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}

	cloned.Transport = rateLimitRoundTripFuncInternal(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet &&
			req.URL.Host == "registry-1.docker.io" &&
			req.URL.Path == "/v2/"+dockerHubRateLimitRepository+"/manifests/"+dockerHubRateLimitTag {
			rewritten := req.Clone(req.Context())
			rewritten.Method = http.MethodHead
			req = rewritten
		}
		return baseTransport.RoundTrip(req)
	})

	return &cloned
}

func (s *ContainerRegistryService) getCachedRateLimitInternal(ctx context.Context, registryID string) (*registry.RateLimitInfo, time.Time, bool) {
	if s.kvService == nil || registryID == "" {
		return nil, time.Time{}, false
	}

	raw, ok, err := s.kvService.Get(ctx, registryRateLimitKeyInternal(registryID))
	if err != nil {
		slog.WarnContext(ctx, "failed to read registry rate limit cache", "registryId", registryID, "error", err)
		return nil, time.Time{}, false
	}
	if !ok {
		return nil, time.Time{}, false
	}

	var entry registryRateLimitCacheEntryInternal
	if unmarshalErr := json.Unmarshal([]byte(raw), &entry); unmarshalErr != nil {
		slog.WarnContext(ctx, "failed to parse registry rate limit cache", "registryId", registryID, "error", unmarshalErr)
		return nil, time.Time{}, false
	}
	if time.Since(entry.CheckedAt) > registryCacheTTL {
		return nil, time.Time{}, false
	}

	return &entry.RateLimit, entry.CheckedAt, true
}

func (s *ContainerRegistryService) setCachedRateLimitInternal(ctx context.Context, registryID string, rateLimit *registry.RateLimitInfo, checkedAt time.Time) {
	if s.kvService == nil || registryID == "" || rateLimit == nil {
		return
	}

	payload, err := json.Marshal(registryRateLimitCacheEntryInternal{
		RateLimit: *rateLimit,
		CheckedAt: checkedAt,
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to encode registry rate limit cache", "registryId", registryID, "error", err)
		return
	}

	if setErr := s.kvService.Set(ctx, registryRateLimitKeyInternal(registryID), string(payload)); setErr != nil {
		slog.WarnContext(ctx, "failed to save registry rate limit cache", "registryId", registryID, "error", setErr)
	}
}

func normalizePullRegistryHostInternal(imageRef string) (string, error) {
	registryHost, err := registryauth.GetRegistryAddress(imageRef)
	if err != nil {
		return "", fmt.Errorf("parse image registry for %q: %w", imageRef, err)
	}

	return registryauth.NormalizeRegistryForComparison(registryHost), nil
}

func registryPullCountKeyInternal(registryHost string) string {
	return registryPullCountKeyPrefix + registryauth.NormalizeRegistryForComparison(registryHost)
}

func registryRateLimitKeyInternal(registryID string) string {
	return registryRateLimitKeyPrefix + strings.TrimSpace(registryID)
}

func registryProviderInternal(registryHost, registryType string) string {
	if registryType == RegistryTypeECR {
		return "ecr"
	}
	return kit.Ternary(registryHost == "docker.io", "dockerhub", "generic")
}

func registryDisplayNameInternal(registryHost, registryType string) string {
	if registryType == RegistryTypeECR {
		return "Amazon ECR"
	}
	switch {
	case registryHost == "" || registryHost == "docker.io":
		return "Docker Hub"
	case strings.Contains(registryHost, "ghcr.io"):
		return "GitHub Container Registry"
	case strings.Contains(registryHost, "gcr.io"):
		return "Google Container Registry"
	case strings.Contains(registryHost, "quay.io"):
		return "Quay.io Registry"
	default:
		return registryHost
	}
}

func (s *ContainerRegistryService) TestRegistry(ctx context.Context, registryURL, username, token string) error {
	if strings.TrimSpace(username) == "" && strings.TrimSpace(token) == "" {
		// No credentials configured — skip the credential test.
		return nil
	}

	dockerClient, err := s.getDockerClientInternal(ctx)
	if err != nil {
		return err
	}

	_, err = dockerClient.RegistryLogin(ctx, client.RegistryLoginOptions{
		Username:      strings.TrimSpace(username),
		Password:      strings.TrimSpace(token),
		ServerAddress: normalizeRegistryServerAddressInternal(registryURL),
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

	dockerClient, err := s.getDockerClientInternal(ctx)
	if err != nil {
		return err
	}

	_, err = dockerClient.RegistryLogin(ctx, client.RegistryLoginOptions{
		Username:      ecrUser,
		Password:      ecrPass,
		ServerAddress: normalizeRegistryServerAddressInternal(reg.URL),
	})
	if err != nil {
		return fmt.Errorf("ECR registry login failed: %w", err)
	}

	return nil
}

// ImageDigest fetches the current digest for an image:tag from the registry
// This is used for digest-based update detection for non-semver tags
func (s *ContainerRegistryService) ImageDigest(ctx context.Context, imageRef string) (string, error) {
	normalizedRef, _, err := normalizeImageReferenceForDistributionInternal(imageRef)
	if err != nil {
		return "", err
	}

	digestValue, found, err := s.cache.GetWithLoaders(normalizedRef, func(_ []string) (map[string]string, error) {
		loadCtx, cancel := context.WithTimeout(ctx, timeouts.DefaultRegistry)
		defer cancel()

		result, loadErr := s.InspectImageDigest(loadCtx, normalizedRef, nil)
		if loadErr != nil {
			return nil, loadErr
		}
		return map[string]string{normalizedRef: result.Digest}, nil
	})
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("registry digest cache loader returned no digest")
	}
	return digestValue, nil
}

func (s *ContainerRegistryService) InspectImageDigest(ctx context.Context, imageRef string, externalCreds []containerregistry.Credential) (*containerregistry.DigestResult, error) {
	parts, err := refs.NormalizeReference(imageRef)
	if err != nil {
		return nil, err
	}

	var lastResult *containerregistry.DigestResult
	var lastErr error
	fallbackWarningLogged := false

	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 500 * time.Millisecond
	bo.MaxInterval = 10 * time.Second
	bo.RandomizationFactor = 0.3

	_, retryErr := backoff.Retry(ctx, func() (*containerregistry.DigestResult, error) {
		result, inspectImageDigestViaDaemonErr := s.inspectImageDigestViaDaemonInternal(ctx, parts.NormalizedRef, parts.RegistryHost, externalCreds)
		if inspectImageDigestViaDaemonErr == nil {
			lastResult = result
			return result, nil
		}

		if isRateLimitErrorInternal(inspectImageDigestViaDaemonErr) {
			lastResult = result
			lastErr = inspectImageDigestViaDaemonErr
			slog.DebugContext(ctx, "rate limited by registry, will retry",
				"registry", parts.RegistryHost,
				"imageRef", parts.NormalizedRef)
			return nil, inspectImageDigestViaDaemonErr
		}

		if !isDistributionFallbackEligibleInternal(inspectImageDigestViaDaemonErr) {
			lastResult = result
			lastErr = inspectImageDigestViaDaemonErr
			return nil, backoff.Permanent(inspectImageDigestViaDaemonErr)
		}

		if !fallbackWarningLogged {
			fallbackWarningLogged = true
			slog.WarnContext(ctx, "distribution inspect unavailable, falling back to direct registry digest lookup",
				"imageRef", parts.NormalizedRef,
				"registry", parts.RegistryHost,
				"error", inspectImageDigestViaDaemonErr.Error())
		}

		fallbackResult, fallbackErr := s.inspectImageDigestViaRegistryInternal(ctx, parts.RegistryHost, parts.Repository, parts.Tag, externalCreds)
		if fallbackErr == nil {
			lastResult = fallbackResult
			return fallbackResult, nil
		}

		if isRateLimitErrorInternal(fallbackErr) {
			lastResult = fallbackResult
			lastErr = fallbackErr
			slog.DebugContext(ctx, "rate limited by registry on fallback, will retry",
				"registry", parts.RegistryHost,
				"imageRef", parts.NormalizedRef)
			return nil, fallbackErr
		}

		lastResult = fallbackResult
		lastErr = fmt.Errorf("daemon digest lookup failed; registry fallback failed: %w", errors.Join(inspectImageDigestViaDaemonErr, fallbackErr))
		return nil, backoff.Permanent(lastErr)
	}, backoff.WithBackOff(bo), backoff.WithMaxTries(5))

	if retryErr != nil {
		if errors.Is(retryErr, context.Canceled) || errors.Is(retryErr, context.DeadlineExceeded) {
			return lastResult, retryErr
		}
		return lastResult, lastErr
	}

	return lastResult, nil
}

func (
	s *ContainerRegistryService,
) inspectImageDigestViaDaemonInternal(
	ctx context.Context,
	normalizedRef, registryHost string,
	externalCreds []containerregistry.Credential,
) (
	*containerregistry.DigestResult,
	error,
) {
	dockerClient, err := s.getDockerClientInternal(ctx)
	if err != nil {
		return nil, err
	}

	value, result, err := registryOperationWithCredentialsInternal(ctx, s, registryHost, "distribution inspect of "+normalizedRef, externalCreds,
		func(ctx context.Context, credential *resolvedRegistryCredential) (string, error) {
			return s.fetchDigestFromDaemonInternal(ctx, dockerClient, registryHost, normalizedRef, credential)
		})
	if result != nil {
		result.Digest = value
	}
	return result, err
}

func (
	s *ContainerRegistryService,
) inspectImageDigestViaRegistryInternal(
	ctx context.Context,
	registryHost, repository, tag string,
	externalCreds []containerregistry.Credential,
) (
	*containerregistry.DigestResult,
	error,
) {
	value, result, err := registryOperationWithCredentialsInternal(ctx, s, registryHost, "registry manifest inspect of "+registryHost+"/"+repository+":"+tag, externalCreds,
		func(ctx context.Context, credential *resolvedRegistryCredential) (string, error) {
			return s.fetchDigestFromRegistryInternal(ctx, registryHost, repository, tag, credential)
		})
	if result != nil {
		result.Digest = value
	}
	return result, err
}

// Stored credentials go first: anonymous requests share a per-registry quota with every other unauthenticated client.
func registryOperationWithCredentialsInternal[
	T any,
](
	ctx context.Context,
	s *ContainerRegistryService,
	registryHost, operation string,
	externalCreds []containerregistry.Credential,
	fetch func(
		context.Context,
		*resolvedRegistryCredential,
	) (
		T,
		error,
	),
) (
	T,
	*containerregistry.DigestResult,
	error,
) {
	var zero T
	matchedCredentials, credErr := s.getMatchingRegistryCredentialsInternal(ctx, registryHost, externalCreds)

	var lastErr error
	var lastResult *containerregistry.DigestResult
	for _, credential := range matchedCredentials {
		lastResult = &containerregistry.DigestResult{AuthMethod: "credential", AuthUsername: credential.Username, AuthRegistry: registryHost, UsedCredential: true}
		value, err := fetch(ctx, &credential)
		if err == nil {
			return value, lastResult, nil
		}
		lastErr = err
		if !browse.IsUnauthorizedRegistryError(err) {
			return zero, lastResult, fmt.Errorf("%s failed with credentials: %w", operation, err)
		}
	}
	if lastErr != nil {
		// Docker Hub anonymous quotas are too small to be worth a retry; elsewhere a stale credential must not break public images.
		if isDockerHubRegistryInternal(registryHost) {
			return zero, lastResult, fmt.Errorf("%s failed: %w", operation, lastErr)
		}
		slog.DebugContext(ctx, "credentialed registry lookup failed, retrying anonymously",
			"registry", registryHost,
			"operation", operation,
			"error", lastErr.Error())
	}

	result := &containerregistry.DigestResult{AuthMethod: "anonymous", AuthRegistry: registryHost}
	value, err := fetch(ctx, nil)
	if err == nil {
		return value, result, nil
	}
	if credErr != nil && browse.IsUnauthorizedRegistryError(err) {
		return zero, result, fmt.Errorf("%s: anonymous access unauthorized; credential lookup failed: %w", operation, errors.Join(err, credErr))
	}
	return zero, result, fmt.Errorf("%s failed: %w", operation, err)
}

func (s *ContainerRegistryService) getDockerClientInternal(ctx context.Context) (RegistryDaemonClient, error) {
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

func (
	s *ContainerRegistryService,
) getMatchingRegistryCredentialsInternal(
	ctx context.Context,
	registryHost string,
	externalCreds []containerregistry.Credential,
) (
	[]resolvedRegistryCredential,
	error,
) {
	if len(externalCreds) > 0 {
		resolvedCredentials := make([]resolvedRegistryCredential, 0, len(externalCreds))
		for _, cred := range externalCreds {
			if !cred.Enabled || strings.TrimSpace(cred.Username) == "" || strings.TrimSpace(cred.Token) == "" {
				continue
			}
			if !registryauth.IsRegistryMatch(cred.URL, registryHost) {
				continue
			}

			resolvedCredentials = append(resolvedCredentials, resolvedRegistryCredential{
				Username:      strings.TrimSpace(cred.Username),
				Token:         strings.TrimSpace(cred.Token),
				ServerAddress: normalizeRegistryServerAddressInternal(cred.URL),
			})
		}
		return resolvedCredentials, nil
	}

	if s == nil || s.db == nil {
		return nil, nil
	}

	registries, err := s.GetEnabledRegistries(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load enabled registries: %w", err)
	}

	creds := make([]resolvedRegistryCredential, 0, len(registries))
	for i := range registries {
		reg := &registries[i]
		if !registryauth.IsRegistryMatch(reg.URL, registryHost) {
			continue
		}

		credential, credErr := s.credentialForRegistryInternal(ctx, reg)
		if credErr != nil {
			slog.WarnContext(ctx, "failed to resolve registry credential", "registry", reg.URL, "error", credErr)
			continue
		}
		if credential != nil {
			creds = append(creds, *credential)
		}
	}

	return creds, nil
}

// credentialForRegistryInternal resolves the stored credential of one registry.
// It returns nil when the registry is configured for anonymous access.
func (s *ContainerRegistryService) credentialForRegistryInternal(ctx context.Context, reg *ContainerRegistry) (*resolvedRegistryCredential, error) {
	if reg.RegistryType == RegistryTypeECR {
		ecrUser, ecrPass, err := s.GetOrRefreshECRToken(ctx, reg)
		if err != nil {
			return nil, fmt.Errorf("failed to get ECR token: %w", err)
		}
		return &resolvedRegistryCredential{
			Username:      ecrUser,
			Token:         ecrPass,
			ServerAddress: normalizeRegistryServerAddressInternal(reg.URL),
		}, nil
	}

	username := strings.TrimSpace(reg.Username)
	if username == "" || reg.Token == "" {
		return nil, nil
	}

	token, err := crypto.Decrypt(reg.Token)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt registry token: %w", err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil
	}

	return &resolvedRegistryCredential{
		Username:      username,
		Token:         token,
		ServerAddress: normalizeRegistryServerAddressInternal(reg.URL),
	}, nil
}

// browseRegistryInternal resolves a stored registry and its credential for browsing.
func (s *ContainerRegistryService) browseRegistryInternal(ctx context.Context, id string) (containerregistry.ContainerRegistry, *containerregistry.Credential, error) {
	reg, err := s.GetRegistryByID(ctx, id)
	if err != nil {
		return containerregistry.ContainerRegistry{}, nil, err
	}
	credential, err := s.credentialForRegistryInternal(ctx, reg)
	if err != nil {
		return containerregistry.ContainerRegistry{}, nil, err
	}
	result := containerregistry.ContainerRegistry{ID: reg.ID, URL: reg.URL, Username: reg.Username, Insecure: reg.Insecure, Enabled: reg.Enabled}
	if credential == nil {
		return result, nil, nil
	}
	return result, &containerregistry.Credential{URL: reg.URL, Username: credential.Username, Token: credential.Token, Enabled: true}, nil
}

// SyncRegistries syncs registries from a manager to this agent instance
// It creates, updates, or deletes registries to match the provided list
func (s *ContainerRegistryService) SyncRegistries(ctx context.Context, syncItems []containerregistry.Sync) error {
	if err := normalization.Normalize(&syncItems); err != nil {
		return err
	}
	existingMap, err := s.getExistingRegistriesMapInternal(ctx)
	if err != nil {
		return err
	}

	syncedIDs := make(map[string]bool)

	// Process each sync item
	for _, item := range syncItems {
		syncedIDs[item.ID] = true

		if processSyncItemErr := s.processSyncItemInternal(ctx, item, existingMap); processSyncItemErr != nil {
			return processSyncItemErr
		}
	}

	// Delete registries that are not in the sync list
	return s.deleteUnsyncedInternal(ctx, existingMap, syncedIDs)
}

func (s *ContainerRegistryService) getExistingRegistriesMapInternal(ctx context.Context) (map[string]*ContainerRegistry, error) {
	var existingRegistries []ContainerRegistry
	if err := s.db.WithContext(ctx).Find(&existingRegistries).Error; err != nil {
		return nil, fmt.Errorf("failed to get existing registries: %w", err)
	}

	existingMap := make(map[string]*ContainerRegistry)
	for i := range existingRegistries {
		existingMap[existingRegistries[i].ID] = &existingRegistries[i]
	}

	return existingMap, nil
}

func (s *ContainerRegistryService) processSyncItemInternal(ctx context.Context, item containerregistry.Sync, existingMap map[string]*ContainerRegistry) error {
	existing, exists := existingMap[item.ID]
	if exists {
		return s.updateExistingRegistryInternal(ctx, item, existing)
	}
	return s.createNewRegistryInternal(ctx, item)
}

func (s *ContainerRegistryService) updateExistingRegistryInternal(ctx context.Context, item containerregistry.Sync, existing *ContainerRegistry) error {
	needsUpdate, err := s.checkRegistryNeedsUpdateInternal(item, existing)
	if err != nil {
		return err
	}

	if needsUpdate {
		existing.UpdatedAt = time.Now()
		if syncRegistryErr := s.db.WithContext(ctx).Save(existing).Error; syncRegistryErr != nil {
			return fmt.Errorf("failed to update registry %s: %w", item.ID, syncRegistryErr)
		}
	}

	return nil
}

func (s *ContainerRegistryService) checkRegistryNeedsUpdateInternal(item containerregistry.Sync, existing *ContainerRegistry) (bool, error) {
	newType, err := NormalizeRegistryType(item.RegistryType)
	if err != nil {
		return false, err
	}

	needsUpdate := utils.ApplyChanged(&existing.URL, mo.Some(item.URL))
	needsUpdate = utils.ApplyNullable(&existing.Description, mo.PointerToOption(item.Description)) || needsUpdate
	needsUpdate = utils.ApplyChanged(&existing.Insecure, mo.Some(item.Insecure)) || needsUpdate
	needsUpdate = utils.ApplyChanged(&existing.Enabled, mo.Some(item.Enabled)) || needsUpdate

	// Normalizing first gives the manager and the local copy the same
	// representation, so the comparison below only reports real changes.
	repositoryNames, err := normalizeRepositoryNamesInternal(item.RepositoryNames)
	if err != nil {
		return false, err
	}
	needsUpdate = utils.ApplySliceChanged(&existing.RepositoryNames, mo.Some(repositoryNames)) || needsUpdate

	// Clear stale credentials when registry type changes during sync
	if newType != existing.RegistryType {
		if newType == RegistryTypeECR {
			existing.Username = ""
			existing.Token = ""
		} else {
			existing.AWSAccessKeyID = ""
			existing.AWSSecretAccessKey = ""
			existing.AWSRegion = ""
			existing.ECRToken = ""
			existing.ECRTokenGeneratedAt = nil
		}
		needsUpdate = true
	}

	needsUpdate = utils.ApplyChanged(&existing.RegistryType, mo.Some(newType)) || needsUpdate

	if newType == RegistryTypeGeneric {
		needsUpdate = utils.ApplyChanged(&existing.Username, mo.Some(item.Username)) || needsUpdate

		tokenChanged, applyEncryptedErr := utils.ApplyEncrypted(&existing.Token, item.Token)
		if applyEncryptedErr != nil {
			return false, fmt.Errorf("failed to apply token for registry %s: %w", existing.ID, applyEncryptedErr)
		}
		return tokenChanged || needsUpdate, nil
	}

	credChanged := utils.ApplyChanged(&existing.AWSAccessKeyID, mo.Some(item.AWSAccessKeyID))
	credChanged = utils.ApplyChanged(&existing.AWSRegion, mo.Some(item.AWSRegion)) || credChanged

	// An empty secret means the manager has no static keys, so the agent's stored one is cleared.
	secretChanged, encryptAWSSecretErr := utils.ApplyEncrypted(&existing.AWSSecretAccessKey, item.AWSSecretAccessKey)
	if encryptAWSSecretErr != nil {
		return false, fmt.Errorf("failed to apply AWS secret for registry %s: %w", existing.ID, encryptAWSSecretErr)
	}
	credChanged = secretChanged || credChanged

	// Invalidate cached ECR token when credentials change
	if credChanged {
		existing.ECRToken = ""
		existing.ECRTokenGeneratedAt = nil
	}
	needsUpdate = credChanged || needsUpdate

	return needsUpdate, nil
}

func (s *ContainerRegistryService) createNewRegistryInternal(ctx context.Context, item containerregistry.Sync) error {
	registryType, err := NormalizeRegistryType(item.RegistryType)
	if err != nil {
		return err
	}
	repositoryNames, err := normalizeRepositoryNamesInternal(item.RepositoryNames)
	if err != nil {
		return err
	}

	newRegistry := &ContainerRegistry{
		ID:              item.ID,
		URL:             item.URL,
		Description:     item.Description,
		Insecure:        item.Insecure,
		Enabled:         item.Enabled,
		RegistryType:    registryType,
		RepositoryNames: repositoryNames,
		AWSAccessKeyID:  item.AWSAccessKeyID,
		AWSRegion:       item.AWSRegion,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if registryType == RegistryTypeGeneric {
		newRegistry.Username = item.Username

		encryptedToken, encryptErr := crypto.Encrypt(item.Token)
		if encryptErr != nil {
			return fmt.Errorf("failed to encrypt token for new registry %s: %w", item.ID, encryptErr)
		}
		newRegistry.Token = encryptedToken
	} else if item.AWSSecretAccessKey != "" {
		encryptedSecret, encryptNewAWSSecretErr := crypto.Encrypt(item.AWSSecretAccessKey)
		if encryptNewAWSSecretErr != nil {
			return fmt.Errorf("failed to encrypt AWS secret for new registry %s: %w", item.ID, encryptNewAWSSecretErr)
		}
		newRegistry.AWSSecretAccessKey = encryptedSecret
	}
	if createRegistryErr := s.db.WithContext(ctx).Create(newRegistry).Error; createRegistryErr != nil {
		return fmt.Errorf("failed to create registry %s: %w", item.ID, createRegistryErr)
	}
	return nil
}

func NormalizeRegistryType(value string) (string, error) {
	registryType := strings.ToLower(strings.TrimSpace(value))
	if registryType == "" {
		return RegistryTypeGeneric, nil
	}

	switch registryType {
	case RegistryTypeGeneric, RegistryTypeECR:
		return registryType, nil
	default:
		return "", common.Classify(
			common.ErrValidation,
			&base.FieldError{
				Field: "registryType",
				Err: errors.New(
					"registry type must be one of: generic, ecr",
				),
			},
		)
	}
}

// normalizeRepositoryNamesInternal trims, filters and deduplicates the given
// entries (preserving first-occurrence order) and validates what remains. It
// returns a non-nil slice so that GORM always serializes to a JSON array.
func normalizeRepositoryNamesInternal(raw []string) (database.StringSlice, error) {
	names := kit.Unique(kit.TrimNonEmpty(raw))
	result := make(database.StringSlice, 0, len(names))
	for _, name := range names {
		// A repository name is only a path, so pair it with placeholder domain
		// and tag segments to validate it against the reference grammar.
		if _, err := reference.ParseNormalizedNamed("registry.invalid/" + name + "/placeholder:latest"); err != nil {
			return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "repositoryNames", Err: fmt.Errorf("invalid repository name %q", name)})
		}
		result = append(result, name)
	}

	return result, nil
}

func (s *ContainerRegistryService) deleteUnsyncedInternal(ctx context.Context, existingMap map[string]*ContainerRegistry, syncedIDs map[string]bool) error {
	for id := range existingMap {
		if !syncedIDs[id] {
			if err := s.db.WithContext(ctx).Where("id = ?", id).Delete(&ContainerRegistry{}).Error; err != nil {
				return fmt.Errorf("failed to delete registry %s: %w", id, err)
			}
		}
	}
	return nil
}

func normalizeImageReferenceForDistributionInternal(imageRef string) (string, string, error) {
	parts, err := refs.NormalizeReference(imageRef)
	if err != nil {
		return "", "", err
	}

	return parts.NormalizedRef, parts.RegistryHost, nil
}

func normalizeRegistryServerAddressInternal(registryURL string) string {
	normalizedHost := strings.TrimSpace(registryauth.NormalizeRegistryForComparison(registryURL))
	if normalizedHost == "" {
		return ""
	}

	if normalizedHost == "docker.io" {
		return registryauth.NormalizeRegistryURL(registryURL)
	}

	return normalizedHost
}

func isRateLimitErrorInternal(err error) bool {
	if err == nil {
		return false
	}
	return IsRateLimitErrorString(err.Error())
}

func IsRateLimitErrorString(msg string) bool {
	msgLower := strings.ToLower(msg)
	indicators := []string{
		"toomanyrequests",
		"rate limit",
		"too many requests",
		"status: 429",
		"status 429",
		"retry-after",
	}
	for _, indicator := range indicators {
		if strings.Contains(msgLower, indicator) {
			return true
		}
	}
	return false
}

func isDockerHubRegistryInternal(registryHost string) bool {
	return registryauth.NormalizeRegistryForComparison(registryHost) == "docker.io"
}

func isDistributionFallbackEligibleInternal(err error) bool {
	if err == nil {
		return false
	}

	if registry.IsFallbackEligibleDaemonError(err) {
		return true
	}

	if browse.IsUnauthorizedRegistryError(err) {
		return false
	}

	errLower := strings.ToLower(err.Error())
	return strings.Contains(errLower, "context deadline exceeded") ||
		strings.Contains(errLower, "client.timeout exceeded") ||
		strings.Contains(errLower, "i/o timeout")
}

func (
	s *ContainerRegistryService,
) fetchDigestFromDaemonInternal(
	ctx context.Context,
	dockerClient RegistryDaemonClient,
	registryHost, normalizedRef string,
	credential *resolvedRegistryCredential,
) (
	string,
	error,
) {
	var inspectOptions client.DistributionInspectOptions
	if credential != nil {
		authHeader, err := registryauth.EncodeAuthHeader(credential.Username, credential.Token, credential.ServerAddress)
		if err != nil {
			return "", fmt.Errorf("encode registry auth header for %s: %w", registryHost, err)
		}
		inspectOptions.EncodedRegistryAuth = authHeader
	}

	inspectResult, err := dockerClient.DistributionInspect(ctx, normalizedRef, inspectOptions)
	if err != nil {
		return "", err
	}
	digestValue, err := digest.Normalize(inspectResult.Descriptor.Digest.String())
	if err != nil {
		return "", fmt.Errorf("distribution inspect returned invalid digest for %s: %w", normalizedRef, err)
	}
	return digestValue, nil
}

func (s *ContainerRegistryService) fetchDigestFromRegistryInternal(ctx context.Context, registryHost, repository, tag string, credential *resolvedRegistryCredential) (string, error) {
	var distributionCredential *authn.AuthConfig
	if credential != nil {
		distributionCredential = &authn.AuthConfig{
			Username: strings.TrimSpace(credential.Username),
			Password: strings.TrimSpace(credential.Token),
		}
	}

	return registry.FetchDigest(
		ctx,
		registryHost,
		repository,
		tag,
		distributionCredential,
		s.distributionHTTPClient,
	)
}

// ListImageTags discovers repository tags with the same credential precedence as
// digest checks. External credentials replace local credentials when supplied.
func (s *ContainerRegistryService) ListImageTags(ctx context.Context, imageRef string, externalCreds []containerregistry.Credential) ([]string, error) {
	if refs.IsDigestPinnedReference(imageRef) || refs.IsImageIDLikeReference(imageRef) {
		return nil, errors.New("cannot discover tags for an immutable image reference")
	}
	parts, err := refs.NormalizeReference(imageRef)
	if err != nil {
		return nil, err
	}
	lookupCtx, cancel := s.tagLookupContextInternal(ctx)
	defer cancel()

	tags, _, err := registryOperationWithCredentialsInternal(lookupCtx, s, parts.RegistryHost, "registry tag listing of "+parts.NormalizedRef, externalCreds,
		func(ctx context.Context, credential *resolvedRegistryCredential) ([]string, error) {
			var auth *authn.AuthConfig
			if credential != nil {
				auth = &authn.AuthConfig{Username: credential.Username, Password: credential.Token}
			}
			return registry.FetchTags(ctx, parts.RegistryHost, parts.Repository, auth, s.distributionHTTPClient)
		})
	return tags, err
}

// tagLookupContextInternal bounds tag and manifest walks by the configured registry tag timeout.
func (s *ContainerRegistryService) tagLookupContextInternal(ctx context.Context) (context.Context, context.CancelFunc) {
	timeoutSeconds := 0
	if s.settingsService != nil {
		timeoutSeconds = s.settingsService.GetSettingsConfig().RegistryTagTimeout.AsInt()
	}
	return context.WithTimeout(ctx, timeouts.GetDuration(timeoutSeconds, timeouts.DefaultRegistryTags))
}

const ecrTokenTTL = 12 * time.Hour

type ecrTokenResult struct {
	username string
	password string
}

// GetOrRefreshECRToken returns a valid ECR auth token (username + password) for the given
// registry. If the cached token (stored encrypted in the DB) is still within its 12-hour
// validity window it is returned directly; otherwise a new token is obtained from the AWS
// ECR API, persisted back to the DB, and returned.
// Concurrent refreshes for the same registry are deduplicated via singleflight.
func (s *ContainerRegistryService) GetOrRefreshECRToken(ctx context.Context, reg *ContainerRegistry) (username, password string, err error) {
	if endpointErr := validateECRRegistryEndpoint(reg.URL, reg.Insecure); endpointErr != nil {
		return "", "", endpointErr
	}
	// Fast path: return cached token if still valid.
	if reg.ECRTokenGeneratedAt != nil && time.Since(reg.ECRTokenGeneratedAt.UTC()) < ecrTokenTTL {
		if reg.ECRToken != "" {
			decrypted, decErr := crypto.Decrypt(reg.ECRToken)
			if decErr == nil && strings.TrimSpace(decrypted) != "" {
				return "AWS", decrypted, nil
			}
		}
	}

	// Slow path: deduplicate concurrent refreshes for the same registry.
	result, sErr, _ := s.ecrRefreshGroup.Do(reg.ID, func() (any, error) {
		// Detach from the request context so a cancelled caller doesn't
		// abort the shared refresh for all waiting goroutines.
		refreshCtx := context.WithoutCancel(ctx)
		return s.refreshECRTokenInternal(refreshCtx, reg)
	})
	if sErr != nil {
		return "", "", sErr
	}
	r, ok := result.(*ecrTokenResult)
	if !ok {
		return "", "", errors.New("unexpected ECR token result type")
	}
	return r.username, r.password, nil
}

func (s *ContainerRegistryService) refreshECRTokenInternal(ctx context.Context, reg *ContainerRegistry) (*ecrTokenResult, error) {
	configOptions := []func(*config.LoadOptions) error{config.WithRegion(reg.AWSRegion)}

	// Without a static access key, the default AWS credential chain applies
	// (env, shared config, ECS task role, EC2 instance profile).
	if strings.TrimSpace(reg.AWSAccessKeyID) != "" {
		secretKey, decErr := crypto.Decrypt(reg.AWSSecretAccessKey)
		if decErr != nil {
			return nil, fmt.Errorf("failed to decrypt AWS secret key for registry %s: %w", reg.URL, decErr)
		}
		secretKey = strings.TrimSpace(secretKey)
		if secretKey == "" {
			return nil, fmt.Errorf("AWS secret access key is empty for registry %s", reg.URL)
		}
		configOptions = append(configOptions, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			reg.AWSAccessKeyID,
			secretKey,
			"",
		)))
	}

	// Call AWS ECR GetAuthorizationToken.
	cfg, cfgErr := config.LoadDefaultConfig(ctx, configOptions...)
	if cfgErr != nil {
		return nil, fmt.Errorf("failed to load AWS config for registry %s: %w", reg.URL, cfgErr)
	}

	ecrClient := ecr.NewFromConfig(cfg)
	result, ecrErr := ecrClient.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if ecrErr != nil {
		return nil, fmt.Errorf("failed to get ECR authorization token for registry %s: %w", reg.URL, ecrErr)
	}
	if len(result.AuthorizationData) == 0 || result.AuthorizationData[0].AuthorizationToken == nil {
		return nil, fmt.Errorf("ECR returned empty authorization data for registry %s", reg.URL)
	}

	// Decode base64 token → "AWS:<password>".
	decoded, decodeErr := base64.StdEncoding.DecodeString(*result.AuthorizationData[0].AuthorizationToken)
	if decodeErr != nil {
		return nil, fmt.Errorf("failed to decode ECR token for registry %s: %w", reg.URL, decodeErr)
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return nil, fmt.Errorf("unexpected ECR token format for registry %s", reg.URL)
	}
	ecrPassword := parts[1]

	// Persist the new token (encrypted) and generation timestamp.
	encryptedToken, encErr := crypto.Encrypt(ecrPassword)
	if encErr != nil {
		return nil, fmt.Errorf("failed to encrypt ECR token for registry %s: %w", reg.URL, encErr)
	}
	now := time.Now().UTC()
	reg.ECRToken = encryptedToken
	reg.ECRTokenGeneratedAt = &now
	if saveErr := s.db.WithContext(ctx).Model(reg).Updates(map[string]any{
		"ecr_token":              encryptedToken,
		"ecr_token_generated_at": now,
	}).Error; saveErr != nil {
		// Non-fatal: log but continue — the token is still usable for this call.
		slog.WarnContext(ctx, "failed to persist ECR token to database", "registry", reg.URL, "error", saveErr)
	}

	return &ecrTokenResult{username: "AWS", password: ecrPassword}, nil
}

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
		return "", fmt.Errorf("invalid image reference %q: %w", imageRef, err)
	}

	img, err := remote.Image(parsedRef,
		remote.WithContext(ctx),
		remote.WithPlatform(v1.Platform{OS: "linux", Architecture: runtime.GOARCH}))
	if err != nil {
		return "", fmt.Errorf("version label fetch failed for %s: %w", imageRef, err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return "", fmt.Errorf("version label fetch failed for %s: %w", imageRef, err)
	}
	if cfg == nil || strings.TrimSpace(cfg.Config.Labels[ociImageVersionLabel]) == "" {
		return "", ErrNoVersionLabel
	}
	return strings.TrimSpace(cfg.Config.Labels[ociImageVersionLabel]), nil
}
