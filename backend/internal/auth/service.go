package auth

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"maps"
	"strconv"
	"strings"
	"time"
	"uuid"

	"emperror.dev/emperror"
	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/jwtclaims"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/validation"
	"github.com/getarcaneapp/arcane/types/v2/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/samber/hot"
)

const (
	ErrInvalidCredentials = errors.Sentinel("invalid credentials")
	ErrLocalAuthDisabled  = errors.Sentinel("local authentication is disabled")
	ErrOidcAuthDisabled   = errors.Sentinel("OIDC authentication is disabled")
	ErrMFARequired        = errors.Sentinel("multi-factor authentication is required")
)

type TokenPair struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type AuthSettings struct {
	LocalAuthEnabled bool                 `json:"localAuthEnabled"`
	OidcEnabled      bool                 `json:"oidcEnabled"`
	SessionTimeout   int                  `json:"sessionTimeout"`
	Oidc             *settings.OidcConfig `json:"oidc,omitempty"`
}

type userClaims struct {
	jwt.RegisteredClaims

	SessionID             string `json:"sid,omitempty"`
	UserID                string `json:"user_id"`
	Username              string `json:"username"`
	Email                 string `json:"email,omitempty"`
	DisplayName           string `json:"display_name,omitempty"`
	AppVersion            string `json:"app_version,omitempty"`
	TokenType             string `json:"token_type,omitempty"`
	FederatedCredentialID string `json:"federated_credential_id,omitempty"`
}

type refreshClaims struct {
	jwt.RegisteredClaims

	UserID     string `json:"user_id"`
	SessionID  string `json:"sid,omitempty"`
	AppVersion string `json:"app_version,omitempty"`
}

type verifiedTokenEntry struct {
	User      common.User
	SessionID string
}

type AuthService struct {
	userService     *user.UserService
	settingsService *settings.SettingsService
	eventService    *event.EventService
	sessionService  *session.SessionService
	roleService     *role.RoleService
	jwtSecret       []byte
	refreshExpiry   time.Duration
	config          *config.Config
	errorHandler    emperror.ErrorHandler
	// tokenCache is a per-process in-memory cache for parsed user/token data.
	// Cached entries still revalidate persisted session state so revocation
	// performed by another process takes effect immediately.
	tokenCache *hot.HotCache[string, verifiedTokenEntry]
}

func NewAuthService(userService *user.UserService, settingsService *settings.SettingsService, eventService *event.EventService, sessionService *session.SessionService, roleService *role.RoleService, jwtSecret string, cfg *config.Config, errorHandler emperror.ErrorHandler) *AuthService {
	// Production managers must supply an explicit, non-default JWT_SECRET (fail
	// closed, mirroring the ENCRYPTION_KEY guard). Dev and agent mode auto-generate.
	requireExplicitSecret := cfg.Environment == config.AppEnvironmentProduction && !cfg.AgentMode
	if errorHandler == nil {
		errorHandler = emperror.NoopHandler{}
	}
	return &AuthService{
		userService:     userService,
		settingsService: settingsService,
		eventService:    eventService,
		sessionService:  sessionService,
		roleService:     roleService,
		jwtSecret:       jwtclaims.CheckOrGenerateJwtSecret(jwtSecret, requireExplicitSecret),
		refreshExpiry:   cfg.JWTRefreshExpiry,
		config:          cfg,
		errorHandler:    errorHandler,
		tokenCache: hot.NewHotCache[string, verifiedTokenEntry](hot.LRU, 4096).
			WithTTL(15 * time.Second).
			WithJanitor().
			Build(),
	}
}

func (s *AuthService) getAuthSettings(ctx context.Context) (*AuthSettings, error) {
	appSettings, err := s.settingsService.GetSettings(ctx)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to get settings")
	}

	timeoutMinutes, _ := s.GetSessionTimeout(ctx)

	authSettings := &AuthSettings{
		LocalAuthEnabled: appSettings.AuthLocalEnabled.IsTrue(),
		OidcEnabled:      appSettings.OidcEnabled.IsTrue(),
		SessionTimeout:   timeoutMinutes,
	}

	if authSettings.OidcEnabled {
		oidcConfig := &settings.OidcConfig{
			ClientID:                    appSettings.OidcClientId.Value,
			ClientSecret:                appSettings.OidcClientSecret.Value,
			IssuerURL:                   appSettings.OidcIssuerUrl.Value,
			AuthorizationEndpoint:       appSettings.OidcAuthorizationEndpoint.Value,
			TokenEndpoint:               appSettings.OidcTokenEndpoint.Value,
			UserinfoEndpoint:            appSettings.OidcUserinfoEndpoint.Value,
			JwksURI:                     appSettings.OidcJwksEndpoint.Value,
			DeviceAuthorizationEndpoint: appSettings.OidcDeviceAuthorizationEndpoint.Value,
			Scopes:                      appSettings.OidcScopes.Value,
			GroupsClaim:                 appSettings.OidcGroupsClaim.Value,
			SkipTlsVerify:               appSettings.OidcSkipTlsVerify.IsTrue(),
		}

		if oidcConfig.ClientID != "" || oidcConfig.IssuerURL != "" {
			authSettings.Oidc = oidcConfig
		}
	}

	return authSettings, nil
}

