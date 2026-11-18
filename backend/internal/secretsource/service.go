package secretsource

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/secretsource/children/bitwarden"
	"getarcane.dev/arcane/v3/internal/secretsource/children/doppler"
	"getarcane.dev/arcane/v3/internal/secretsource/children/httpsource"
	"getarcane.dev/arcane/v3/internal/secretsource/children/infisical"
	"getarcane.dev/arcane/v3/internal/secretsource/children/onepassword"
	"getarcane.dev/arcane/v3/internal/secretsource/children/protonpass"
	"getarcane.dev/arcane/v3/internal/secretsource/children/setupplan"
	"getarcane.dev/arcane/v3/internal/secretsource/children/vault"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

type (
	// secretProvider is what each provider child implements.
	secretProvider interface {
		// Test reports connection problems in the result rather than as an error.
		Test(ctx context.Context) types.SecretSourceTestResult
		Browse(ctx context.Context, query types.SecretSourceBrowseQuery) ([]types.SecretSourceBrowseItem, error)
		// Fetch returns the target's values and the names of entries it skipped
		// because they have no usable value.
		Fetch(ctx context.Context, target types.SecretSourceBindingTarget) (map[string]string, []string, error)
	}

	// ProjectRef identifies the project a binding belongs to. Path and
	// ProjectsDirectory are only needed to report .env keys that a secret overrides.
	ProjectRef struct {
		ID                string
		Name              string
		Path              string
		ProjectsDirectory string
	}

	// ProjectResolver looks up a project of the local environment.
	ProjectResolver func(ctx context.Context, projectID string) (ProjectRef, error)

	// fetchOptions tunes one fetch. Quiet fetches (background checks) log no
	// success event and log an error only when it differs from the last one.
	fetchOptions struct {
		reason      string
		requireKeys bool
		quiet       bool
	}

	// SecretSourceService manages secret sources, project bindings, deploy-time
	// fetches, and background drift checks. Secret values are only held in memory
	// for the duration of a deploy or check.
	SecretSourceService struct {
		projectFiles ProjectFileAccess
		db           *database.DB
		eventService *event.EventService
		httpClient   *http.Client
		providers    map[string]cachedProvider
		providersMu  sync.Mutex
	}

	cachedProvider struct {
		provider    secretProvider
		fingerprint string
	}
)

const (
	providerTimeout    = 30 * time.Second
	localEnvironmentID = "0"
)

var (
	// envKeyPattern matches names that compose can interpolate and pass through.
	envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func NewSecretSourceService(db *database.DB, eventService *event.EventService) *SecretSourceService {
	return &SecretSourceService{
		db:           db,
		eventService: eventService,
		// Not the SSRF-safe client: self-hosted secret managers and bw serve
		// normally live on private, VPN, tailnet, or Docker network addresses,
		// which that client rejects. Cloud metadata addresses are still
		// refused (see transport.go), and creating a source requires the
		// secret-sources:create permission.
		httpClient: newSecretHTTPClientInternal(providerTimeout),
		providers:  make(map[string]cachedProvider),
	}
}

// ---- Sources ----

func (s *SecretSourceService) ListSources(ctx context.Context) ([]types.SecretSource, error) {
	var sources []SecretSource
	if err := s.db.WithContext(ctx).Order("name ASC").Find(&sources).Error; err != nil {
		return nil, fmt.Errorf("failed to list secret sources: %w", err)
	}
	counts, err := s.bindingCountsInternal(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]types.SecretSource, 0, len(sources))
	for i := range sources {
		result = append(result, sourceToDTOInternal(&sources[i], counts[sources[i].ID]))
	}
	return result, nil
}

func (s *SecretSourceService) GetSource(ctx context.Context, id string) (*types.SecretSource, error) {
	source, err := s.loadSourceInternal(ctx, id)
	if err != nil {
		return nil, err
	}
	counts, err := s.bindingCountsInternal(ctx)
	if err != nil {
		return nil, err
	}
	dto := sourceToDTOInternal(source, counts[source.ID])
	return &dto, nil
}

