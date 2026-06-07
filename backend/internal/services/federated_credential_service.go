package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	pkgutils "github.com/getarcaneapp/arcane/backend/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/dbutil"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/jwtclaims"
	federatedtypes "github.com/getarcaneapp/arcane/types/federated"
)

const (
	federatedCredentialLastUsedWriteWindow = 5 * time.Minute
	defaultFederatedSubjectClaim           = "sub"
)

type FederatedCredentialService struct {
	db              *database.DB
	authService     *AuthService
	userService     *UserService
	settingsService *SettingsService
	eventService    *EventService
	roleService     *RoleService
	httpClient      *http.Client
	providerMu      sync.RWMutex
	providers       map[string]*oidc.Provider
	providerGroup   singleflight.Group
}

func NewFederatedCredentialService(
	db *database.DB,
	authService *AuthService,
	userService *UserService,
	settingsService *SettingsService,
	eventService *EventService,
	httpClient *http.Client,
) *FederatedCredentialService {
	if httpClient == nil {
		httpClient = httpx.NewHTTPClientWithTimeout(15 * time.Second)
	}

	return &FederatedCredentialService{
		db:              db,
		authService:     authService,
		userService:     userService,
		settingsService: settingsService,
		eventService:    eventService,
		httpClient:      httpClient,
		providers:       make(map[string]*oidc.Provider),
	}
}

func (s *FederatedCredentialService) WithRoleService(roleService *RoleService) *FederatedCredentialService {
	s.roleService = roleService
	return s
}

func (s *FederatedCredentialService) ExchangeToken(ctx context.Context, req federatedtypes.TokenExchangeRequest) (*federatedtypes.FederatedTokenResponse, error) {
	issuer, subject, audiences := unverifiedTokenExchangeMetadataInternal(req.SubjectToken)
	logResult := "failure"
	logReason := ""
	var matchedCredential *models.FederatedCredential
	var matchedUser *models.User
	defer func() {
		s.logExchangeInternal(ctx, logResult, logReason, issuer, subject, audiences, matchedCredential, matchedUser)
	}()

	if err := validateTokenExchangeRequestInternal(req); err != nil {
		logReason = "invalid_request"
		return nil, err
	}
	if issuer == "" {
		logReason = "missing_issuer"
		return nil, &common.FederatedCredentialInvalidGrantError{}
	}

	credentials, err := s.listEnabledCredentialsForIssuerInternal(ctx, issuer)
	if err != nil {
		logReason = "credential_lookup_failed"
		return nil, err
	}
	if len(credentials) == 0 {
		logReason = "issuer_not_allowed"
		return nil, &common.FederatedCredentialInvalidGrantError{}
	}

	verifiedToken, verifiedClaims, err := s.verifySubjectTokenInternal(ctx, issuer, req.SubjectToken)
	if err != nil {
		logReason = "token_verification_failed"
		return nil, fmt.Errorf("%w: %w", &common.FederatedCredentialInvalidGrantError{}, err)
	}
	if subject == "" {
		subject = stringClaimByPathInternal(verifiedClaims, defaultFederatedSubjectClaim)
	}
	if len(audiences) == 0 {
		audiences = append([]string{}, verifiedToken.Audience...)
	}

	credential := selectMatchingCredentialInternal(credentials, verifiedToken.Audience, verifiedClaims)
	if credential == nil {
		logReason = "no_matching_credential"
		return nil, &common.FederatedCredentialInvalidGrantError{}
	}
	matchedCredential = credential
	if err := s.recordTokenReplayGuardInternal(ctx, issuer, req.SubjectToken, verifiedClaims, verifiedToken.Expiry); err != nil {
		logReason = "token_replay_rejected"
		return nil, err
	}

	user, err := s.userService.GetUserByID(ctx, credential.IdentityUserID)
	if err != nil {
		logReason = "identity_user_missing"
		return nil, fmt.Errorf("%w: %w", &common.FederatedCredentialInvalidGrantError{}, err)
	}
	matchedUser = user

	tokenPair, err := s.authService.IssueFederatedToken(ctx, user, credential.ID, credential.TokenTTLSeconds)
	if err != nil {
		logReason = "token_issue_failed"
		return nil, err
	}

	s.markCredentialUsedAsyncInternal(ctx, credential.ID)
	logResult = "success"
	logReason = "matched"

	return &federatedtypes.FederatedTokenResponse{
		AccessToken:     tokenPair.AccessToken,
		TokenType:       "Bearer",
		ExpiresIn:       max(int(time.Until(tokenPair.ExpiresAt).Seconds()), 0),
		IssuedTokenType: federatedtypes.IssuedTokenTypeAccessToken,
	}, nil
}

