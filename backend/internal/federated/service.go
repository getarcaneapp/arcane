package federated

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/samber/mo"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/fx"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/auth"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/role"
	"getarcane.dev/arcane/v3/internal/session"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/user"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/logging"
	arcaneoidc "getarcane.dev/arcane/v3/pkg/oidc"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/types/v3"
)

type (
	FederatedCredentialService struct {
		providerGroup   singleflight.Group
		exchanges       metric.Int64Counter
		db              *database.DB
		authService     *auth.AuthService
		userService     *user.UserService
		settingsService *settings.SettingsService
		eventService    *event.EventService
		roleService     *role.RoleService
		httpClient      *http.Client
		keySetManager   *arcaneoidc.KeySetManager
		keySets         map[string]oidc.KeySet
		providerMu      sync.RWMutex
	}

	FederatedCredentialServiceParams struct {
		fx.In

		DB              *database.DB
		AuthService     *auth.AuthService
		UserService     *user.UserService
		SettingsService *settings.SettingsService
		EventService    *event.EventService
		HTTPClient      *http.Client
		KeySetManager   *arcaneoidc.KeySetManager
		RoleService     *role.RoleService
	}

	CredentialUpdate struct {
		Submitted map[string]bool
		Request   types.FederatedCredential
	}
)

const (
	federatedCredentialLastUsedWriteWindow = 5 * time.Minute
	defaultFederatedSubjectClaim           = "sub"
)

var errInvalidGrant = common.Classify(common.ErrFederatedCredentialInvalidGrant, errors.New("invalid federated token grant"))

func NewFederatedCredentialService(p FederatedCredentialServiceParams) *FederatedCredentialService {
	client := httpx.NewHTTPClient(httpx.ClientConfig{
		Timeout:             15 * time.Second,
		MaxIdleConnsPerHost: 32,
	})
	if p.HTTPClient != nil {
		copied := *p.HTTPClient
		client = &copied
	}
	client.Transport = otelhttp.NewTransport(client.Transport)

	exchanges, err := otel.Meter(logging.InstrumentationName).Int64Counter("arcane.federated.token_exchanges",
		metric.WithDescription("Federated credential token exchanges by outcome"), metric.WithUnit("{exchange}"))
	if err != nil {
		otel.Handle(err)
	}

	return &FederatedCredentialService{
		db:              p.DB,
		authService:     p.AuthService,
		userService:     p.UserService,
		settingsService: p.SettingsService,
		eventService:    p.EventService,
		httpClient:      client,
		keySetManager:   p.KeySetManager,
		roleService:     p.RoleService,
		keySets:         make(map[string]oidc.KeySet),
		exchanges:       exchanges,
	}
}

