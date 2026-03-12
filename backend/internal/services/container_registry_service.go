package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
	"github.com/getarcaneapp/arcane/backend/internal/utils/cache"
	"github.com/getarcaneapp/arcane/backend/internal/utils/crypto"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	utilsregistry "github.com/getarcaneapp/arcane/backend/internal/utils/registry"
	"github.com/getarcaneapp/arcane/types/containerregistry"
	dockerregistry "github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	ref "go.podman.io/image/v5/docker/reference"
)

const (
	registryCacheTTL = 30 * time.Minute
)

type RegistryDaemonClient interface {
	RegistryLogin(ctx context.Context, options client.RegistryLoginOptions) (client.RegistryLoginResult, error)
	DistributionInspect(ctx context.Context, imageRef string, options client.DistributionInspectOptions) (client.DistributionInspectResult, error)
}

type registryDaemonGetter func(context.Context) (RegistryDaemonClient, error)

type registryDigestResult struct {
	Digest         string
	AuthMethod     string
	AuthUsername   string
	AuthRegistry   string
	UsedCredential bool
}

type resolvedRegistryCredential struct {
	Username      string
	Token         string
	ServerAddress string
}

type ContainerRegistryService struct {
	db           *database.DB
	dockerClient registryDaemonGetter
	cache        map[string]*cache.Cache[string] // imageRef -> digest cache
	cacheMu      sync.RWMutex
}

func NewContainerRegistryService(db *database.DB, dockerClient registryDaemonGetter) *ContainerRegistryService {
	return &ContainerRegistryService{
		db:           db,
		dockerClient: dockerClient,
		cache:        make(map[string]*cache.Cache[string]),
	}
}

func (s *ContainerRegistryService) GetAllRegistries(ctx context.Context) ([]models.ContainerRegistry, error) {
	var registries []models.ContainerRegistry
	if err := s.db.WithContext(ctx).Find(&registries).Error; err != nil {
		return nil, fmt.Errorf("failed to get container registries: %w", err)
	}
	return registries, nil
}

func (s *ContainerRegistryService) GetRegistriesPaginated(ctx context.Context, params pagination.QueryParams) ([]containerregistry.ContainerRegistry, pagination.Response, error) {
	var registries []models.ContainerRegistry
	q := s.db.WithContext(ctx).Model(&models.ContainerRegistry{})

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"url LIKE ? OR username LIKE ? OR COALESCE(description, '') LIKE ?",
			searchPattern, searchPattern, searchPattern,
		)
	}

	q = pagination.ApplyBooleanFilter(q, "enabled", params.Filters["enabled"])
	q = pagination.ApplyBooleanFilter(q, "insecure", params.Filters["insecure"])

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &registries)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate container registries: %w", err)
	}

	out, mapErr := mapper.MapSlice[models.ContainerRegistry, containerregistry.ContainerRegistry](registries)
	if mapErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to map registries: %w", mapErr)
	}

	return out, paginationResp, nil
}

func (s *ContainerRegistryService) GetRegistryByID(ctx context.Context, id string) (*models.ContainerRegistry, error) {
	var registry models.ContainerRegistry
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&registry).Error; err != nil {
		return nil, fmt.Errorf("failed to get container registry: %w", err)
	}
	return &registry, nil
}

func (s *ContainerRegistryService) CreateRegistry(ctx context.Context, req models.CreateContainerRegistryRequest) (*models.ContainerRegistry, error) {
	// Encrypt the token before storing
	encryptedToken, err := crypto.Encrypt(req.Token)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt token: %w", err)
	}

	registry := &models.ContainerRegistry{
		URL:         req.URL,
		Username:    req.Username,
		Token:       encryptedToken,
		Description: req.Description,
		Insecure:    req.Insecure != nil && *req.Insecure,
		Enabled:     req.Enabled == nil || *req.Enabled,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := s.db.WithContext(ctx).Create(registry).Error; err != nil {
		return nil, fmt.Errorf("failed to create registry: %w", err)
	}

	return registry, nil
}