func (s *FederatedCredentialService) Create(ctx context.Context, callerUserID string, req federatedtypes.CreateFederatedCredential) (*federatedtypes.FederatedCredential, error) {
	normalized, err := normalizeCreateFederatedCredentialInternal(req)
	if err != nil {
		return nil, err
	}
	if err := s.validateRoleGrantAgainstUserInternal(ctx, callerUserID, normalized.RoleID, normalized.EnvironmentID); err != nil {
		return nil, err
	}

	var created models.FederatedCredential
	err = dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		serviceUser := models.User{
			Username:         "svc_federated_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			DisplayName:      pkgutils.StringPtrFromTrimmed("Federated: " + normalized.Name),
			IsServiceAccount: true,
		}
		if err := tx.Create(&serviceUser).Error; err != nil {
			return fmt.Errorf("failed to create federated service user: %w", err)
		}

		created = models.FederatedCredential{
			Name:            normalized.Name,
			Description:     normalized.Description,
			Enabled:         normalized.Enabled,
			IssuerURL:       normalized.IssuerURL,
			Audiences:       models.StringSlice(normalized.Audiences),
			SubjectClaim:    normalized.SubjectClaim,
			SubjectMatch:    normalized.SubjectMatch,
			MatchType:       normalized.MatchType,
			RoleID:          normalized.RoleID,
			EnvironmentID:   normalized.EnvironmentID,
			IdentityUserID:  serviceUser.ID,
			TokenTTLSeconds: normalized.TokenTTLSeconds,
			ExpiresAt:       normalized.ExpiresAt,
		}
		if err := tx.Create(&created).Error; err != nil {
			return fmt.Errorf("failed to create federated credential: %w", err)
		}

		assignment := models.UserRoleAssignment{
			UserID:        serviceUser.ID,
			RoleID:        normalized.RoleID,
			EnvironmentID: normalized.EnvironmentID,
			Source:        models.RoleAssignmentSourceManual,
		}
		if err := tx.Create(&assignment).Error; err != nil {
			return fmt.Errorf("failed to create federated role assignment: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.roleService != nil {
		s.roleService.InvalidateUser(created.IdentityUserID)
	}

	reloaded, err := s.Get(ctx, created.ID)
	if err != nil {
		return nil, err
	}
	return reloaded, nil
}

func (s *FederatedCredentialService) List(ctx context.Context, params pagination.QueryParams) ([]federatedtypes.FederatedCredential, pagination.Response, error) {
	var credentials []models.FederatedCredential
	query := s.db.WithContext(ctx).
		Model(&models.FederatedCredential{}).
		Preload("IdentityUser").
		Preload("Role").
		Preload("Environment")

	if term := strings.TrimSpace(params.Search); term != "" {
		pattern := "%" + term + "%"
		query = query.Where("name LIKE ? OR COALESCE(description, '') LIKE ? OR issuer_url LIKE ? OR subject_match LIKE ?", pattern, pattern, pattern, pattern)
	}

	resp, err := pagination.PaginateAndSortDB(params, query, &credentials)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate federated credentials: %w", err)
	}

	result := make([]federatedtypes.FederatedCredential, len(credentials))
	for i := range credentials {
		result[i] = toFederatedCredentialDTOInternal(&credentials[i])
	}
	return result, resp, nil
}

func (s *FederatedCredentialService) Get(ctx context.Context, id string) (*federatedtypes.FederatedCredential, error) {
	var credential models.FederatedCredential
	if err := s.db.WithContext(ctx).
		Preload("IdentityUser").
		Preload("Role").
		Preload("Environment").
		Where("id = ?", id).
		First(&credential).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &common.FederatedCredentialNotFoundError{}
		}
		return nil, fmt.Errorf("failed to get federated credential: %w", err)
	}
	dto := toFederatedCredentialDTOInternal(&credential)
	return &dto, nil
}