func (s *FederatedCredentialService) Create(ctx context.Context, callerUserID string, req types.FederatedCredential) (*types.FederatedCredential, error) {
	normalized, err := normalizeFederatedCredential(req)
	if err != nil {
		return nil, err
	}
	if validateRoleGrantAgainstUserErr := s.validateRoleGrantAgainstUser(ctx, callerUserID, normalized.RoleID, normalized.EnvironmentID); validateRoleGrantAgainstUserErr != nil {
		return nil, validateRoleGrantAgainstUserErr
	}

	var created FederatedCredential
	err = s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		serviceUser := user.User{
			Username:         "svc_federated_" + strings.ReplaceAll(uuid.New().String(), "-", ""),
			DisplayName:      mo.EmptyableToOption(strings.TrimSpace("Federated: " + normalized.Name)).ToPointer(),
			IsServiceAccount: true,
		}
		if createServiceUserErr := tx.Create(&serviceUser).Error; createServiceUserErr != nil {
			return fmt.Errorf("failed to create federated service user: %w", createServiceUserErr)
		}

		created = FederatedCredential{
			Name:            normalized.Name,
			Description:     normalized.Description,
			Enabled:         normalized.Enabled,
			IssuerURL:       normalized.IssuerURL,
			Audiences:       normalized.Audiences,
			SubjectClaim:    normalized.SubjectClaim,
			SubjectMatch:    normalized.SubjectMatch,
			MatchType:       normalized.MatchType,
			RoleID:          normalized.RoleID,
			EnvironmentID:   normalized.EnvironmentID,
			IdentityUserID:  serviceUser.ID,
			TokenTTLSeconds: normalized.TokenTTLSeconds,
			ExpiresAt:       normalized.ExpiresAt,
		}
		if createCredentialErr := tx.Create(&created).Error; createCredentialErr != nil {
			return fmt.Errorf("failed to create federated credential: %w", createCredentialErr)
		}

		assignment := role.UserRoleAssignment{
			UserID:        serviceUser.ID,
			RoleID:        normalized.RoleID,
			EnvironmentID: normalized.EnvironmentID,
			Source:        role.RoleAssignmentSourceManual,
		}
		if createRoleAssignmentErr := tx.Create(&assignment).Error; createRoleAssignmentErr != nil {
			return fmt.Errorf("failed to create federated role assignment: %w", createRoleAssignmentErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.roleService.InvalidateUser(created.IdentityUserID)

	return s.Get(ctx, created.ID)
}

func (s *FederatedCredentialService) List(ctx context.Context, params pagination.QueryParams) ([]types.FederatedCredential, pagination.Response, error) {
	var credentials []FederatedCredential
	query := s.db.WithContext(ctx).
		Model(&FederatedCredential{}).
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

	result := make([]types.FederatedCredential, len(credentials))
	for i := range credentials {
		result[i] = toFederatedCredentialDTO(&credentials[i])
	}
	return result, resp, nil
}

func (s *FederatedCredentialService) Get(ctx context.Context, id string) (*types.FederatedCredential, error) {
	credential, err := findCredential(s.db.WithContext(ctx).Preload("IdentityUser").Preload("Role").Preload("Environment"), id)
	if err != nil {
		return nil, err
	}
	return new(toFederatedCredentialDTO(&credential)), nil
}

func findCredential(query *gorm.DB, id string) (FederatedCredential, error) {
	var credential FederatedCredential
	if err := query.Where("id = ?", id).First(&credential).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return credential, common.Classify(common.ErrFederatedCredentialNotFound, errors.New("federated credential not found"))
		}
		return credential, fmt.Errorf("failed to load federated credential: %w", err)
	}
	return credential, nil
}