func (s *SecretSourceService) CreateSource(ctx context.Context, req types.SecretSourceCreateRequest) (*types.SecretSource, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, common.Classify(common.ErrSecretSourceInvalid, errors.New("name is required"))
	}
	settings, err := normalizeSettingsInternal(req.Provider, req.Settings)
	if err != nil {
		return nil, err
	}
	credential := ""
	if providerNeedsCredentialInternal(req.Provider) && req.Credential == "" {
		return nil, common.Classify(common.ErrSecretSourceInvalid, errors.New(credentialLabelInternal(req.Provider)+" is required"))
	}
	if providerAcceptsCredentialInternal(req.Provider) && req.Credential != "" {
		if credential, err = crypto.Encrypt(req.Credential); err != nil {
			return nil, fmt.Errorf("failed to encrypt credential: %w", err)
		}
	}
	if conflictErr := s.ensureNameAvailableInternal(ctx, name, ""); conflictErr != nil {
		return nil, conflictErr
	}

	source := SecretSource{Name: name, Provider: req.Provider, Settings: settings, Credential: credential}
	if setupErr := applySetupCredentialInternal(&source, "", req.SetupCredential); setupErr != nil {
		return nil, setupErr
	}
	if createErr := s.db.WithContext(ctx).Create(&source).Error; createErr != nil {
		return nil, fmt.Errorf("failed to create secret source: %w", createErr)
	}
	dto := sourceToDTOInternal(&source, 0)
	return &dto, nil
}

func (s *SecretSourceService) UpdateSource(ctx context.Context, id string, req types.SecretSourceUpdateRequest) (*types.SecretSource, error) {
	source, err := s.loadSourceInternal(ctx, id)
	if err != nil {
		return nil, err
	}
	beforeFingerprint := connectionFingerprintInternal(source)
	beforeSetupClientID := setupClientIDInternal(source.Settings)
	beforeEndpoint := endpointKeyInternal(source.Settings)

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, common.Classify(common.ErrSecretSourceInvalid, errors.New("name is required"))
		}
		if conflictErr := s.ensureNameAvailableInternal(ctx, name, source.ID); conflictErr != nil {
			return nil, conflictErr
		}
		source.Name = name
	}
	if req.Settings != nil {
		settings, settingsErr := normalizeSettingsInternal(source.Provider, *req.Settings)
		if settingsErr != nil {
			return nil, settingsErr
		}
		source.Settings = settings
	}
	newCredential := req.Credential != nil && *req.Credential != ""
	if req.ClearCredential {
		if !providerAcceptsCredentialInternal(source.Provider) || providerNeedsCredentialInternal(source.Provider) {
			return nil, common.Classify(common.ErrSecretSourceInvalid, errors.New("only an optional token, such as an HTTP endpoint's, can be removed"))
		}
		source.Credential = ""
	}
	// A stored credential only goes to the address it was saved for. When the
	// address or login method changes, it has to be entered again.
	endpointChanged := endpointKeyInternal(source.Settings) != beforeEndpoint
	if endpointChanged && source.Credential != "" && !newCredential && !req.ClearCredential {
		return nil, common.Classify(common.ErrSecretSourceInvalid,
			errors.New("the address or login method changed; enter "+credentialLabelInternal(source.Provider)+" again"))
	}
	if newCredential && providerAcceptsCredentialInternal(source.Provider) {
		encrypted, encryptErr := crypto.Encrypt(*req.Credential)
		if encryptErr != nil {
			return nil, fmt.Errorf("failed to encrypt credential: %w", encryptErr)
		}
		source.Credential = encrypted
	}
	setupSecret := ""
	if req.SetupCredential != nil {
		setupSecret = *req.SetupCredential
	}
	if endpointChanged && source.SetupCredential != "" && setupSecret == "" && setupClientIDInternal(source.Settings) != "" {
		return nil, common.Classify(common.ErrSecretSourceInvalid, errors.New("the address changed; enter the setup identity's secret again"))
	}
	if setupErr := applySetupCredentialInternal(source, beforeSetupClientID, setupSecret); setupErr != nil {
		return nil, setupErr
	}
	// A test result only describes the connection it was run against.
	if connectionFingerprintInternal(source) != beforeFingerprint {
		source.LastTestedAt = nil
		source.LastTestError = nil
	}

	if saveErr := s.db.WithContext(ctx).Save(source).Error; saveErr != nil {
		return nil, fmt.Errorf("failed to update secret source: %w", saveErr)
	}
	s.forgetProviderInternal(source.ID)
	return s.GetSource(ctx, source.ID)
}