func (s *FederatedCredentialService) Update(ctx context.Context, callerUserID string, id string, req federatedtypes.UpdateFederatedCredential) (*federatedtypes.FederatedCredential, error) {
	var credential models.FederatedCredential
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&credential).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &common.FederatedCredentialNotFoundError{}
		}
		return nil, fmt.Errorf("failed to load federated credential: %w", err)
	}

	updated, roleChanged, err := applyFederatedCredentialUpdateInternal(credential, req)
	if err != nil {
		return nil, err
	}
	revokeActiveSessions := credential.Enabled && !updated.Enabled
	if roleChanged {
		if err := s.validateRoleGrantAgainstUserInternal(ctx, callerUserID, updated.RoleID, updated.EnvironmentID); err != nil {
			return nil, err
		}
	}

	err = dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := tx.Save(&updated).Error; err != nil {
			return fmt.Errorf("failed to update federated credential: %w", err)
		}
		if revokeActiveSessions {
			if err := revokeFederatedCredentialSessionsInternal(tx, updated.ID); err != nil {
				return err
			}
		}
		if roleChanged {
			if err := tx.Where("user_id = ? AND source = ?", updated.IdentityUserID, models.RoleAssignmentSourceManual).
				Delete(&models.UserRoleAssignment{}).Error; err != nil {
				return fmt.Errorf("failed to clear federated role assignment: %w", err)
			}
			assignment := models.UserRoleAssignment{
				UserID:        updated.IdentityUserID,
				RoleID:        updated.RoleID,
				EnvironmentID: updated.EnvironmentID,
				Source:        models.RoleAssignmentSourceManual,
			}
			if err := tx.Create(&assignment).Error; err != nil {
				return fmt.Errorf("failed to update federated role assignment: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if roleChanged && s.roleService != nil {
		s.roleService.InvalidateUser(updated.IdentityUserID)
	}

	return s.Get(ctx, id)
}

func (s *FederatedCredentialService) Delete(ctx context.Context, id string) error {
	var credential models.FederatedCredential
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&credential).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &common.FederatedCredentialNotFoundError{}
		}
		return fmt.Errorf("failed to load federated credential: %w", err)
	}

	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := tx.Delete(&models.FederatedCredential{}, "id = ?", credential.ID).Error; err != nil {
			return fmt.Errorf("failed to delete federated credential: %w", err)
		}
		if err := tx.Delete(&models.User{}, "id = ?", credential.IdentityUserID).Error; err != nil {
			return fmt.Errorf("failed to delete federated service user: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if s.roleService != nil {
		s.roleService.InvalidateUser(credential.IdentityUserID)
	}
	return nil
}

func validateTokenExchangeRequestInternal(req federatedtypes.TokenExchangeRequest) error {
	if req.GrantType != federatedtypes.TokenExchangeGrantType {
		return &common.FederatedCredentialInvalidRequestError{}
	}
	if strings.TrimSpace(req.SubjectToken) == "" {
		return &common.FederatedCredentialInvalidRequestError{}
	}
	switch req.SubjectTokenType {
	case federatedtypes.SubjectTokenTypeJWT, federatedtypes.SubjectTokenTypeIDToken:
	default:
		return &common.FederatedCredentialInvalidRequestError{}
	}
	if req.RequestedTokenType != "" && req.RequestedTokenType != federatedtypes.RequestedTokenTypeAccessJWT {
		return &common.FederatedCredentialInvalidRequestError{}
	}
	return nil
}

func (s *FederatedCredentialService) listEnabledCredentialsForIssuerInternal(ctx context.Context, issuer string) ([]models.FederatedCredential, error) {
	var credentials []models.FederatedCredential
	if err := s.db.WithContext(ctx).
		Where("issuer_url = ? AND enabled = ?", issuer, true).
		Order("created_at ASC").
		Order("id ASC").
		Find(&credentials).Error; err != nil {
		return nil, fmt.Errorf("failed to list federated credentials for issuer: %w", err)
	}

	now := time.Now()
	active := credentials[:0]
	for _, credential := range credentials {
		if credential.ExpiresAt != nil && now.After(*credential.ExpiresAt) {
			continue
		}
		active = append(active, credential)
	}
	return active, nil
}