func (s *AuthService) GetOidcConfigurationStatus(ctx context.Context) (*auth.OidcStatusInfo, error) {
	oidcEnvForced := s.settingsService != nil && s.settingsService.IsEnvOverrideActive("oidcEnabled")

	mergeAccounts := false
	providerName := ""
	providerLogoUrl := ""
	if s.settingsService != nil {
		func() {
			defer func() {
				// In tests, a zero-valued settings.SettingsService may panic; treat as merge disabled
				_ = recover()
			}()
			if settings, err := s.settingsService.GetSettings(ctx); err == nil {
				mergeAccounts = settings.OidcMergeAccounts.IsTrue()
				providerName = settings.OidcProviderName.Value
				providerLogoUrl = settings.OidcProviderLogoUrl.Value
			}
		}()
	}

	status := &auth.OidcStatusInfo{
		EnvForced:       oidcEnvForced,
		MergeAccounts:   mergeAccounts,
		ProviderName:    providerName,
		ProviderLogoUrl: providerLogoUrl,
	}
	if oidcEnvForced {
		status.EnvConfigured = s.config.OidcClientID != "" && s.config.OidcIssuerURL != ""
		if status.ProviderName == "" {
			status.ProviderName = s.config.OidcProviderName
		}
		if status.ProviderLogoUrl == "" {
			status.ProviderLogoUrl = s.config.OidcProviderLogoUrl
		}
	}
	return status, nil
}

func (s *AuthService) GetSessionTimeout(ctx context.Context) (int, error) {
	settings, err := s.settingsService.GetSettings(ctx)
	if err != nil {
		return 60, err
	}

	minutes := settings.AuthSessionTimeout.AsInt()
	if minutes <= 0 {
		minutes = 60
	}

	if minutes < 15 {
		minutes = 15
	} else if minutes > 525600 {
		minutes = 525600
	}

	return minutes, nil
}

func (s *AuthService) IsLocalAuthEnabled(ctx context.Context) (bool, error) {
	settings, err := s.settingsService.GetSettings(ctx)
	if err != nil {
		return true, err
	}
	return settings.AuthLocalEnabled.IsTrue(), nil
}

func (s *AuthService) IsOidcEnabled(ctx context.Context) (bool, error) {
	settings, err := s.settingsService.GetSettings(ctx)
	if err != nil {
		return false, err
	}
	return settings.OidcEnabled.IsTrue(), nil
}

func (s *AuthService) GetOidcConfig(ctx context.Context) (*settings.OidcConfig, error) {
	authSettings, err := s.getAuthSettings(ctx)
	if err != nil {
		return nil, err
	}

	if !authSettings.OidcEnabled || authSettings.Oidc == nil {
		return nil, ErrOidcAuthDisabled
	}

	return authSettings.Oidc, nil
}

// AuthenticateLocalPrimary validates the local primary factor without
// creating a session. Callers must complete passkey MFA, when enabled, before
// issuing a bearer or refresh token.
func (s *AuthService) AuthenticateLocalPrimary(ctx context.Context, username, password string) (*common.User, error) {
	localEnabled, err := s.IsLocalAuthEnabled(ctx)
	if err != nil {
		return nil, err
	}
	if !localEnabled {
		return nil, ErrLocalAuthDisabled
	}

	user, err := s.userService.GetUserByUsername(ctx, username)
	if err != nil && !errors.Is(err, common.ErrUserNotFound) {
		return nil, err
	}
	// When the identifier looks like an email, resolve the email identity too:
	// a collision between one account's username and a different account's
	// email is rejected instead of silently validating against whichever
	// account the username lookup happened to return.
	if strings.Contains(username, "@") {
		emailUser, emailErr := s.userService.GetUserByEmail(ctx, username)
		switch {
		case errors.Is(emailErr, common.ErrAmbiguousUserEmail):
			slog.WarnContext(ctx, "Rejecting email login: multiple accounts share this email", "email", username)
			return nil, ErrInvalidCredentials
		case emailErr != nil && !errors.Is(emailErr, common.ErrUserNotFound):
			return nil, emailErr
		case emailErr == nil && user != nil && emailUser.ID != user.ID:
			slog.WarnContext(ctx, "Rejecting login: identifier matches one account's username and a different account's email", "identifier", username)
			return nil, ErrInvalidCredentials
		case emailErr == nil && user == nil:
			user = emailUser
		}
	}
	if user == nil {
		return nil, ErrInvalidCredentials
	}
	if err := s.userService.ValidatePassword(user.PasswordHash, password); err != nil {
		return nil, ErrInvalidCredentials
	}

	user.LastLogin = new(time.Now())
	userCopy := new(*user)
	s.runInBackground(ctx, "update_last_login", func(ctx context.Context) error {
		if _, err := s.userService.UpdateUser(ctx, userCopy, nil); err != nil {
			return errors.WrapIf(err, "failed to update user's last login time")
		}
		return nil
	})

	return user, nil
}