func (s *SecretSourceService) DeleteSource(ctx context.Context, id string) error {
	source, err := s.loadSourceInternal(ctx, id)
	if err != nil {
		return err
	}
	var bound int64
	if countErr := s.db.WithContext(ctx).Model(&ProjectSecretBinding{}).Where("source_id = ?", source.ID).Count(&bound).Error; countErr != nil {
		return fmt.Errorf("failed to check secret source usage: %w", countErr)
	}
	if bound > 0 {
		return common.ErrSecretSourceInUse
	}
	if deleteErr := s.db.WithContext(ctx).Delete(&SecretSource{}, "id = ?", source.ID).Error; deleteErr != nil {
		return fmt.Errorf("failed to delete secret source: %w", deleteErr)
	}
	s.forgetProviderInternal(source.ID)
	return nil
}

// TestSource tests settings without saving them. The result is recorded on
// the source only when the tested settings and credential are the stored
// ones, so an unsaved edit never overwrites the saved status.
func (s *SecretSourceService) TestSource(ctx context.Context, req types.SecretSourceTestRequest) (types.SecretSourceTestResult, error) {
	providerName := req.Provider
	credential := req.Credential
	var stored *SecretSource
	if req.SourceID != "" {
		source, err := s.loadSourceInternal(ctx, req.SourceID)
		if err != nil {
			return types.SecretSourceTestResult{}, err
		}
		stored = source
		providerName = source.Provider
	}

	settings, err := normalizeSettingsInternal(providerName, req.Settings)
	if err != nil {
		return types.SecretSourceTestResult{OK: false, Message: err.Error()}, nil
	}

	recordOn := ""
	if stored != nil {
		storedCredential, decryptErr := decryptCredentialInternal(stored.Credential)
		if decryptErr != nil {
			return types.SecretSourceTestResult{}, decryptErr
		}
		// The stored credential is only sent to the address it was saved for.
		if credential == "" && endpointKeyInternal(settings) == endpointKeyInternal(stored.Settings) {
			credential = storedCredential
		}
		candidate := *stored
		candidate.Settings = settings
		if credential == storedCredential && connectionFingerprintInternal(&candidate) == connectionFingerprintInternal(stored) {
			recordOn = stored.ID
		}
	}

	provider, err := s.newProviderInternal(providerName, settings, credential)
	if err != nil {
		return types.SecretSourceTestResult{OK: false, Message: err.Error()}, nil
	}
	testCtx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	result := provider.Test(testCtx)
	if recordOn != "" {
		s.recordTestInternal(ctx, recordOn, result)
	}
	return result, nil
}

func (s *SecretSourceService) recordTestInternal(ctx context.Context, sourceID string, result types.SecretSourceTestResult) {
	var testError *string
	if !result.OK {
		testError = new(result.Message)
	}
	err := s.db.WithContext(ctx).Model(&SecretSource{}).Where("id = ?", sourceID).
		UpdateColumns(map[string]any{"last_tested_at": time.Now(), "last_test_error": testError}).Error
	if err != nil {
		slog.WarnContext(ctx, "failed to record secret source test result", "sourceId", sourceID, "error", err)
	}
}