func (s *FederatedCredentialService) verifySubjectTokenInternal(ctx context.Context, issuer string, rawToken string) (*oidc.IDToken, map[string]any, error) {
	provider, err := s.providerForIssuerInternal(ctx, issuer)
	if err != nil {
		return nil, nil, err
	}

	providerCtx := oidc.ClientContext(ctx, s.httpClient)
	verifier := provider.Verifier(&oidc.Config{SkipClientIDCheck: true})
	idToken, err := verifier.Verify(providerCtx, rawToken)
	if err != nil {
		return nil, nil, err
	}

	claims := map[string]any{}
	if err := idToken.Claims(&claims); err != nil {
		return nil, nil, err
	}
	return idToken, claims, nil
}

func (s *FederatedCredentialService) recordTokenReplayGuardInternal(ctx context.Context, issuer string, rawToken string, claims map[string]any, expiresAt time.Time) error {
	if expiresAt.IsZero() || time.Now().After(expiresAt) {
		return &common.FederatedCredentialInvalidGrantError{}
	}

	now := time.Now()
	if err := s.db.WithContext(ctx).
		Where("expires_at < ?", now).
		Delete(&models.FederatedTokenReplay{}).Error; err != nil {
		return fmt.Errorf("failed to prune federated token replay records: %w", err)
	}

	replay := models.FederatedTokenReplay{
		TokenHash: federatedTokenReplayHashInternal(issuer, rawToken, claims),
		IssuerURL: issuer,
		ExpiresAt: expiresAt,
	}
	if err := s.db.WithContext(ctx).Create(&replay).Error; err != nil {
		if isUniqueConstraintErrorInternal(err) {
			return &common.FederatedCredentialInvalidGrantError{}
		}
		return fmt.Errorf("failed to record federated token replay guard: %w", err)
	}
	return nil
}

func federatedTokenReplayHashInternal(issuer string, rawToken string, claims map[string]any) string {
	tokenID := strings.TrimSpace(stringClaimByPathInternal(claims, "jti"))
	tokenKind := "jti"
	if tokenID == "" {
		tokenID = rawToken
		tokenKind = "token"
	}

	sum := sha256.Sum256([]byte(issuer + "\x00" + tokenKind + "\x00" + tokenID))
	return hex.EncodeToString(sum[:])
}

func isUniqueConstraintErrorInternal(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate key")
}

func (s *FederatedCredentialService) providerForIssuerInternal(ctx context.Context, issuer string) (*oidc.Provider, error) {
	s.providerMu.RLock()
	if provider := s.providers[issuer]; provider != nil {
		s.providerMu.RUnlock()
		return provider, nil
	}
	s.providerMu.RUnlock()

	v, err, _ := s.providerGroup.Do(issuer, func() (any, error) {
		providerCtx := oidc.ClientContext(context.WithoutCancel(ctx), s.httpClient)
		provider, err := oidc.NewProvider(providerCtx, issuer)
		if err != nil {
			return nil, fmt.Errorf("failed to discover federated issuer: %w", err)
		}

		s.providerMu.Lock()
		s.providers[issuer] = provider
		s.providerMu.Unlock()
		return provider, nil
	})
	if err != nil {
		return nil, err
	}

	provider, ok := v.(*oidc.Provider)
	if !ok || provider == nil {
		return nil, errors.New("federated issuer discovery returned invalid provider")
	}
	return provider, nil
}

func selectMatchingCredentialInternal(credentials []models.FederatedCredential, tokenAudiences []string, claims map[string]any) *models.FederatedCredential {
	for i := range credentials {
		credential := &credentials[i]
		if !audienceMatchesInternal(tokenAudiences, []string(credential.Audiences)) {
			continue
		}
		subjectClaim := strings.TrimSpace(credential.SubjectClaim)
		if subjectClaim == "" {
			subjectClaim = defaultFederatedSubjectClaim
		}
		subject := stringClaimByPathInternal(claims, subjectClaim)
		if !subjectMatchesInternal(credential.MatchType, credential.SubjectMatch, subject) {
			continue
		}
		return credential
	}
	return nil
}