// PrepareOidcLogin reconciles the provider identity without creating a
// session. The caller must complete passkey MFA, when enabled, before issuing
// tokens.
func (s *AuthService) PrepareOidcLogin(ctx context.Context, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) (*common.User, bool, error) {
	if userInfo.Subject == "" {
		return nil, false, errors.New("missing OIDC subject identifier")
	}
	return s.findOrCreateOidcUser(ctx, userInfo, tokenResp)
}

// CompleteLogin creates the authenticated session after all required factors
// have succeeded. Source is server-selected and is persisted with the session.
func (s *AuthService) CompleteLogin(ctx context.Context, user *common.User, meta auth.SessionMeta, source, mfaMethod string, eventMetadata ...database.JSON) (*TokenPair, error) {
	if user == nil {
		return nil, common.ErrUserNotFound
	}
	if strings.TrimSpace(source) == "" {
		source = session.UserSessionSourceLocal
	}
	mfaMethod = strings.TrimSpace(mfaMethod)
	if user.PasskeyMFAEnabled && source != session.UserSessionSourcePasskey && mfaMethod != session.PasskeyMFAMethod && mfaMethod != session.RecoveryCodeMFAMethod {
		return nil, ErrMFARequired
	}
	meta.Source = source
	meta.MFAMethod = mfaMethod
	if meta.MFAMethod != "" && meta.MFAVerifiedAt == nil {
		now := time.Now()
		meta.MFAVerifiedAt = &now
	}

	tokenPair, err := s.createSessionAndTokensInternal(ctx, user, meta)
	if err != nil {
		return nil, err
	}

	metadata := database.JSON{"action": "login", "method": source}
	if len(eventMetadata) > 0 {
		maps.Copy(metadata, eventMetadata[0])
	}
	if meta.MFAMethod != "" {
		metadata["mfa"] = meta.MFAMethod
	}
	if s.eventService != nil {
		logUserID := user.ID
		logUsername := user.Username
		s.runInBackground(ctx, "log_user_login", func(ctx context.Context) error {
			return s.eventService.LogUserEvent(ctx, event.EventTypeUserLogin, logUserID, logUsername, metadata)
		})
	}

	return tokenPair, nil
}

func (s *AuthService) Login(ctx context.Context, username, password string, meta auth.SessionMeta) (*common.User, *TokenPair, error) {
	user, err := s.AuthenticateLocalPrimary(ctx, username, password)
	if err != nil {
		return nil, nil, err
	}
	if user.PasskeyMFAEnabled {
		return nil, nil, ErrMFARequired
	}
	tokenPair, err := s.CompleteLogin(ctx, user, meta, session.UserSessionSourceLocal, "")
	if err != nil {
		return nil, nil, err
	}
	return user, tokenPair, nil
}

func (s *AuthService) OidcLogin(ctx context.Context, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse, meta auth.SessionMeta) (*common.User, *TokenPair, error) {
	user, isNewUser, err := s.PrepareOidcLogin(ctx, userInfo, tokenResp)
	if err != nil {
		return nil, nil, err
	}
	if user.PasskeyMFAEnabled {
		return nil, nil, ErrMFARequired
	}
	tokenPair, err := s.CompleteLogin(ctx, user, meta, session.UserSessionSourceOidc, "", database.JSON{
		"newUser": isNewUser,
		"subject": userInfo.Subject,
	})
	if err != nil {
		return nil, nil, err
	}
	return user, tokenPair, nil
}

func (s *AuthService) LogLogout(ctx context.Context, user *common.User) {
	if s.eventService == nil || user == nil {
		return
	}

	metadata := database.JSON{
		"action": "logout",
	}

	userID := user.ID
	username := user.Username
	s.runInBackground(ctx, "log_user_logout", func(ctx context.Context) error {
		return s.eventService.LogUserEvent(ctx, event.EventTypeUserLogout, userID, username, metadata)
	})
}

func (s *AuthService) findOrCreateOidcUser(ctx context.Context, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) (*common.User, bool, error) {
	user, err := s.userService.GetUserByOidcSubjectId(ctx, userInfo.Subject)
	if err != nil && !errors.Is(err, common.ErrUserNotFound) {
		return nil, false, err
	}

	if user != nil {
		return s.updateExistingOidcUser(ctx, user, userInfo, tokenResp)
	}

	mergedUser, merged, err := s.tryMergeOidcUser(ctx, userInfo, tokenResp)
	if err != nil {
		return nil, false, err
	}
	if merged {
		return mergedUser, false, nil
	}

	created, err := s.createOidcUser(ctx, userInfo, tokenResp)
	if err != nil {
		// A concurrent login for the same subject may have created the
		// account between our lookup and the insert; resolve to it
		// instead of failing the login.
		if existing, lookupErr := s.userService.GetUserByOidcSubjectId(ctx, userInfo.Subject); lookupErr == nil {
			return s.updateExistingOidcUser(ctx, existing, userInfo, tokenResp)
		}
		return nil, false, err
	}
	return created, true, nil
}