// Browse lists pickable targets of a source, such as Infisical projects or
// Bitwarden folders.
func (s *SecretSourceService) Browse(ctx context.Context, sourceID string, query types.SecretSourceBrowseQuery) ([]types.SecretSourceBrowseItem, error) {
	source, err := s.loadSourceInternal(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	provider, err := s.providerForSourceInternal(source)
	if err != nil {
		return nil, err
	}
	browseCtx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	items, err := provider.Browse(browseCtx, query)
	if err != nil {
		return nil, common.Classify(common.ErrSecretFetchFailed, err)
	}
	return items, nil
}

// fetchInternal reads the binding's secrets, drops names that are not valid
// environment variable names, records the outcome on the binding, and logs an
// event without any secret values. With requireKeys, an empty result is a
// failure: a required binding that delivers nothing is almost always pointed at
// the wrong target.
func (s *SecretSourceService) fetchInternal(
	ctx context.Context,
	binding *ProjectSecretBinding,
	project ProjectRef,
	actor types.Actor,
	opts fetchOptions,
) (values map[string]string, skipped []string, err error) {
	provider, err := s.providerForSourceInternal(binding.Source)
	if err == nil {
		fetchCtx, cancel := context.WithTimeout(ctx, providerTimeout)
		values, skipped, err = provider.Fetch(fetchCtx, binding.Target)
		cancel()
	}

	if err == nil {
		for key := range values {
			if !envKeyPattern.MatchString(key) {
				skipped = append(skipped, key)
				delete(values, key)
			}
		}
		slices.Sort(skipped)
		if opts.requireKeys && len(values) == 0 {
			err = fmt.Errorf("the source returned no usable secrets for %s; check the binding or turn off Required", describeTargetInternal(binding))
		}
	}

	metadata := database.JSON{
		"reason":   opts.reason,
		"sourceId": binding.SourceID,
		"target":   describeTargetInternal(binding),
	}
	if binding.Source != nil {
		metadata["sourceName"] = binding.Source.Name
		metadata["provider"] = binding.Source.Provider
	}

	now := time.Now()
	if err != nil {
		message := err.Error()
		repeated := binding.LastFetchError != nil && *binding.LastFetchError == message
		s.updateFetchStatusInternal(ctx, binding.ID, now, &message)
		if !opts.quiet || !repeated {
			metadata["error"] = message
			s.logEventInternal(ctx, event.EventTypeProjectSecretsError, project, actor, metadata)
		}
		return nil, nil, common.Classify(common.ErrSecretFetchFailed, fmt.Errorf("failed to fetch secrets for project %q: %w", project.Name, err))
	}

	s.updateFetchStatusInternal(ctx, binding.ID, now, nil)
	if !opts.quiet {
		metadata["keyCount"] = len(values)
		metadata["skippedKeyCount"] = len(skipped)
		s.logEventInternal(ctx, event.EventTypeProjectSecretsFetch, project, actor, metadata)
	}
	return values, skipped, nil
}

// updateFetchStatusInternal records the outcome of a fetch. last_fetched_at
// only moves on success, so it always means "values last read at".
func (s *SecretSourceService) updateFetchStatusInternal(ctx context.Context, bindingID string, at time.Time, fetchError *string) {
	columns := map[string]any{"last_fetch_error": fetchError}
	if fetchError == nil {
		columns["last_fetched_at"] = at
	}
	err := s.db.WithContext(ctx).Model(&ProjectSecretBinding{}).Where("id = ?", bindingID).
		UpdateColumns(columns).Error
	if err != nil {
		slog.WarnContext(ctx, "failed to record secret fetch status", "bindingId", bindingID, "error", err)
	}
}

func (s *SecretSourceService) recordSeenInternal(ctx context.Context, bindingID, hash string) {
	err := s.db.WithContext(ctx).Model(&ProjectSecretBinding{}).Where("id = ?", bindingID).
		UpdateColumns(map[string]any{"last_seen_hash": hash, "last_checked_at": time.Now()}).Error
	if err != nil {
		slog.WarnContext(ctx, "failed to record checked secret hash", "bindingId", bindingID, "error", err)
	}
}

func (s *SecretSourceService) recordNotifiedInternal(ctx context.Context, bindingID, hash string) {
	err := s.db.WithContext(ctx).Model(&ProjectSecretBinding{}).Where("id = ?", bindingID).
		UpdateColumn("last_notified_hash", hash).Error
	if err != nil {
		slog.WarnContext(ctx, "failed to record notified secret hash", "bindingId", bindingID, "error", err)
	}
}

func (s *SecretSourceService) logEventInternal(ctx context.Context, eventType event.EventType, project ProjectRef, actor types.Actor, metadata database.JSON) {
	if s.eventService == nil {
		return
	}
	if err := s.eventService.LogResourceEvent(ctx, event.ResourceEvent{
		Type:          eventType,
		ResourceType:  "project",
		ResourceID:    project.ID,
		ResourceName:  project.Name,
		UserID:        actor.ID,
		Username:      actor.Username,
		EnvironmentID: localEnvironmentID,
		Metadata:      metadata,
	}); err != nil {
		slog.WarnContext(ctx, "failed to log project secrets event", "projectId", project.ID, "error", err)
	}
}

// ---- Providers ----

// providerForSourceInternal reuses one provider per source so per-provider
// state, such as Infisical's access token, survives across deploys. Editing
// the source invalidates it.
func (s *SecretSourceService) providerForSourceInternal(source *SecretSource) (secretProvider, error) {
	if source == nil {
		return nil, common.ErrSecretSourceNotFound
	}
	fingerprint := connectionFingerprintInternal(source)

	s.providersMu.Lock()
	defer s.providersMu.Unlock()
	if cached, ok := s.providers[source.ID]; ok && cached.fingerprint == fingerprint {
		return cached.provider, nil
	}
	credential, err := decryptCredentialInternal(source.Credential)
	if err != nil {
		return nil, err
	}
	provider, err := s.newProviderInternal(source.Provider, source.Settings, credential)
	if err != nil {
		return nil, err
	}
	s.providers[source.ID] = cachedProvider{fingerprint: fingerprint, provider: provider}
	return provider, nil
}

func (s *SecretSourceService) newProviderInternal(providerName string, settings types.SecretSourceSettings, credential string) (secretProvider, error) {
	switch providerName {
	case types.SecretSourceProviderInfisical:
		if settings.Infisical == nil {
			return nil, common.Classify(common.ErrSecretSourceInvalid, errors.New("infisical settings are required"))
		}
		return infisical.New(s.httpClient, *settings.Infisical, credential)
	case types.SecretSourceProviderBitwarden:
		if settings.Bitwarden == nil {
			return nil, common.Classify(common.ErrSecretSourceInvalid, errors.New("bitwarden settings are required"))
		}
		return bitwarden.New(s.httpClient, *settings.Bitwarden)
	case types.SecretSourceProviderVault:
		if settings.Vault == nil {
			return nil, common.Classify(common.ErrSecretSourceInvalid, errors.New("vault settings are required"))
		}
		return vault.New(s.httpClient, *settings.Vault, credential)
	case types.SecretSourceProviderDoppler:
		if settings.Doppler == nil {
			return nil, common.Classify(common.ErrSecretSourceInvalid, errors.New("doppler settings are required"))
		}
		return doppler.New(s.httpClient, *settings.Doppler, credential)
	case types.SecretSourceProviderOnePassword:
		if settings.OnePassword == nil {
			return nil, common.Classify(common.ErrSecretSourceInvalid, errors.New("1Password settings are required"))
		}
		return onepassword.New(s.httpClient, *settings.OnePassword, credential)
	case types.SecretSourceProviderProtonPass:
		if settings.ProtonPass == nil {
			return nil, common.Classify(common.ErrSecretSourceInvalid, errors.New("settings for Proton Pass are required"))
		}
		return protonpass.New(s.httpClient, *settings.ProtonPass, credential)
	case types.SecretSourceProviderHTTP:
		if settings.HTTP == nil {
			return nil, common.Classify(common.ErrSecretSourceInvalid, errors.New("endpoint settings are required"))
		}
		return httpsource.New(s.httpClient, *settings.HTTP, credential)
	default:
		return nil, common.Classify(common.ErrSecretSourceInvalid, fmt.Errorf("unknown provider %q", providerName))
	}
}

func (s *SecretSourceService) forgetProviderInternal(sourceID string) {
	s.providersMu.Lock()
	defer s.providersMu.Unlock()
	delete(s.providers, sourceID)
}

// ---- Persistence helpers ----

func (s *SecretSourceService) loadSourceInternal(ctx context.Context, id string) (*SecretSource, error) {
	if strings.TrimSpace(id) == "" {
		return nil, common.ErrSecretSourceNotFound
	}
	var source SecretSource
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&source).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, common.ErrSecretSourceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load secret source: %w", err)
	}
	return &source, nil
}