func audienceMatchesInternal(tokenAudiences, credentialAudiences []string) bool {
	allowed := make(map[string]struct{}, len(credentialAudiences))
	for _, audience := range credentialAudiences {
		audience = strings.TrimSpace(audience)
		if audience != "" {
			allowed[audience] = struct{}{}
		}
	}
	for _, audience := range tokenAudiences {
		if _, ok := allowed[audience]; ok {
			return true
		}
	}
	return false
}

func subjectMatchesInternal(matchType, pattern, subject string) bool {
	if subject == "" {
		return false
	}
	switch normalizeMatchTypeInternal(matchType) {
	case federatedtypes.MatchTypeGlob:
		return anchoredGlobMatchesInternal(pattern, subject)
	default:
		return subject == pattern
	}
}

func anchoredGlobMatchesInternal(pattern, value string) bool {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteByte('.')
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	matched, err := regexp.MatchString(b.String(), value)
	return err == nil && matched
}

func unverifiedTokenExchangeMetadataInternal(rawToken string) (string, string, []string) {
	claims := jwtclaims.ParseJWTClaims(rawToken)
	if claims == nil {
		return "", "", nil
	}
	return stringClaimByPathInternal(claims, "iss"), stringClaimByPathInternal(claims, "sub"), stringSliceClaimInternal(claims, "aud")
}

func stringClaimByPathInternal(claims map[string]any, path string) string {
	value, ok := jwtclaims.GetByPath(claims, path)
	if !ok {
		return ""
	}
	return pkgutils.ToString(value)
}

func stringSliceClaimInternal(claims map[string]any, path string) []string {
	value, ok := jwtclaims.GetByPath(claims, path)
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{strings.TrimSpace(typed)}
	case []string:
		return pkgutils.UniqueNonEmptyStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := pkgutils.ToString(item); value != "" {
				out = append(out, value)
			}
		}
		return pkgutils.UniqueNonEmptyStrings(out)
	default:
		return nil
	}
}

func normalizeCreateFederatedCredentialInternal(req federatedtypes.CreateFederatedCredential) (federatedtypes.CreateFederatedCredential, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.IssuerURL = strings.TrimRight(strings.TrimSpace(req.IssuerURL), "/")
	req.SubjectClaim = strings.TrimSpace(req.SubjectClaim)
	req.SubjectMatch = strings.TrimSpace(req.SubjectMatch)
	req.MatchType = normalizeMatchTypeInternal(req.MatchType)
	req.Audiences = pkgutils.UniqueNonEmptyStrings(req.Audiences)
	req.EnvironmentID = pkgutils.StringPtrFromTrimmed(pkgutils.DerefString(req.EnvironmentID))
	req.TokenTTLSeconds = clampFederatedTokenTTLSecondsInternal(req.TokenTTLSeconds)

	if req.SubjectClaim == "" {
		req.SubjectClaim = defaultFederatedSubjectClaim
	}
	if req.Name == "" || req.SubjectMatch == "" || req.RoleID == "" || len(req.Audiences) == 0 {
		return req, &common.FederatedCredentialInvalidError{}
	}
	if err := validateIssuerURLInternal(req.IssuerURL); err != nil {
		return req, err
	}
	if err := validateSubjectMatchInternal(req.MatchType, req.SubjectMatch); err != nil {
		return req, err
	}
	return req, nil
}