func (s *ContainerRegistryService) UpdateRegistry(ctx context.Context, id string, req models.UpdateContainerRegistryRequest) (*models.ContainerRegistry, error) {
	registry, err := s.GetRegistryByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update fields
	utils.UpdateIfChanged(&registry.URL, req.URL)
	utils.UpdateIfChanged(&registry.Username, req.Username)

	if req.Token != nil && *req.Token != "" {
		// Encrypt the new token
		encryptedToken, err := crypto.Encrypt(*req.Token)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt token: %w", err)
		}
		utils.UpdateIfChanged(&registry.Token, encryptedToken)
	}

	utils.UpdateIfChanged(&registry.Description, req.Description)
	utils.UpdateIfChanged(&registry.Insecure, req.Insecure)
	utils.UpdateIfChanged(&registry.Enabled, req.Enabled)

	registry.UpdatedAt = time.Now()

	if err := s.db.WithContext(ctx).Save(registry).Error; err != nil {
		return nil, fmt.Errorf("failed to update registry: %w", err)
	}

	return registry, nil
}

func (s *ContainerRegistryService) DeleteRegistry(ctx context.Context, id string) error {
	if err := s.db.WithContext(ctx).Where("id = ?", id).Delete(&models.ContainerRegistry{}).Error; err != nil {
		return fmt.Errorf("failed to delete container registry: %w", err)
	}
	return nil
}

// GetDecryptedToken returns the decrypted token for a registry
func (s *ContainerRegistryService) GetDecryptedToken(ctx context.Context, id string) (string, error) {
	registry, err := s.GetRegistryByID(ctx, id)
	if err != nil {
		return "", err
	}

	decryptedToken, err := crypto.Decrypt(registry.Token)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt token: %w", err)
	}

	return decryptedToken, nil
}

// GetEnabledRegistries returns all enabled registries
func (s *ContainerRegistryService) GetEnabledRegistries(ctx context.Context) ([]models.ContainerRegistry, error) {
	var registries []models.ContainerRegistry
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&registries).Error; err != nil {
		return nil, fmt.Errorf("failed to get enabled container registries: %w", err)
	}
	return registries, nil
}

// GetRegistryAuthForImage returns X-Registry-Auth for the image's registry host.
func (s *ContainerRegistryService) GetRegistryAuthForImage(ctx context.Context, imageRef string) (string, error) {
	registryHost, err := utilsregistry.GetRegistryAddress(imageRef)
	if err != nil {
		return "", err
	}
	return s.GetRegistryAuthForHost(ctx, registryHost)
}

// GetRegistryAuthForHost returns X-Registry-Auth for a configured and enabled registry.
func (s *ContainerRegistryService) GetRegistryAuthForHost(ctx context.Context, registryHost string) (string, error) {
	normalizedRegistryHost := utilsregistry.NormalizeRegistryForComparison(registryHost)
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

	return utilsregistry.EncodeAuthHeader(cfg.Username, cfg.Password, cfg.ServerAddress)
}