func (s *SecretSourceService) bindingCountsInternal(ctx context.Context) (map[string]int, error) {
	var rows []struct {
		SourceID string
		Count    int
	}
	err := s.db.WithContext(ctx).Model(&ProjectSecretBinding{}).
		Select("source_id, COUNT(*) AS count").Group("source_id").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to count secret bindings: %w", err)
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.SourceID] = row.Count
	}
	return counts, nil
}

func (s *SecretSourceService) ensureNameAvailableInternal(ctx context.Context, name, exceptID string) error {
	query := s.db.WithContext(ctx).Model(&SecretSource{}).Where("LOWER(name) = LOWER(?)", name)
	if exceptID != "" {
		query = query.Where("id <> ?", exceptID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return fmt.Errorf("failed to check secret source name: %w", err)
	}
	if count > 0 {
		return common.ErrSecretSourceConflict
	}
	return nil
}

// ---- Pure helpers ----

// providerNeedsCredentialInternal reports providers that cannot connect
// without a stored credential.
func providerNeedsCredentialInternal(providerName string) bool {
	switch providerName {
	case types.SecretSourceProviderInfisical, types.SecretSourceProviderVault,
		types.SecretSourceProviderDoppler, types.SecretSourceProviderOnePassword:
		return true
	default:
		return false
	}
}

// providerAcceptsCredentialInternal reports providers that store a
// credential: the ones that need one, and HTTP, where a bearer token is
// optional, and Proton Pass, whose kit may require one. Bitwarden's session
// lives in bw serve.
func providerAcceptsCredentialInternal(providerName string) bool {
	return providerNeedsCredentialInternal(providerName) ||
		providerName == types.SecretSourceProviderHTTP || providerName == types.SecretSourceProviderProtonPass
}

// endpointKeyInternal identifies where a source's credential is sent and how
// it is used: the server address and, for Vault, the login method.
func endpointKeyInternal(settings types.SecretSourceSettings) string {
	switch {
	case settings.Infisical != nil:
		return "infisical\x00" + settings.Infisical.SiteURL
	case settings.Vault != nil:
		v := settings.Vault
		return "vault\x00" + v.Address + "\x00" + v.AuthMethod + "\x00" + v.RoleID + "\x00" + v.AppRoleMount
	case settings.Doppler != nil:
		return "doppler\x00" + settings.Doppler.APIURL
	case settings.OnePassword != nil:
		return "onepassword\x00" + settings.OnePassword.ServerURL
	case settings.ProtonPass != nil:
		return "protonpass\x00" + settings.ProtonPass.KitURL
	case settings.HTTP != nil:
		return "http\x00" + settings.HTTP.BaseURL
	case settings.Bitwarden != nil:
		return "bitwarden\x00" + settings.Bitwarden.ServeURL
	default:
		return ""
	}
}

func credentialLabelInternal(providerName string) string {
	switch providerName {
	case types.SecretSourceProviderInfisical:
		return "a client secret"
	case types.SecretSourceProviderVault:
		return "a token or AppRole secret ID"
	case types.SecretSourceProviderDoppler:
		return "a Doppler token"
	case types.SecretSourceProviderOnePassword:
		return "a Connect token"
	default:
		return "a credential"
	}
}

// normalizeSettingsInternal validates the settings of one provider and drops
// any other provider's settings.
func normalizeSettingsInternal(providerName string, settings types.SecretSourceSettings) (types.SecretSourceSettings, error) {
	var err error
	var normalized types.SecretSourceSettings
	switch providerName {
	case types.SecretSourceProviderInfisical:
		normalized.Infisical, err = infisical.NormalizeSettings(settings.Infisical)
	case types.SecretSourceProviderBitwarden:
		normalized.Bitwarden, err = bitwarden.NormalizeSettings(settings.Bitwarden)
	case types.SecretSourceProviderVault:
		normalized.Vault, err = vault.NormalizeSettings(settings.Vault)
	case types.SecretSourceProviderDoppler:
		normalized.Doppler, err = doppler.NormalizeSettings(settings.Doppler)
	case types.SecretSourceProviderOnePassword:
		normalized.OnePassword, err = onepassword.NormalizeSettings(settings.OnePassword)
	case types.SecretSourceProviderProtonPass:
		normalized.ProtonPass, err = protonpass.NormalizeSettings(settings.ProtonPass)
	case types.SecretSourceProviderHTTP:
		normalized.HTTP, err = httpsource.NormalizeSettings(settings.HTTP)
	default:
		err = fmt.Errorf("unknown provider %q; use one of %s", providerName, strings.Join(types.SecretSourceProviders, ", "))
	}
	if err != nil {
		return types.SecretSourceSettings{}, common.Classify(common.ErrSecretSourceInvalid, err)
	}
	return normalized, nil
}

// normalizeTargetInternal validates the target for the source's provider and
// drops any other provider's target.
func normalizeTargetInternal(providerName string, target types.SecretSourceBindingTarget) (types.SecretSourceBindingTarget, error) {
	var err error
	var normalized types.SecretSourceBindingTarget
	switch providerName {
	case types.SecretSourceProviderInfisical:
		normalized.Infisical, err = infisical.NormalizeTarget(target.Infisical)
	case types.SecretSourceProviderBitwarden:
		normalized.Bitwarden, err = bitwarden.NormalizeTarget(target.Bitwarden)
	case types.SecretSourceProviderVault:
		normalized.Vault, err = vault.NormalizeTarget(target.Vault)
	case types.SecretSourceProviderDoppler:
		normalized.Doppler, err = doppler.NormalizeTarget(target.Doppler)
	case types.SecretSourceProviderOnePassword:
		normalized.OnePassword, err = onepassword.NormalizeTarget(target.OnePassword)
	case types.SecretSourceProviderProtonPass:
		normalized.ProtonPass, err = protonpass.NormalizeTarget(target.ProtonPass)
	case types.SecretSourceProviderHTTP:
		normalized.HTTP, err = httpsource.NormalizeTarget(target.HTTP)
	default:
		err = fmt.Errorf("unknown provider %q", providerName)
	}
	if err != nil {
		return types.SecretSourceBindingTarget{}, common.Classify(common.ErrSecretBindingInvalid, err)
	}
	return normalized, nil
}

func describeTargetInternal(binding *ProjectSecretBinding) string {
	switch {
	case binding.Target.Infisical != nil:
		return infisical.Describe(binding.Target.Infisical)
	case binding.Target.Bitwarden != nil:
		return bitwarden.Describe(binding.Target.Bitwarden)
	case binding.Target.Vault != nil:
		return vault.Describe(binding.Target.Vault)
	case binding.Target.Doppler != nil:
		return doppler.Describe(binding.Target.Doppler)
	case binding.Target.OnePassword != nil:
		return onepassword.Describe(binding.Target.OnePassword)
	case binding.Target.ProtonPass != nil:
		return protonpass.Describe(binding.Target.ProtonPass)
	case binding.Target.HTTP != nil:
		return httpsource.Describe(binding.Target.HTTP)
	default:
		return "an unknown target"
	}
}

func sameTargetInternal(a, b types.SecretSourceBindingTarget) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && string(left) == string(right)
}