func (s *AuthService) updateExistingOidcUser(ctx context.Context, user *common.User, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) (*common.User, bool, error) {
	if err := s.updateOidcUser(ctx, user, userInfo, tokenResp); err != nil {
		return nil, false, err
	}
	return user, false, nil
}

func (s *AuthService) tryMergeOidcUser(ctx context.Context, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) (*common.User, bool, error) {
	if userInfo.Email == "" || !s.isOidcMergeEnabled(ctx) {
		return nil, false, nil
	}

	existingUser, emailErr := s.userService.GetUserByEmail(ctx, userInfo.Email)
	if emailErr != nil {
		if errors.Is(emailErr, common.ErrUserNotFound) {
			return nil, false, nil
		}
		return nil, false, emailErr
	}
	if existingUser == nil {
		return nil, false, nil
	}

	if err := s.validateMergeEmailVerification(userInfo); err != nil {
		return nil, false, err
	}

	slog.Info("Merging OIDC account with existing user", "email", userInfo.Email, "subject", userInfo.Subject)
	if mergeErr := s.mergeOidcWithExistingUser(ctx, existingUser, userInfo, tokenResp); mergeErr != nil {
		return nil, false, mergeErr
	}
	return existingUser, true, nil
}

func (s *AuthService) isOidcMergeEnabled(ctx context.Context) bool {
	settings, settingsErr := s.settingsService.GetSettings(ctx)
	return settingsErr == nil && settings.OidcMergeAccounts.IsTrue()
}

func (s *AuthService) validateMergeEmailVerification(userInfo auth.OidcUserInfo) error {
	emailVerifiedPresent := false
	if userInfo.Extra != nil {
		if _, ok := userInfo.Extra["email_verified"]; ok {
			emailVerifiedPresent = true
		}
	}
	if emailVerifiedPresent && !userInfo.EmailVerified {
		return errors.New("email not verified by OIDC provider; cannot merge accounts")
	}
	if !emailVerifiedPresent {
		slog.Warn("OIDC email_verified claim missing; allowing merge", "email", userInfo.Email, "subject", userInfo.Subject)
	}
	return nil
}

func (s *AuthService) createOidcUser(ctx context.Context, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) (*common.User, error) {
	username, err := s.resolveOidcUsernameInternal(ctx, userInfo)
	if err != nil {
		return nil, err
	}

	var displayName *string
	switch {
	case userInfo.Name != "":
		displayName = new(userInfo.Name)
	case userInfo.GivenName != "" || userInfo.FamilyName != "":
		displayName = new(strings.TrimSpace(fmt.Sprintf("%s %s", userInfo.GivenName, userInfo.FamilyName)))
	default:
		displayName = new(username)
	}

	user := &common.User{
		ID:            uuid.New().String(),
		Username:      username,
		DisplayName:   displayName,
		Email:         new(userInfo.Email),
		OidcSubjectId: new(userInfo.Subject),
		LastLogin:     new(time.Now()),
	}

	s.persistOidcTokens(user, tokenResp)

	// The username probe is not atomic with the insert: a concurrent
	// first-time login can claim the candidate in between, failing the
	// unique constraint. Probe again for a fresh candidate and retry.
	var createErr error
	for attempt := range 3 {
		if attempt > 0 {
			if user.Username, err = s.resolveOidcUsernameInternal(ctx, userInfo); err != nil {
				return nil, err
			}
		}
		if _, createErr = s.userService.CreateUser(ctx, user); createErr == nil {
			break
		}
	}
	if createErr != nil {
		return nil, createErr
	}
	if err := s.syncOidcRoleAssignments(ctx, user, userInfo, tokenResp); err != nil {
		slog.WarnContext(ctx, "failed to sync OIDC role assignments on user create", "error", err, "user_id", user.ID)
	}
	return user, nil
}

// resolveOidcUsernameInternal returns the preferred username for a new OIDC
// user, or a currently unclaimed collision-free candidate when an existing
// account already owns it.
func (s *AuthService) resolveOidcUsernameInternal(ctx context.Context, userInfo auth.OidcUserInfo) (string, error) {
	var username string
	if userInfo.PreferredUsername == "" {
		username = generateUsernameFromEmail(userInfo.Email, userInfo.Subject)
	} else {
		username = userInfo.PreferredUsername
	}

	if _, lookupErr := s.userService.GetUserByUsername(ctx, username); errors.Is(lookupErr, common.ErrUserNotFound) {
		return username, nil
	} else if lookupErr != nil {
		return "", lookupErr
	}

	base := uniqueOidcUsernameInternal(username, userInfo.Subject)
	unique := base
	for i := 2; ; i++ {
		_, candidateErr := s.userService.GetUserByUsername(ctx, unique)
		if errors.Is(candidateErr, common.ErrUserNotFound) {
			slog.InfoContext(ctx, "OIDC username already taken by an existing account; assigning a unique username instead",
				"username", username, "unique_username", unique, "subject", userInfo.Subject)
			return unique, nil
		}
		if candidateErr != nil {
			return "", candidateErr
		}
		unique = base + "_" + strconv.Itoa(i)
	}
}