func (s *ContainerRegistryService) GetAllRegistryAuthConfigs(ctx context.Context) (map[string]dockerregistry.AuthConfig, error) {
	registries, err := s.GetEnabledRegistries(ctx)
	if err != nil {
		return nil, err
	}

	authConfigs := make(map[string]dockerregistry.AuthConfig, len(registries))
	for _, reg := range registries {
		username := strings.TrimSpace(reg.Username)
		if !reg.Enabled || username == "" || reg.Token == "" {
			continue
		}

		decryptedToken, decryptErr := crypto.Decrypt(reg.Token)
		if decryptErr != nil {
			return nil, fmt.Errorf("failed to decrypt token for registry %s: %w", reg.URL, decryptErr)
		}
		token := strings.TrimSpace(decryptedToken)
		if token == "" {
			continue
		}

		normalizedHost := strings.TrimSpace(utilsregistry.NormalizeRegistryForComparison(reg.URL))
		if normalizedHost == "" {
			continue
		}

		serverAddress := normalizedHost
		if normalizedHost == "docker.io" {
			serverAddress = utilsregistry.NormalizeRegistryURL(reg.URL)
		}
		if serverAddress == "" {
			continue
		}

		authConfig := dockerregistry.AuthConfig{
			Username:      username,
			Password:      token,
			ServerAddress: serverAddress,
		}
		for _, key := range utilsregistry.RegistryAuthLookupKeys(normalizedHost) {
			authConfigs[key] = authConfig
		}
	}

	if len(authConfigs) == 0 {
		return nil, nil
	}

	return authConfigs, nil
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

// GetImageDigest fetches the current digest for an image:tag from the registry
// This is used for digest-based update detection for non-semver tags
func (s *ContainerRegistryService) GetImageDigest(ctx context.Context, imageRef string) (string, error) {
	normalizedRef, _, err := normalizeImageReferenceForDistributionInternal(imageRef)
	if err != nil {
		return "", err
	}

	// Build a cache key from the full image reference
	cacheKey := normalizedRef

	// Get or create a cache for this specific image reference
	s.cacheMu.RLock()
	imageCache, exists := s.cache[cacheKey]
	s.cacheMu.RUnlock()

	if !exists {
		s.cacheMu.Lock()
		if imageCache, exists = s.cache[cacheKey]; !exists {
			imageCache = cache.New[string](registryCacheTTL)
			s.cache[cacheKey] = imageCache
		}
		s.cacheMu.Unlock()
	}

	digest, err := imageCache.GetOrFetch(ctx, func(ctx context.Context) (string, error) {
		// Pass the original imageRef; inspectImageDigestInternal normalizes internally.
		result, fetchErr := s.inspectImageDigestInternal(ctx, imageRef, nil)
		if fetchErr != nil {
			return "", fetchErr
		}
		return result.Digest, nil
	})

	var staleErr *cache.ErrStale
	if err != nil && !errors.As(err, &staleErr) {
		return "", err
	}

	return digest, nil
}

func (s *ContainerRegistryService) inspectImageDigestInternal(ctx context.Context, imageRef string, externalCreds []containerregistry.Credential) (*registryDigestResult, error) {
	normalizedRef, registryHost, err := normalizeImageReferenceForDistributionInternal(imageRef)
	if err != nil {
		return nil, err
	}

	dockerClient, err := s.getDockerClientInternal(ctx)
	if err != nil {
		return nil, err
	}

	inspectResult, err := dockerClient.DistributionInspect(ctx, normalizedRef, client.DistributionInspectOptions{})
	if err == nil {
		digest := strings.TrimSpace(string(inspectResult.Descriptor.Digest))
		if digest == "" {
			return nil, fmt.Errorf("distribution inspect returned empty digest for %s", normalizedRef)
		}
		return &registryDigestResult{
			Digest:       digest,
			AuthMethod:   "anonymous",
			AuthRegistry: registryHost,
		}, nil
	}
	if !isUnauthorizedRegistryErrorInternal(err) {
		return &registryDigestResult{AuthMethod: "anonymous", AuthRegistry: registryHost},
			fmt.Errorf("distribution inspect failed for %s: %w", normalizedRef, err)
	}

	credentials, credErr := s.getMatchingRegistryCredentialsInternal(ctx, registryHost, externalCreds)
	if credErr != nil {
		return &registryDigestResult{AuthMethod: "anonymous", AuthRegistry: registryHost}, credErr
	}

	lastErr := err
	var lastCred resolvedRegistryCredential
	for _, credential := range credentials {
		lastCred = credential
		authHeader, encodeErr := utilsregistry.EncodeAuthHeader(credential.Username, credential.Token, credential.ServerAddress)
		if encodeErr != nil {
			return nil, fmt.Errorf("encode registry auth header for %s: %w", registryHost, encodeErr)
		}

		inspectResult, err = dockerClient.DistributionInspect(ctx, normalizedRef, client.DistributionInspectOptions{
			EncodedRegistryAuth: authHeader,
		})
		if err == nil {
			digest := strings.TrimSpace(string(inspectResult.Descriptor.Digest))
			if digest == "" {
				return nil, fmt.Errorf("distribution inspect returned empty digest for %s", normalizedRef)
			}
			return &registryDigestResult{
				Digest:         digest,
				AuthMethod:     "credential",
				AuthUsername:   credential.Username,
				AuthRegistry:   registryHost,
				UsedCredential: true,
			}, nil
		}
		lastErr = err
		if !isUnauthorizedRegistryErrorInternal(err) {
			return &registryDigestResult{
				AuthMethod:     "credential",
				AuthUsername:   credential.Username,
				AuthRegistry:   registryHost,
				UsedCredential: true,
			}, fmt.Errorf("distribution inspect failed for %s with credentials: %w", normalizedRef, err)
		}
	}

	partial := &registryDigestResult{AuthMethod: "anonymous", AuthRegistry: registryHost}
	if lastCred.Username != "" {
		partial.AuthMethod = "credential"
		partial.AuthUsername = lastCred.Username
		partial.UsedCredential = true
	}
	return partial, fmt.Errorf("distribution inspect failed for %s: %w", normalizedRef, lastErr)
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

func (s *ContainerRegistryService) getMatchingRegistryCredentialsInternal(ctx context.Context, registryHost string, externalCreds []containerregistry.Credential) ([]resolvedRegistryCredential, error) {
	if len(externalCreds) > 0 {
		credentials := make([]resolvedRegistryCredential, 0, len(externalCreds))
		for _, cred := range externalCreds {
			if !cred.Enabled || strings.TrimSpace(cred.Username) == "" || strings.TrimSpace(cred.Token) == "" {
				continue
			}
			if !utilsregistry.IsRegistryMatch(cred.URL, registryHost) {
				continue
			}

			credentials = append(credentials, resolvedRegistryCredential{
				Username:      strings.TrimSpace(cred.Username),
				Token:         strings.TrimSpace(cred.Token),
				ServerAddress: normalizeRegistryServerAddressInternal(cred.URL),
			})
		}
		return credentials, nil
	}

	registries, err := s.GetEnabledRegistries(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load enabled registries: %w", err)
	}

	credentials := make([]resolvedRegistryCredential, 0, len(registries))
	for _, reg := range registries {
		if !utilsregistry.IsRegistryMatch(reg.URL, registryHost) {
			continue
		}

		username := strings.TrimSpace(reg.Username)
		if username == "" || reg.Token == "" {
			continue
		}

		token, decryptErr := crypto.Decrypt(reg.Token)
		if decryptErr != nil {
			slog.WarnContext(ctx, "failed to decrypt registry token", "registry", reg.URL, "error", decryptErr)
			continue
		}
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}

		credentials = append(credentials, resolvedRegistryCredential{
			Username:      username,
			Token:         token,
			ServerAddress: normalizeRegistryServerAddressInternal(reg.URL),
		})
	}

	return credentials, nil
}

// SyncRegistries syncs registries from a manager to this agent instance
// It creates, updates, or deletes registries to match the provided list
func (s *ContainerRegistryService) SyncRegistries(ctx context.Context, syncItems []containerregistry.Sync) error {
	existingMap, err := s.getExistingRegistriesMapInternal(ctx)
	if err != nil {
		return err
	}

	syncedIDs := make(map[string]bool)

	// Process each sync item
	for _, item := range syncItems {
		syncedIDs[item.ID] = true

		if err := s.processSyncItemInternal(ctx, item, existingMap); err != nil {
			return err
		}
	}

	// Delete registries that are not in the sync list
	return s.deleteUnsyncedInternal(ctx, existingMap, syncedIDs)
}

func (s *ContainerRegistryService) getExistingRegistriesMapInternal(ctx context.Context) (map[string]*models.ContainerRegistry, error) {
	var existingRegistries []models.ContainerRegistry
	if err := s.db.WithContext(ctx).Find(&existingRegistries).Error; err != nil {
		return nil, fmt.Errorf("failed to get existing registries: %w", err)
	}

	existingMap := make(map[string]*models.ContainerRegistry)
	for i := range existingRegistries {
		existingMap[existingRegistries[i].ID] = &existingRegistries[i]
	}

	return existingMap, nil
}

func (s *ContainerRegistryService) processSyncItemInternal(ctx context.Context, item containerregistry.Sync, existingMap map[string]*models.ContainerRegistry) error {
	existing, exists := existingMap[item.ID]
	if exists {
		return s.updateExistingRegistryInternal(ctx, item, existing)
	}
	return s.createNewRegistryInternal(ctx, item)
}

func (s *ContainerRegistryService) updateExistingRegistryInternal(ctx context.Context, item containerregistry.Sync, existing *models.ContainerRegistry) error {
	needsUpdate := s.checkRegistryNeedsUpdateInternal(item, existing)

	if needsUpdate {
		existing.UpdatedAt = time.Now()
		if err := s.db.WithContext(ctx).Save(existing).Error; err != nil {
			return fmt.Errorf("failed to update registry %s: %w", item.ID, err)
		}
	}

	return nil
}

func (s *ContainerRegistryService) checkRegistryNeedsUpdateInternal(item containerregistry.Sync, existing *models.ContainerRegistry) bool {
	needsUpdate := utils.UpdateIfChanged(&existing.URL, item.URL)
	needsUpdate = utils.UpdateIfChanged(&existing.Username, item.Username) || needsUpdate

	// Always update token as it comes decrypted from manager
	encryptedToken, err := crypto.Encrypt(item.Token)
	if err == nil {
		needsUpdate = utils.UpdateIfChanged(&existing.Token, encryptedToken) || needsUpdate
	}

	needsUpdate = utils.UpdateIfChanged(&existing.Description, item.Description) || needsUpdate
	needsUpdate = utils.UpdateIfChanged(&existing.Insecure, item.Insecure) || needsUpdate
	needsUpdate = utils.UpdateIfChanged(&existing.Enabled, item.Enabled) || needsUpdate

	return needsUpdate
}

func (s *ContainerRegistryService) createNewRegistryInternal(ctx context.Context, item containerregistry.Sync) error {
	encryptedToken, err := crypto.Encrypt(item.Token)
	if err != nil {
		return fmt.Errorf("failed to encrypt token for new registry %s: %w", item.ID, err)
	}

	newRegistry := &models.ContainerRegistry{
		BaseModel: models.BaseModel{
			ID: item.ID,
		},
		URL:         item.URL,
		Username:    item.Username,
		Token:       encryptedToken,
		Description: item.Description,
		Insecure:    item.Insecure,
		Enabled:     item.Enabled,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := s.db.WithContext(ctx).Create(newRegistry).Error; err != nil {
		return fmt.Errorf("failed to create registry %s: %w", item.ID, err)
	}

	return nil
}

func (s *ContainerRegistryService) deleteUnsyncedInternal(ctx context.Context, existingMap map[string]*models.ContainerRegistry, syncedIDs map[string]bool) error {
	for id := range existingMap {
		if !syncedIDs[id] {
			if err := s.db.WithContext(ctx).Where("id = ?", id).Delete(&models.ContainerRegistry{}).Error; err != nil {
				return fmt.Errorf("failed to delete registry %s: %w", id, err)
			}
		}
	}
	return nil
}

func normalizeImageReferenceForDistributionInternal(imageRef string) (string, string, error) {
	named, err := ref.ParseNormalizedNamed(strings.TrimSpace(imageRef))
	if err != nil {
		return "", "", fmt.Errorf("invalid image reference %q: %w", imageRef, err)
	}

	if _, ok := named.(ref.Digested); ok {
		return "", "", fmt.Errorf("digest-pinned references are not supported for distribution inspect: %q", imageRef)
	}

	registryHost := utilsregistry.NormalizeRegistryForComparison(ref.Domain(named))
	repository := ref.Path(named)

	tag := "latest"
	if tagged, ok := named.(ref.NamedTagged); ok {
		tag = tagged.Tag()
	}

	return registryHost + "/" + repository + ":" + tag, registryHost, nil
}

func normalizeRegistryServerAddressInternal(registryURL string) string {
	normalizedHost := strings.TrimSpace(utilsregistry.NormalizeRegistryForComparison(registryURL))
	if normalizedHost == "" {
		return ""
	}

	if normalizedHost == "docker.io" {
		return utilsregistry.NormalizeRegistryURL(registryURL)
	}

	return normalizedHost
}

func isUnauthorizedRegistryErrorInternal(err error) bool {
	if err == nil {
		return false
	}

	// Prefer structured error type check from the Docker SDK / containerd.
	if cerrdefs.IsUnauthorized(err) || cerrdefs.IsPermissionDenied(err) {
		return true
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
	}

	for _, indicator := range indicators {
		if strings.Contains(errLower, indicator) {
			return true
		}
	}

	return false
}