func applyFederatedCredentialUpdateInternal(existing models.FederatedCredential, req federatedtypes.UpdateFederatedCredential) (models.FederatedCredential, bool, error) {
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return existing, false, &common.FederatedCredentialInvalidError{}
		}
		existing.Name = name
	}
	if req.Description != nil {
		existing.Description = req.Description
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.IssuerURL != nil {
		issuerURL := strings.TrimRight(strings.TrimSpace(*req.IssuerURL), "/")
		if err := validateIssuerURLInternal(issuerURL); err != nil {
			return existing, false, err
		}
		existing.IssuerURL = issuerURL
	}
	if req.Audiences != nil {
		audiences := pkgutils.UniqueNonEmptyStrings(req.Audiences)
		if len(audiences) == 0 {
			return existing, false, &common.FederatedCredentialInvalidError{}
		}
		existing.Audiences = models.StringSlice(audiences)
	}
	if req.SubjectClaim != nil {
		subjectClaim := strings.TrimSpace(*req.SubjectClaim)
		if subjectClaim == "" {
			subjectClaim = defaultFederatedSubjectClaim
		}
		existing.SubjectClaim = subjectClaim
	}
	if req.SubjectMatch != nil {
		subjectMatch := strings.TrimSpace(*req.SubjectMatch)
		if subjectMatch == "" {
			return existing, false, &common.FederatedCredentialInvalidError{}
		}
		existing.SubjectMatch = subjectMatch
	}
	if req.MatchType != nil {
		existing.MatchType = normalizeMatchTypeInternal(*req.MatchType)
	}
	if err := validateSubjectMatchInternal(existing.MatchType, existing.SubjectMatch); err != nil {
		return existing, false, err
	}
	roleChanged, err := applyFederatedRoleScopeUpdateInternal(&existing, req.RoleID, req.EnvironmentID)
	if err != nil {
		return existing, false, err
	}
	if req.TokenTTLSeconds != nil {
		existing.TokenTTLSeconds = clampFederatedTokenTTLSecondsInternal(*req.TokenTTLSeconds)
	}
	if req.ExpiresAt != nil {
		existing.ExpiresAt = req.ExpiresAt
	}
	return existing, roleChanged, nil
}

func applyFederatedRoleScopeUpdateInternal(existing *models.FederatedCredential, roleID *string, environmentID *string) (bool, error) {
	if existing == nil {
		return false, &common.FederatedCredentialInvalidError{}
	}

	roleChanged := false
	if roleID != nil {
		normalizedRoleID := strings.TrimSpace(*roleID)
		if normalizedRoleID == "" {
			return false, &common.FederatedCredentialInvalidError{}
		}
		roleChanged = roleChanged || normalizedRoleID != existing.RoleID
		existing.RoleID = normalizedRoleID
	}
	if environmentID != nil {
		normalized := pkgutils.StringPtrFromTrimmed(*environmentID)
		roleChanged = roleChanged || pkgutils.DerefString(existing.EnvironmentID) != pkgutils.DerefString(normalized)
		existing.EnvironmentID = normalized
	}
	return roleChanged, nil
}

func normalizeMatchTypeInternal(matchType string) string {
	switch strings.ToLower(strings.TrimSpace(matchType)) {
	case federatedtypes.MatchTypeGlob:
		return federatedtypes.MatchTypeGlob
	default:
		return federatedtypes.MatchTypeExact
	}
}

func validateIssuerURLInternal(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed == nil || parsed.Host == "" || parsed.Scheme != "https" {
		return fmt.Errorf("%w: issuerUrl must be an HTTPS URL", &common.FederatedCredentialInvalidError{})
	}
	return nil
}

func validateSubjectMatchInternal(matchType, subjectMatch string) error {
	if strings.TrimSpace(subjectMatch) == "" {
		return &common.FederatedCredentialInvalidError{}
	}
	if normalizeMatchTypeInternal(matchType) == federatedtypes.MatchTypeGlob && strings.TrimSpace(subjectMatch) == "*" {
		return &common.FederatedCredentialInvalidError{}
	}
	return nil
}

func (s *FederatedCredentialService) validateRoleGrantAgainstUserInternal(ctx context.Context, userID, roleID string, environmentID *string) error {
	if s.roleService == nil || strings.TrimSpace(userID) == "" {
		return nil
	}

	user, err := s.userService.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("load user for federated role validation: %w", err)
	}

	ps, err := s.roleService.ResolvePermissions(ctx, user)
	if err != nil {
		return fmt.Errorf("resolve user permissions: %w", err)
	}

	if err := s.roleService.ValidateRoleAssignmentAgainstCaller(ctx, ps, roleID, environmentID); err != nil {
		if common.IsRolePermissionEscalationError(err) {
			return fmt.Errorf("%w: %w", &common.FederatedCredentialPermissionEscalationError{}, err)
		}
		return fmt.Errorf("%w: %w", &common.FederatedCredentialInvalidError{}, err)
	}
	return nil
}

func revokeFederatedCredentialSessionsInternal(tx *gorm.DB, credentialID string) error {
	if tx == nil || strings.TrimSpace(credentialID) == "" {
		return nil
	}

	now := time.Now()
	if err := tx.Model(&models.UserSession{}).
		Where("federated_credential_id = ? AND revoked_at IS NULL", credentialID).
		Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error; err != nil {
		return fmt.Errorf("failed to revoke federated credential sessions: %w", err)
	}
	return nil
}