func (s *AuthService) updateOidcUser(ctx context.Context, user *common.User, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) error {
	if userInfo.Name != "" && user.DisplayName == nil {
		user.DisplayName = new(userInfo.Name)
	}
	if userInfo.Email != "" && user.Email == nil {
		user.Email = new(userInfo.Email)
	}

	s.persistOidcTokens(user, tokenResp)

	user.LastLogin = new(time.Now())
	if _, err := s.userService.UpdateUser(ctx, user, nil); err != nil {
		return err
	}
	if err := s.syncOidcRoleAssignments(ctx, user, userInfo, tokenResp); err != nil {
		slog.WarnContext(ctx, "failed to sync OIDC role assignments on user update", "error", err, "user_id", user.ID)
	}
	return nil
}

func (s *AuthService) mergeOidcWithExistingUser(ctx context.Context, user *common.User, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) error {
	// Perform the merge atomically to avoid races when multiple OIDC subjects share the same email
	merged, err := s.userService.AttachOidcSubjectTransactional(ctx, user.ID, userInfo.Subject, func(u *common.User) {
		if userInfo.Name != "" && u.DisplayName == nil {
			u.DisplayName = new(userInfo.Name)
		}
		s.persistOidcTokens(u, tokenResp)
		u.LastLogin = new(time.Now())
	})
	if err != nil {
		return err
	}
	if merged != nil {
		if syncErr := s.syncOidcRoleAssignments(ctx, merged, userInfo, tokenResp); syncErr != nil {
			slog.WarnContext(ctx, "failed to sync OIDC role assignments on user merge", "error", syncErr, "user_id", merged.ID)
		}
	}
	return nil
}

// syncOidcRoleAssignments rebuilds the user's `source='oidc'` role assignments
// based on the OIDC group claim and the configured OidcRoleMapping rows.
// Manual assignments are untouched.
func (s *AuthService) syncOidcRoleAssignments(ctx context.Context, user *common.User, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) error {
	if s.roleService == nil || user == nil {
		return nil
	}

	groups := s.extractOidcGroups(ctx, userInfo, tokenResp)
	mappings, err := s.roleService.ListOidcMappings(ctx)
	if err != nil {
		return errors.WrapIf(err, "list oidc mappings")
	}

	groupSet := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		groupSet[g] = struct{}{}
	}

	var desired []role.UserRoleAssignment
	seen := make(map[string]struct{}) // dedup by roleID|envID
	for _, m := range mappings {
		if _, ok := groupSet[m.ClaimValue]; !ok {
			continue
		}
		key := m.RoleID + "|"
		if m.EnvironmentID != nil {
			key += *m.EnvironmentID
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		desired = append(desired, role.UserRoleAssignment{
			RoleID:        m.RoleID,
			EnvironmentID: m.EnvironmentID,
		})
	}

	return s.roleService.ReplaceOidcAssignments(ctx, user.ID, desired)
}

// extractOidcGroups reads the user's group memberships from the OIDC userinfo
// and ID token, using the claim path configured in OidcGroupsClaim (defaults
// to "groups"). Falls back to userInfo.Groups if no value is found at the
// configured path.
func (s *AuthService) extractOidcGroups(ctx context.Context, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) []string {
	claim := s.oidcGroupsClaim(ctx)

	if claim != "" {
		if v, ok := jwtclaims.GetByPath(userInfo.Extra, claim).Get(); ok {
			if groups := stringValuesFromClaim(v); len(groups) > 0 {
				return groups
			}
		}
		if tokenResp != nil && tokenResp.IDToken != "" {
			if parsed := jwtclaims.ParseJWTClaims(tokenResp.IDToken); parsed != nil {
				if v, ok := jwtclaims.GetByPath(parsed, claim).Get(); ok {
					if groups := stringValuesFromClaim(v); len(groups) > 0 {
						return groups
					}
				}
			}
		}
	}

	return userInfo.Groups
}

func (s *AuthService) oidcGroupsClaim(ctx context.Context) string {
	settings, err := s.settingsService.GetSettings(ctx)
	if err != nil {
		return "groups"
	}
	v := strings.TrimSpace(settings.OidcGroupsClaim.Value)
	if v == "" {
		return "groups"
	}
	return v
}