// setupClientIDInternal names the setup identity a stored setup credential
// belongs to, or "" when the source has none: the Infisical setup client ID,
// or a fixed marker for a Vault/OpenBao setup token.
func setupClientIDInternal(settings types.SecretSourceSettings) string {
	switch {
	case settings.Infisical != nil:
		return settings.Infisical.SetupClientID
	case settings.Vault != nil && settings.Vault.SetupToken:
		return "vault-setup-token"
	default:
		return ""
	}
}

// applySetupCredentialInternal keeps the setup credential consistent with the
// setup client ID: no ID means no credential, and a new ID needs its secret.
// An empty secret keeps the stored one for an unchanged ID.
func applySetupCredentialInternal(source *SecretSource, previousClientID, secret string) error {
	clientID := setupClientIDInternal(source.Settings)
	if clientID == "" {
		source.SetupCredential = ""
		return nil
	}
	if secret == "" {
		if source.SetupCredential == "" || clientID != previousClientID {
			return common.Classify(common.ErrSecretSourceInvalid, errors.New("enter the setup identity's secret"))
		}
		return nil
	}
	encrypted, err := crypto.Encrypt(secret)
	if err != nil {
		return fmt.Errorf("failed to encrypt setup credential: %w", err)
	}
	source.SetupCredential = encrypted
	return nil
}