func (s *FederatedCredentialService) Update(ctx context.Context, callerUserID, id string, change CredentialUpdate) (*types.FederatedCredential, error) {
	credential, err := findCredential(s.db.WithContext(ctx), id)
	if err != nil {
		return nil, err
	}

	normalized, err := normalizeFederatedCredential(types.FederatedCredential{
		Name:            kit.Ternary(change.Submitted["name"], change.Request.Name, credential.Name),
		Description:     kit.Ternary(change.Submitted["description"], change.Request.Description, credential.Description),
		Enabled:         kit.Ternary(change.Submitted["enabled"], change.Request.Enabled, credential.Enabled),
		IssuerURL:       kit.Ternary(change.Submitted["issuerUrl"], change.Request.IssuerURL, credential.IssuerURL),
		Audiences:       kit.Ternary(change.Submitted["audiences"], change.Request.Audiences, []string(credential.Audiences)),
		SubjectClaim:    kit.Ternary(change.Submitted["subjectClaim"], change.Request.SubjectClaim, credential.SubjectClaim),
		SubjectMatch:    kit.Ternary(change.Submitted["subjectMatch"], change.Request.SubjectMatch, credential.SubjectMatch),
		MatchType:       kit.Ternary(change.Submitted["matchType"], change.Request.MatchType, credential.MatchType),
		RoleID:          kit.Ternary(change.Submitted["roleId"], change.Request.RoleID, credential.RoleID),
		EnvironmentID:   kit.Ternary(change.Submitted["environmentId"], change.Request.EnvironmentID, credential.EnvironmentID),
		TokenTTLSeconds: kit.Ternary(change.Submitted["tokenTtlSeconds"], change.Request.TokenTTLSeconds, credential.TokenTTLSeconds),
		ExpiresAt:       kit.Ternary(change.Submitted["expiresAt"], change.Request.ExpiresAt, credential.ExpiresAt),
	})
	if err != nil {
		return nil, err
	}
	updated := FederatedCredential{
		BaseModel:       credential.BaseModel,
		Name:            normalized.Name,
		Description:     normalized.Description,
		Enabled:         normalized.Enabled,
		IssuerURL:       normalized.IssuerURL,
		Audiences:       normalized.Audiences,
		SubjectClaim:    normalized.SubjectClaim,
		SubjectMatch:    normalized.SubjectMatch,
		MatchType:       normalized.MatchType,
		RoleID:          normalized.RoleID,
		EnvironmentID:   normalized.EnvironmentID,
		IdentityUserID:  credential.IdentityUserID,
		TokenTTLSeconds: normalized.TokenTTLSeconds,
		LastUsedAt:      credential.LastUsedAt,
		ExpiresAt:       normalized.ExpiresAt,
	}
	roleChanged := updated.RoleID != credential.RoleID || kit.FromPtr(updated.EnvironmentID) != kit.FromPtr(credential.EnvironmentID)
	revokeActiveSessions := credential.Enabled && !updated.Enabled
	if roleChanged {
		if validateRoleGrantAgainstUserErr := s.validateRoleGrantAgainstUser(ctx, callerUserID, updated.RoleID, updated.EnvironmentID); validateRoleGrantAgainstUserErr != nil {
			return nil, validateRoleGrantAgainstUserErr
		}
	}

	err = s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if updateCredentialErr := tx.Save(&updated).Error; updateCredentialErr != nil {
			return fmt.Errorf("failed to update federated credential: %w", updateCredentialErr)
		}
		if revokeActiveSessions {
			now := time.Now()
			if revokeCredentialSessionsErr := tx.Model(&session.UserSession{}).
				Where("federated_credential_id = ? AND revoked_at IS NULL", updated.ID).
				Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error; revokeCredentialSessionsErr != nil {
				return fmt.Errorf("failed to revoke federated credential sessions: %w", revokeCredentialSessionsErr)
			}
		}
		if roleChanged {
			if clearRoleAssignmentErr := tx.Where("user_id = ? AND source = ?", updated.IdentityUserID, role.RoleAssignmentSourceManual).
				Delete(&role.UserRoleAssignment{}).Error; clearRoleAssignmentErr != nil {
				return fmt.Errorf("failed to clear federated role assignment: %w", clearRoleAssignmentErr)
			}
			assignment := role.UserRoleAssignment{
				UserID:        updated.IdentityUserID,
				RoleID:        updated.RoleID,
				EnvironmentID: updated.EnvironmentID,
				Source:        role.RoleAssignmentSourceManual,
			}
			if updateRoleAssignmentErr := tx.Create(&assignment).Error; updateRoleAssignmentErr != nil {
				return fmt.Errorf("failed to update federated role assignment: %w", updateRoleAssignmentErr)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if roleChanged {
		s.roleService.InvalidateUser(updated.IdentityUserID)
	}
	if revokeActiveSessions || roleChanged {
		s.authService.InvalidateUserTokenCache(updated.IdentityUserID)
	}
	return s.Get(ctx, id)
}

func (s *FederatedCredentialService) Delete(ctx context.Context, id string) error {
	credential, err := findCredential(s.db.WithContext(ctx), id)
	if err != nil {
		return err
	}

	err = s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if deleteErr := tx.Delete(&FederatedCredential{}, "id = ?", credential.ID).Error; deleteErr != nil {
			return fmt.Errorf("failed to delete federated credential: %w", deleteErr)
		}
		if deleteErr := tx.Delete(&user.User{}, "id = ?", credential.IdentityUserID).Error; deleteErr != nil {
			return fmt.Errorf("failed to delete federated service user: %w", deleteErr)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.roleService.InvalidateUser(credential.IdentityUserID)
	s.authService.InvalidateUserTokenCache(credential.IdentityUserID)
	return nil
}

func (s *FederatedCredentialService) ExchangeToken(ctx context.Context, req types.TokenExchangeRequest) (_ *types.FederatedTokenResponse, err error) {
	ctx, span := otel.Tracer(logging.InstrumentationName).Start(ctx, "federated.token_exchange")
	claims := arcaneoidc.ParseJWTClaims(req.SubjectToken)
	issuer := ""
	subject := ""
	var audiences []string
	if claims != nil {
		issuer = kit.ToString(arcaneoidc.GetByPath(claims, "iss").OrEmpty())
		subject = kit.ToString(arcaneoidc.GetByPath(claims, "sub").OrEmpty())
		audiences = kit.Unique(kit.TrimNonEmpty(kit.Collect(arcaneoidc.GetByPath(claims, "aud").OrEmpty(), func(item any) string { return kit.As(item, "") })))
	}

	logResult := "failure"
	logReason := ""
	var credentialID, credentialName, userID, username string
	defer func() {
		outcome := []attribute.KeyValue{attribute.String("arcane.federated.outcome", logResult), attribute.String("arcane.federated.reason", logReason)}
		s.exchanges.Add(context.WithoutCancel(ctx), 1, metric.WithAttributes(outcome...))
		span.SetAttributes(append(outcome, attribute.String("arcane.federated.issuer", issuer), attribute.String("arcane.federated.credential_id", credentialID))...)
		logging.EndSpan(span, err)

		slog.InfoContext(ctx, "Federated credential token exchange",
			"result", logResult,
			"reason", logReason,
			"issuer", issuer,
			"subject", subject,
			"audiences", audiences,
			"credentialId", credentialID,
		)
		severity := event.EventSeverityInfo
		title := "Federated credential token exchange"
		if logResult != "success" {
			severity = event.EventSeverityWarning
			title = "Federated credential token exchange rejected"
		}
		eventRequest := event.CreateEventRequest{
			Type:         event.EventTypeFederatedExchange,
			Severity:     severity,
			Title:        title,
			Description:  "Workload identity federation token exchange",
			ResourceType: new("federated_credential"),
			ResourceID:   mo.EmptyableToOption(strings.TrimSpace(credentialID)).ToPointer(),
			ResourceName: mo.EmptyableToOption(strings.TrimSpace(credentialName)).ToPointer(),
			UserID:       mo.EmptyableToOption(strings.TrimSpace(userID)).ToPointer(),
			Username:     mo.EmptyableToOption(strings.TrimSpace(username)).ToPointer(),
			Metadata: database.JSON{
				"action":       "federated_token_exchange",
				"result":       logResult,
				"reason":       logReason,
				"issuer":       issuer,
				"subject":      subject,
				"audiences":    audiences,
				"credentialId": credentialID,
			},
		}
		go func() {
			bgCtx := context.WithoutCancel(ctx)
			if _, eventErr := s.eventService.CreateEvent(bgCtx, eventRequest); eventErr != nil {
				slog.WarnContext(bgCtx, "failed to audit federated credential token exchange", "error", eventErr)
			}
		}()
	}()

	invalidRequest := req.GrantType != types.TokenExchangeGrantType || strings.TrimSpace(req.SubjectToken) == "" ||
		(req.SubjectTokenType != types.SubjectTokenTypeJWT && req.SubjectTokenType != types.SubjectTokenTypeIDToken) ||
		(req.RequestedTokenType != "" && req.RequestedTokenType != types.RequestedTokenTypeAccessJWT)
	if invalidRequest {
		logReason = "invalid_request"
		return nil, common.Classify(common.ErrFederatedCredentialInvalidRequest, errors.New("invalid federated token exchange request"))
	}
	if issuer == "" {
		logReason = "missing_issuer"
		return nil, errInvalidGrant
	}

	var credentials []FederatedCredential
	if findErr := s.db.WithContext(ctx).
		Where("issuer_url = ? AND enabled = ?", issuer, true).
		Order("created_at ASC").
		Order("id ASC").
		Find(&credentials).Error; findErr != nil {
		logReason = "credential_lookup_failed"
		return nil, fmt.Errorf("failed to list federated credentials for issuer: %w", findErr)
	}
	now := time.Now()
	credentials = slices.DeleteFunc(credentials, func(credential FederatedCredential) bool {
		return credential.ExpiresAt != nil && now.After(*credential.ExpiresAt)
	})
	if len(credentials) == 0 {
		logReason = "issuer_not_allowed"
		return nil, errInvalidGrant
	}

	verifiedToken, verifiedClaims, err := s.verifySubjectToken(ctx, issuer, req.SubjectToken)
	if err != nil {
		logReason = "token_verification_failed"
		return nil, common.Classify(common.ErrFederatedCredentialInvalidGrant, fmt.Errorf("invalid federated token grant: %w", err))
	}
	if subject == "" {
		subject = kit.ToString(arcaneoidc.GetByPath(verifiedClaims, defaultFederatedSubjectClaim).OrEmpty())
	}
	if len(audiences) == 0 {
		audiences = append([]string{}, verifiedToken.Audience...)
	}

	matchIndex := slices.IndexFunc(credentials, func(credential FederatedCredential) bool {
		return credentialMatchesToken(&credential, verifiedToken.Audience, verifiedClaims)
	})
	if matchIndex < 0 {
		logReason = "no_matching_credential"
		return nil, errInvalidGrant
	}
	credential := &credentials[matchIndex]
	credentialID, credentialName = credential.ID, credential.Name
	logReason = "token_replay_rejected"
	if verifiedToken.Expiry.IsZero() || time.Now().After(verifiedToken.Expiry) {
		return nil, errInvalidGrant
	}
	if pruneErr := s.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&FederatedTokenReplay{}).Error; pruneErr != nil {
		return nil, fmt.Errorf("failed to prune federated token replay records: %w", pruneErr)
	}
	tokenID := strings.TrimSpace(kit.ToString(arcaneoidc.GetByPath(verifiedClaims, "jti").OrEmpty()))
	tokenKind := "jti"
	if tokenID == "" {
		tokenID = req.SubjectToken
		tokenKind = "token"
	}
	replay := FederatedTokenReplay{
		TokenHash: kit.SHA256Hex(issuer + "\x00" + tokenKind + "\x00" + tokenID),
		IssuerURL: issuer,
		ExpiresAt: verifiedToken.Expiry,
	}
	if replayErr := s.db.WithContext(ctx).Create(&replay).Error; replayErr != nil {
		message := strings.ToLower(replayErr.Error())
		if strings.Contains(message, "unique") || strings.Contains(message, "duplicate key") {
			return nil, errInvalidGrant
		}
		return nil, fmt.Errorf("failed to record federated token replay guard: %w", replayErr)
	}

	identityUser, err := s.userService.GetUserByID(ctx, credential.IdentityUserID)
	if err != nil {
		logReason = "identity_user_missing"
		return nil, common.Classify(common.ErrFederatedCredentialInvalidGrant, fmt.Errorf("invalid federated token grant: %w", err))
	}
	userID, username = identityUser.ID, identityUser.Username

	tokenPair, err := s.authService.IssueFederatedToken(ctx, identityUser, credential.ID, credential.TokenTTLSeconds)
	if err != nil {
		logReason = "token_issue_failed"
		return nil, err
	}

	go func() {
		bgCtx := context.WithoutCancel(ctx)
		localNow := time.Now()
		cutoff := localNow.Add(-federatedCredentialLastUsedWriteWindow)
		if updateLastUsedErr := s.db.WithContext(bgCtx).
			Model(&FederatedCredential{}).
			Where("id = ? AND (last_used_at IS NULL OR last_used_at < ?)", credential.ID, cutoff).
			Update("last_used_at", localNow).Error; updateLastUsedErr != nil {
			slog.WarnContext(bgCtx, "failed to update federated credential last_used_at", "credentialId", credential.ID, "error", updateLastUsedErr)
		}
	}()

	logResult = "success"
	logReason = "matched"
	return &types.FederatedTokenResponse{
		AccessToken:     tokenPair.AccessToken,
		TokenType:       "Bearer",
		ExpiresIn:       max(int(time.Until(tokenPair.ExpiresAt).Seconds()), 0),
		IssuedTokenType: types.IssuedTokenTypeAccessToken,
	}, nil
}

// verifySubjectToken discovers the issuer's JWK set once and verifies rawToken against it.
func (s *FederatedCredentialService) verifySubjectToken(ctx context.Context, issuer, rawToken string) (*oidc.IDToken, map[string]any, error) {
	s.providerMu.RLock()
	keySet := s.keySets[issuer]
	s.providerMu.RUnlock()
	if keySet == nil {
		value, err, _ := s.providerGroup.Do(issuer, func() (any, error) {
			providerCtx := oidc.ClientContext(context.WithoutCancel(ctx), s.httpClient)
			provider, err := oidc.NewProvider(providerCtx, issuer)
			if err != nil {
				return nil, fmt.Errorf("failed to discover federated issuer: %w", err)
			}

			var metadata struct {
				JWKSURL string `json:"jwks_uri"`
			}
			if claimsErr := provider.Claims(&metadata); claimsErr != nil {
				return nil, fmt.Errorf("failed to read federated issuer metadata: %w", claimsErr)
			}
			if metadata.JWKSURL == "" {
				return nil, errors.New("federated issuer metadata is missing jwks_uri")
			}

			discovered, err := s.keySetManager.KeySet(context.WithoutCancel(ctx), s.httpClient, metadata.JWKSURL)
			if err != nil {
				return nil, fmt.Errorf("failed to configure federated issuer JWK set: %w", err)
			}
			s.providerMu.Lock()
			s.keySets[issuer] = discovered
			s.providerMu.Unlock()
			return discovered, nil
		})
		if err != nil {
			return nil, nil, err
		}
		if keySet, _ = value.(oidc.KeySet); keySet == nil {
			return nil, nil, errors.New("federated issuer discovery returned invalid key set")
		}
	}

	verifier := oidc.NewVerifier(issuer, keySet, &oidc.Config{
		SkipClientIDCheck:    true,
		SupportedSigningAlgs: arcaneoidc.SupportedSigningAlgs(),
	})
	idToken, err := verifier.Verify(oidc.ClientContext(ctx, s.httpClient), rawToken)
	if err != nil {
		return nil, nil, err
	}

	claims := map[string]any{}
	if claimsErr := idToken.Claims(&claims); claimsErr != nil {
		return nil, nil, claimsErr
	}
	return idToken, claims, nil
}

func credentialMatchesToken(credential *FederatedCredential, tokenAudiences []string, claims map[string]any) bool {
	audienceMatched := slices.ContainsFunc(credential.Audiences, func(audience string) bool {
		audience = strings.TrimSpace(audience)
		return audience != "" && slices.Contains(tokenAudiences, audience)
	})
	if !audienceMatched {
		return false
	}

	subjectClaim := cmp.Or(strings.TrimSpace(credential.SubjectClaim), defaultFederatedSubjectClaim)
	subject := kit.ToString(arcaneoidc.GetByPath(claims, subjectClaim).OrEmpty())
	if subject == "" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(credential.MatchType), types.FederatedMatchTypeGlob) {
		return subject == credential.SubjectMatch
	}

	var expression strings.Builder
	expression.WriteString("^")
	for _, character := range credential.SubjectMatch {
		switch character {
		case '*':
			expression.WriteString(".*")
		case '?':
			expression.WriteByte('.')
		default:
			expression.WriteString(regexp.QuoteMeta(string(character)))
		}
	}
	expression.WriteString("$")
	matched, err := regexp.MatchString(expression.String(), subject)
	return err == nil && matched
}