// stringValuesFromClaim flattens a claim value into a slice of strings.
// Accepts string, []string, []any (coerces each element to string), or nil.
func stringValuesFromClaim(v any) []string {
	switch typed := v.(type) {
	case nil:
		return nil
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func (s *AuthService) persistOidcTokens(user *common.User, tokenResp *auth.OidcTokenResponse) {
	if tokenResp == nil {
		return
	}
	if tokenResp.AccessToken != "" {
		user.OidcAccessToken = new(tokenResp.AccessToken)
	}
	if tokenResp.RefreshToken != "" {
		user.OidcRefreshToken = new(tokenResp.RefreshToken)
	}
	if tokenResp.ExpiresIn > 0 {
		user.OidcAccessTokenExpiresAt = new(time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second))
	}
}

func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string, meta auth.SessionMeta) (*TokenPair, error) {
	token, err := jwt.ParseWithClaims(refreshToken, &refreshClaims{},
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return s.jwtSecret, nil
		})
	if err != nil {
		return nil, common.ErrInvalidToken
	}

	if !token.Valid {
		return nil, common.ErrInvalidToken
	}

	claims, ok := token.Claims.(*refreshClaims)
	if !ok {
		return nil, common.Classify(common.ErrTokenValidation, errors.New("Invalid token claims"))
	}

	if claims.Subject != "refresh" {
		return nil, common.Classify(common.ErrTokenValidation, errors.New("Not a refresh token"))
	}

	if claims.AppVersion != "" && claims.AppVersion != config.Version {
		slog.InfoContext(ctx, "Refresh token version mismatch — rotating to current version", "tokenVersion", claims.AppVersion, "currentVersion", config.Version)
	}

	if claims.UserID == "" {
		return nil, common.Classify(common.ErrTokenValidation, errors.New("Missing user ID in token"))
	}
	if claims.ID == "" {
		return nil, common.Classify(common.ErrTokenValidation, errors.New("Missing refresh token ID"))
	}
	if claims.SessionID == "" {
		return nil, common.Classify(common.ErrTokenValidation, errors.New("Missing session ID in token"))
	}
	if s.sessionService == nil {
		return nil, common.Classify(common.ErrUnavailable, errors.New("Session service is not configured"))
	}

	userSession, err := s.sessionService.GetSessionByID(ctx, claims.SessionID)
	if err != nil {
		return nil, err
	}
	if userSession.UserID != claims.UserID {
		return nil, common.ErrInvalidToken
	}
	if err := session.ValidateActive(userSession); err != nil {
		return nil, err
	}

	rotatedSession, refreshJTI, err := s.sessionService.RotateRefreshToken(ctx, claims.SessionID, claims.ID, meta)
	if err != nil {
		return nil, err
	}

	user, err := s.userService.GetUserByID(ctx, claims.UserID)
	if err != nil {
		return nil, err
	}

	return s.buildTokenPairInternal(ctx, user, rotatedSession, refreshJTI)
}

func (s *AuthService) VerifyToken(ctx context.Context, accessToken string) (*common.User, string, error) {
	token, err := jwt.ParseWithClaims(accessToken, &userClaims{},
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return s.jwtSecret, nil
		})
	if err != nil {
		if strings.Contains(err.Error(), "token is expired") {
			return nil, "", common.ErrExpiredToken
		}
		return nil, "", common.ErrInvalidToken
	}

	if !token.Valid {
		return nil, "", common.ErrInvalidToken
	}

	claims, ok := token.Claims.(*userClaims)
	if !ok {
		return nil, "", common.Classify(common.ErrTokenValidation, errors.New("Invalid token claims"))
	}

	if claims.Subject != "access" {
		return nil, "", common.Classify(common.ErrTokenValidation, errors.New("Not an access token"))
	}

	if claims.ID == "" {
		return nil, "", common.Classify(common.ErrTokenValidation, errors.New("Missing user ID in token"))
	}

	if claims.AppVersion != "" && claims.AppVersion != config.Version {
		slog.InfoContext(ctx, "Token version mismatch detected", "tokenVersion", claims.AppVersion, "currentVersion", config.Version, "user", claims.Username)
		return nil, "", common.ErrTokenVersionMismatch
	}
	if claims.SessionID == "" {
		return nil, "", common.Classify(common.ErrTokenValidation, errors.New("Missing session ID in token"))
	}
	if s.sessionService == nil {
		return nil, "", common.Classify(common.ErrUnavailable, errors.New("Session service is not configured"))
	}

	tokenHash := hashTokenInternal(accessToken)
	if cached, ok, _ := s.tokenCache.Get(tokenHash); ok {
		if cached.User.ID != claims.ID || cached.SessionID != claims.SessionID {
			s.tokenCache.Delete(tokenHash)
			return nil, "", common.ErrInvalidToken
		}

		userSession, err := s.sessionService.GetSessionByID(ctx, cached.SessionID)
		if err != nil {
			s.tokenCache.Delete(tokenHash)
			return nil, "", err
		}
		if userSession.UserID != cached.User.ID {
			s.tokenCache.Delete(tokenHash)
			return nil, "", common.ErrInvalidToken
		}
		if err := session.ValidateActive(userSession); err != nil {
			s.tokenCache.Delete(tokenHash)
			return nil, "", err
		}

		return new(cached.User), cached.SessionID, nil
	}

	// Verify user exists in DB
	// This ensures that if the database is wiped or user is deleted, the token becomes invalid
	// even if the JWT signature is still valid (e.g. same JWT_SECRET).
	dbUser, err := s.userService.GetUserByID(ctx, claims.ID)
	if err != nil {
		if errors.Is(err, common.ErrUserNotFound) {
			return nil, "", common.ErrInvalidToken
		}
		return nil, "", err
	}

	userSession, err := s.sessionService.GetSessionByID(ctx, claims.SessionID)
	if err != nil {
		return nil, "", err
	}
	if userSession.UserID != dbUser.ID {
		return nil, "", common.ErrInvalidToken
	}
	if err := session.ValidateActive(userSession); err != nil {
		return nil, "", err
	}

	s.tokenCache.Set(tokenHash, verifiedTokenEntry{User: *dbUser, SessionID: userSession.ID})

	return dbUser, userSession.ID, nil
}