// connectionFingerprintInternal identifies everything a deploy connection
// depends on: provider, settings, and the stored credential. The setup
// identity is left out because deploys never use it.
func connectionFingerprintInternal(source *SecretSource) string {
	fingerprintSettings := source.Settings
	if fingerprintSettings.Infisical != nil {
		deployOnly := *fingerprintSettings.Infisical
		deployOnly.SetupClientID = ""
		fingerprintSettings.Infisical = &deployOnly
	}
	if fingerprintSettings.Vault != nil {
		deployOnly := *fingerprintSettings.Vault
		deployOnly.SetupToken = false
		fingerprintSettings.Vault = &deployOnly
	}
	settings, err := json.Marshal(fingerprintSettings)
	if err != nil {
		settings = nil
	}
	return source.Provider + "\x00" + string(settings) + "\x00" + source.Credential
}

func decryptCredentialInternal(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	plain, err := crypto.Decrypt(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt stored credential: %w", err)
	}
	return plain, nil
}

func newHashSaltInternal() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate hash salt: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// hashValuesInternal returns a salted HMAC over the sorted key/value set, so
// two deploys can be compared without storing anything derived from a single
// secret in a guessable way.
func hashValuesInternal(salt string, values map[string]string) string {
	mac := hmac.New(sha256.New, []byte(salt))
	for _, key := range sortedKeysInternal(values) {
		_, _ = mac.Write([]byte(key))
		_, _ = mac.Write([]byte{0})
		_, _ = mac.Write([]byte(values[key]))
		_, _ = mac.Write([]byte{0})
	}
	return hex.EncodeToString(mac.Sum(nil))
}