func toFederatedCredentialDTOInternal(credential *models.FederatedCredential) federatedtypes.FederatedCredential {
	if credential == nil {
		return federatedtypes.FederatedCredential{}
	}
	dto := federatedtypes.FederatedCredential{
		ID:              credential.ID,
		Name:            credential.Name,
		Description:     credential.Description,
		Enabled:         credential.Enabled,
		IssuerURL:       credential.IssuerURL,
		Audiences:       []string(credential.Audiences),
		SubjectClaim:    credential.SubjectClaim,
		SubjectMatch:    credential.SubjectMatch,
		MatchType:       credential.MatchType,
		RoleID:          credential.RoleID,
		EnvironmentID:   credential.EnvironmentID,
		IdentityUserID:  credential.IdentityUserID,
		TokenTTLSeconds: credential.TokenTTLSeconds,
		LastUsedAt:      credential.LastUsedAt,
		ExpiresAt:       credential.ExpiresAt,
		CreatedAt:       credential.CreatedAt,
		UpdatedAt:       credential.UpdatedAt,
	}
	if credential.IdentityUser != nil {
		dto.ServiceUsername = credential.IdentityUser.Username
	}
	if credential.Role != nil {
		dto.RoleName = credential.Role.Name
	}
	if credential.Environment != nil {
		dto.EnvironmentName = credential.Environment.Name
	}
	return dto
}

func (s *FederatedCredentialService) markCredentialUsedAsyncInternal(ctx context.Context, credentialID string) {
	go func() {
		bgCtx := context.WithoutCancel(ctx)
		now := time.Now()
		cutoff := now.Add(-federatedCredentialLastUsedWriteWindow)
		if err := s.db.WithContext(bgCtx).
			Model(&models.FederatedCredential{}).
			Where("id = ? AND (last_used_at IS NULL OR last_used_at < ?)", credentialID, cutoff).
			Update("last_used_at", now).Error; err != nil {
			slog.WarnContext(bgCtx, "failed to update federated credential last_used_at", "credential_id", credentialID, "error", err)
		}
	}()
}

func (s *FederatedCredentialService) logExchangeInternal(ctx context.Context, result, reason, issuer, subject string, audiences []string, credential *models.FederatedCredential, user *models.User) {
	credentialID := ""
	credentialName := ""
	if credential != nil {
		credentialID = credential.ID
		credentialName = credential.Name
	}
	slog.InfoContext(ctx, "Federated credential token exchange",
		"result", result,
		"reason", reason,
		"issuer", issuer,
		"subject", subject,
		"audiences", audiences,
		"credential_id", credentialID,
	)

	if s.eventService == nil {
		return
	}

	metadata := models.JSON{
		"action":       "federated_token_exchange",
		"result":       result,
		"reason":       reason,
		"issuer":       issuer,
		"subject":      subject,
		"audiences":    audiences,
		"credentialId": credentialID,
	}

	userID := ""
	username := ""
	if user != nil {
		userID = user.ID
		username = user.Username
	}
	severity := models.EventSeverityInfo
	title := "Federated credential token exchange"
	if result != "success" {
		severity = models.EventSeverityWarning
		title = "Federated credential token exchange rejected"
	}

	go func() {
		bgCtx := context.WithoutCancel(ctx)
		_, err := s.eventService.CreateEvent(bgCtx, CreateEventRequest{
			Type:         models.EventTypeFederatedExchange,
			Severity:     severity,
			Title:        title,
			Description:  "Workload identity federation token exchange",
			ResourceType: pkgutils.StringPtrFromTrimmed("federated_credential"),
			ResourceID:   pkgutils.StringPtrFromTrimmed(credentialID),
			ResourceName: pkgutils.StringPtrFromTrimmed(credentialName),
			UserID:       pkgutils.StringPtrFromTrimmed(userID),
			Username:     pkgutils.StringPtrFromTrimmed(username),
			Metadata:     metadata,
		})
		if err != nil {
			slog.WarnContext(bgCtx, "failed to audit federated credential token exchange", "error", err)
		}
	}()
}