func hashTokenInternal(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *AuthService) ChangePassword(ctx context.Context, userID, currentPassword, newPassword, currentSessionID string) error {
	if s.sessionService == nil {
		return common.Classify(common.ErrUnavailable, errors.New("Session service is not configured"))
	}

	user, err := s.userService.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	if user.PasswordHash != "" {
		if err := s.userService.ValidatePassword(user.PasswordHash, currentPassword); err != nil {
			return ErrInvalidCredentials
		}
	}

	policy := validation.PasswordPolicyStrong
	if s.settingsService != nil {
		policy = s.settingsService.GetStringSetting(ctx, "authPasswordPolicy", validation.PasswordPolicyStrong)
	}
	if err := validation.ValidatePasswordPolicy(newPassword, policy); err != nil {
		return common.Classify(common.ErrValidation, err)
	}

	if _, err = s.userService.SetPasswordAndRevokeSessionsExcept(ctx, user, newPassword, currentSessionID); err != nil {
		return err
	}
	keys := make([]string, 0)
	s.tokenCache.Range(func(key string, entry verifiedTokenEntry) bool {
		if entry.User.ID == userID && entry.SessionID != currentSessionID {
			keys = append(keys, key)
		}
		return true
	})
	s.tokenCache.DeleteMany(keys)
	return nil
}

// InvalidateUserTokenCache purges all cached token verifications for a user.
// Call this after admin-initiated role changes, account disable, or user
// deletion so stale verifications cannot grant access for the cache TTL.
func (s *AuthService) InvalidateUserTokenCache(userID string) {
	if s.tokenCache == nil || strings.TrimSpace(userID) == "" {
		return
	}
	keys := make([]string, 0)
	s.tokenCache.Range(func(key string, entry verifiedTokenEntry) bool {
		if entry.User.ID == userID {
			keys = append(keys, key)
		}
		return true
	})
	s.tokenCache.DeleteMany(keys)
}

func (s *AuthService) RevokeSession(ctx context.Context, sessionID string) error {
	if s.sessionService == nil {
		return nil
	}
	keys := make([]string, 0)
	s.tokenCache.Range(func(key string, entry verifiedTokenEntry) bool {
		if entry.SessionID == sessionID {
			keys = append(keys, key)
		}
		return true
	})
	s.tokenCache.DeleteMany(keys)
	return s.sessionService.RevokeSession(ctx, sessionID)
}

// LogoutAllOtherSessions revokes every active session for userID except
// currentSessionID, so the caller stays signed in on their current device.
func (s *AuthService) LogoutAllOtherSessions(ctx context.Context, userID, currentSessionID string) error {
	if s.sessionService == nil {
		return nil
	}
	keys := make([]string, 0)
	s.tokenCache.Range(func(key string, entry verifiedTokenEntry) bool {
		if entry.User.ID == userID && entry.SessionID != currentSessionID {
			keys = append(keys, key)
		}
		return true
	})
	s.tokenCache.DeleteMany(keys)
	return s.sessionService.RevokeAllUserSessionsExcept(ctx, userID, currentSessionID)
}

func (s *AuthService) createSessionAndTokensInternal(ctx context.Context, user *common.User, meta auth.SessionMeta) (*TokenPair, error) {
	if s.sessionService == nil {
		return nil, common.Classify(common.ErrUnavailable, errors.New("Session service is not configured"))
	}
	refreshExpiry := time.Now().Add(s.refreshExpiry)
	session, refreshJTI, err := s.sessionService.CreateSession(ctx, user.ID, refreshExpiry, meta)
	if err != nil {
		return nil, err
	}
	return s.buildTokenPairInternal(ctx, user, session, refreshJTI)
}