func sortedKeysInternal(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// overriddenKeysInternal lists secret keys that are also set in the project's
// .env or the environment's .env.global. Read failures only hide the warning.
func overriddenKeysInternal(ctx context.Context, project ProjectRef, keys []string) []string {
	defined := make(map[string]struct{})
	paths := make([]string, 0, 2)
	if project.ProjectsDirectory != "" {
		paths = append(paths, filepath.Join(project.ProjectsDirectory, projects.GlobalEnvFileName))
	}
	if project.Path != "" {
		paths = append(paths, filepath.Join(project.Path, projects.EffectiveEnvFileName))
	}
	for _, path := range paths {
		values, err := projects.ParseProjectEnvFile(path, nil)
		if err != nil {
			slog.DebugContext(ctx, "could not read env file for override check", "path", path, "error", err)
			continue
		}
		// KEY=${KEY} passes the secret on rather than setting its own value.
		var placeholders map[string]struct{}
		if content, readErr := os.ReadFile(path); readErr == nil {
			placeholders = setupplan.SelfReferencedKeys(string(content))
		}
		for key := range values {
			if _, placeholder := placeholders[key]; !placeholder {
				defined[key] = struct{}{}
			}
		}
	}

	overridden := make([]string, 0)
	for _, key := range keys {
		if _, ok := defined[key]; ok {
			overridden = append(overridden, key)
		}
	}
	return overridden
}

func sourceToDTOInternal(source *SecretSource, bindingCount int) types.SecretSource {
	return types.SecretSource{
		ID:                 source.ID,
		Name:               source.Name,
		Provider:           source.Provider,
		Settings:           source.Settings,
		HasCredential:      source.Credential != "",
		HasSetupCredential: source.SetupCredential != "",
		BindingCount:       bindingCount,
		LastTestedAt:       source.LastTestedAt,
		LastTestError:      source.LastTestError,
		CreatedAt:          source.CreatedAt,
		UpdatedAt:          source.UpdatedAt,
	}
}