func (s *AuthService) buildTokenPairInternal(ctx context.Context, user *common.User, session *session.UserSession, refreshJTI string) (*TokenPair, error) {
	sessionTimeout, _ := s.GetSessionTimeout(ctx)

	accessTokenExpiry := time.Now().Add(time.Duration(sessionTimeout) * time.Minute)

	userClaims := userClaims{
		ID:         user.ID,
		Subject:    "access",
		IssuedAt:   jwt.NewNumericDate(time.Now()),
		ExpiresAt:  jwt.NewNumericDate(accessTokenExpiry),
		SessionID:  session.ID,
		UserID:     user.ID,
		Username:   user.Username,
		AppVersion: config.Version,
	}

	if user.Email != nil {
		userClaims.Email = *user.Email
	}

	if user.DisplayName != nil {
		userClaims.DisplayName = *user.DisplayName
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, userClaims)

	accessTokenString, err := accessToken.SignedString(s.jwtSecret)
	if err != nil {
		return nil, err
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims{
		ID:         refreshJTI,
		Subject:    "refresh",
		IssuedAt:   jwt.NewNumericDate(time.Now()),
		ExpiresAt:  jwt.NewNumericDate(session.ExpiresAt),
		UserID:     user.ID,
		SessionID:  session.ID,
		AppVersion: config.Version,
	})

	refreshTokenString, err := refreshToken.SignedString(s.jwtSecret)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		ExpiresAt:    accessTokenExpiry,
	}, nil
}

func (s *AuthService) IssueFederatedToken(ctx context.Context, user *common.User, credentialID string, ttlSeconds int) (*TokenPair, error) {
	if s.sessionService == nil {
		return nil, common.Classify(common.ErrUnavailable, errors.New("Session service is not configured"))
	}
	if user == nil {
		return nil, common.ErrUserNotFound
	}

	ttlSeconds = ClampFederatedTokenTTLSeconds(ttlSeconds)
	now := time.Now()
	accessTokenExpiry := now.Add(time.Duration(ttlSeconds) * time.Second)

	federatedSession, err := s.sessionService.CreateFederatedSession(ctx, user.ID, accessTokenExpiry, credentialID)
	if err != nil {
		return nil, err
	}

	claims := userClaims{
		ID:                    user.ID,
		Subject:               "access",
		IssuedAt:              jwt.NewNumericDate(now),
		ExpiresAt:             jwt.NewNumericDate(accessTokenExpiry),
		SessionID:             federatedSession.ID,
		UserID:                user.ID,
		Username:              user.Username,
		AppVersion:            config.Version,
		TokenType:             session.UserSessionSourceFederated,
		FederatedCredentialID: credentialID,
	}

	if user.Email != nil {
		claims.Email = *user.Email
	}
	if user.DisplayName != nil {
		claims.DisplayName = *user.DisplayName
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	accessTokenString, err := accessToken.SignedString(s.jwtSecret)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken: accessTokenString,
		ExpiresAt:   accessTokenExpiry,
	}, nil
}

func generateUsernameFromEmail(email, subject string) string {
	if email != "" {
		parts := strings.Split(email, "@")
		if len(parts) > 0 && parts[0] != "" {
			return parts[0]
		}
	}

	if len(subject) >= 8 {
		return "user_" + subject[len(subject)-8:]
	}
	return "user_" + subject
}

// uniqueOidcUsernameInternal derives a collision-free username candidate by
// suffixing the tail of the OIDC subject, mirroring generateUsernameFromEmail's
// fallback format.
func uniqueOidcUsernameInternal(base, subject string) string {
	suffix := subject
	if len(suffix) > 8 {
		suffix = suffix[len(suffix)-8:]
	}
	return base + "_" + suffix
}

func (s *AuthService) runInBackground(ctx context.Context, name string, fn func(ctx context.Context) error) {
	// Detach context to prevent cancellation when the parent request finishes
	bgCtx := context.WithoutCancel(ctx)

	go func() {
		defer func() {
			if panicErr := emperror.Recover(recover()); panicErr != nil {
				panicErr = errors.WithDetails(errors.WrapIf(panicErr, "Background task panicked"), "task", name)
				if contextHandler, ok := s.errorHandler.(emperror.ErrorHandlerContext); ok {
					contextHandler.HandleContext(bgCtx, panicErr)
				} else {
					s.errorHandler.Handle(panicErr)
				}
			}
		}()

		// Set a reasonable timeout for background tasks
		taskCtx, cancel := context.WithTimeout(bgCtx, 1*time.Minute)
		defer cancel()

		if err := fn(taskCtx); err != nil {
			slog.ErrorContext(taskCtx, "Background task failed", "task", name, "error", err)
		}
	}()
}

// ClampFederatedTokenTTLSeconds bounds a requested federated-token lifetime to
// the range Arcane will mint.
func ClampFederatedTokenTTLSeconds(ttlSeconds int) int {
	if ttlSeconds <= 0 {
		return 900
	}
	if ttlSeconds < 60 {
		return 60
	}
	if ttlSeconds > 3600 {
		return 3600
	}
	return ttlSeconds
}
