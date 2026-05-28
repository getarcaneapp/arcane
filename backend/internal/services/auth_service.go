package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/cache"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/jwtclaims"
	"github.com/getarcaneapp/arcane/types/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrUserNotFound         = errors.New("user not found")
	ErrInvalidToken         = errors.New("invalid token")
	ErrExpiredToken         = errors.New("token expired")
	ErrTokenVersionMismatch = errors.New("token version mismatch")
	ErrLocalAuthDisabled    = errors.New("local authentication is disabled")
	ErrOidcAuthDisabled     = errors.New("OIDC authentication is disabled")
)

type TokenPair struct { //nolint:gosec // API response contract intentionally includes token fields
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"` //nolint:gosec // API response contract requires refreshToken field
	ExpiresAt    time.Time `json:"expiresAt"`
}

type AuthSettings struct {
	LocalAuthEnabled bool               `json:"localAuthEnabled"`
	OidcEnabled      bool               `json:"oidcEnabled"`
	SessionTimeout   int                `json:"sessionTimeout"`
	Oidc             *models.OidcConfig `json:"oidc,omitempty"`
}

type userClaims struct {
	jwt.RegisteredClaims
	SessionID   string `json:"sid,omitempty"`
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	AppVersion  string `json:"app_version,omitempty"`
}

type refreshClaims struct {
	jwt.RegisteredClaims
	UserID     string `json:"user_id"`
	SessionID  string `json:"sid,omitempty"`
	AppVersion string `json:"app_version,omitempty"`
}

type verifiedTokenEntry struct {
	User      models.User
	SessionID string
}

type AuthService struct {
	userService     *UserService
	settingsService *SettingsService
	eventService    *EventService
	sessionService  *SessionService
	roleService     *RoleService
	jwtSecret       []byte
	refreshExpiry   time.Duration
	config          *config.Config
	// tokenCache is a per-process in-memory cache. In horizontally-scaled
	// deployments, RevokeSession / ChangePassword / InvalidateUserTokenCache
	// only purge the local instance; peers continue to accept the token until
	// their own TTL expires. The TTL is kept short to bound this window.
	tokenCache *cache.TTL[verifiedTokenEntry]
}

func NewAuthService(userService *UserService, settingsService *SettingsService, eventService *EventService, sessionService *SessionService, roleService *RoleService, jwtSecret string, cfg *config.Config) *AuthService {
	// Production managers must supply an explicit, non-default JWT_SECRET (fail
	// closed, mirroring the ENCRYPTION_KEY guard). Dev and agent mode auto-generate.
	requireExplicitSecret := cfg.Environment == config.AppEnvironmentProduction && !cfg.AgentMode
	return &AuthService{
		userService:     userService,
		settingsService: settingsService,
		eventService:    eventService,
		sessionService:  sessionService,
		roleService:     roleService,
		jwtSecret:       jwtclaims.CheckOrGenerateJwtSecret(jwtSecret, requireExplicitSecret),
		refreshExpiry:   cfg.JWTRefreshExpiry,
		config:          cfg,
		tokenCache:      cache.NewTTL[verifiedTokenEntry](15 * time.Second),
	}
}

func (s *AuthService) getAuthSettings(ctx context.Context) (*AuthSettings, error) {
	settings, err := s.settingsService.GetSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}

	timeoutMinutes, _ := s.GetSessionTimeout(ctx)

	authSettings := &AuthSettings{
		LocalAuthEnabled: settings.AuthLocalEnabled.IsTrue(),
		OidcEnabled:      settings.OidcEnabled.IsTrue(),
		SessionTimeout:   timeoutMinutes,
	}

	if authSettings.OidcEnabled {
		oidcConfig := &models.OidcConfig{
			ClientID:                    settings.OidcClientId.Value,
			ClientSecret:                settings.OidcClientSecret.Value,
			IssuerURL:                   settings.OidcIssuerUrl.Value,
			AuthorizationEndpoint:       settings.OidcAuthorizationEndpoint.Value,
			TokenEndpoint:               settings.OidcTokenEndpoint.Value,
			UserinfoEndpoint:            settings.OidcUserinfoEndpoint.Value,
			JwksURI:                     settings.OidcJwksEndpoint.Value,
			DeviceAuthorizationEndpoint: settings.OidcDeviceAuthorizationEndpoint.Value,
			Scopes:                      settings.OidcScopes.Value,
			GroupsClaim:                 settings.OidcGroupsClaim.Value,
			SkipTlsVerify:               settings.OidcSkipTlsVerify.IsTrue(),
		}

		if oidcConfig.ClientID != "" || oidcConfig.IssuerURL != "" {
			authSettings.Oidc = oidcConfig
		}
	}

	return authSettings, nil
}

func (s *AuthService) GetOidcConfigurationStatus(ctx context.Context) (*auth.OidcStatusInfo, error) {
	oidcEnvForced := s.settingsService != nil && s.settingsService.isEnvOverrideActiveInternal("oidcEnabled")

	mergeAccounts := false
	providerName := ""
	providerLogoUrl := ""
	if s.settingsService != nil {
		func() {
			defer func() {
				// In tests, a zero-valued SettingsService may panic; treat as merge disabled
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
	} else if minutes > 1440 {
		minutes = 1440
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

func (s *AuthService) GetOidcConfig(ctx context.Context) (*models.OidcConfig, error) {
	authSettings, err := s.getAuthSettings(ctx)
	if err != nil {
		return nil, err
	}

	if !authSettings.OidcEnabled || authSettings.Oidc == nil {
		return nil, ErrOidcAuthDisabled
	}

	return authSettings.Oidc, nil
}

func (s *AuthService) Login(ctx context.Context, username, password string, meta auth.SessionMeta) (*models.User, *TokenPair, error) {
	localEnabled, err := s.IsLocalAuthEnabled(ctx)
	if err != nil {
		return nil, nil, err
	}

	if !localEnabled {
		return nil, nil, ErrLocalAuthDisabled
	}

	user, err := s.userService.GetUserByUsername(ctx, username)
	if err != nil {
		if strings.Contains(err.Error(), "user not found") {
			return nil, nil, ErrInvalidCredentials
		}
		return nil, nil, err
	}

	if err := s.userService.ValidatePassword(user.PasswordHash, password); err != nil {
		return nil, nil, ErrInvalidCredentials
	}

	if s.userService.NeedsPasswordUpgrade(user.PasswordHash) {
		s.runInBackground(ctx, "upgrade_password_hash", func(ctx context.Context) error {
			if err := s.userService.UpgradePasswordHash(ctx, user.ID, password); err != nil {
				return fmt.Errorf("failed to upgrade password hash for user %s: %w", user.ID, err)
			}
			slog.InfoContext(ctx, "Successfully upgraded password hash from bcrypt to Argon2", "user", user.Username)
			return nil
		})
	}

	user.LastLogin = new(time.Now())

	// Run last login update in background
	// Use new(*user) to create a safe copy of the user struct to avoid data race
	userCopy := new(*user)
	s.runInBackground(ctx, "update_last_login", func(ctx context.Context) error {
		if _, err := s.userService.UpdateUser(ctx, userCopy); err != nil {
			return fmt.Errorf("failed to update user's last login time: %w", err)
		}
		return nil
	})

	tokenPair, err := s.createSessionAndTokensInternal(ctx, user, meta)
	if err != nil {
		return nil, nil, err
	}

	metadata := models.JSON{
		"action": "login",
		"method": "local",
	}

	// Run event logging in background
	logUserID := user.ID
	logUsername := user.Username
	s.runInBackground(ctx, "log_user_login", func(ctx context.Context) error {
		return s.eventService.LogUserEvent(ctx, models.EventTypeUserLogin, logUserID, logUsername, metadata)
	})

	return user, tokenPair, nil
}

func (s *AuthService) OidcLogin(ctx context.Context, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse, meta auth.SessionMeta) (*models.User, *TokenPair, error) {
	if userInfo.Subject == "" {
		return nil, nil, errors.New("missing OIDC subject identifier")
	}

	user, isNewUser, err := s.findOrCreateOidcUser(ctx, userInfo, tokenResp)
	if err != nil {
		return nil, nil, err
	}

	tokenPair, err := s.createSessionAndTokensInternal(ctx, user, meta)
	if err != nil {
		return nil, nil, err
	}

	metadata := models.JSON{
		"action":  "login",
		"method":  "oidc",
		"newUser": isNewUser,
		"subject": userInfo.Subject,
	}

	// Run event logging in background
	userID := user.ID
	username := user.Username
	s.runInBackground(ctx, "log_oidc_login", func(ctx context.Context) error {
		return s.eventService.LogUserEvent(ctx, models.EventTypeUserLogin, userID, username, metadata)
	})

	return user, tokenPair, nil
}

func (s *AuthService) LogLogout(ctx context.Context, user *models.User) {
	if s.eventService == nil || user == nil {
		return
	}

	metadata := models.JSON{
		"action": "logout",
	}

	userID := user.ID
	username := user.Username
	s.runInBackground(ctx, "log_user_logout", func(ctx context.Context) error {
		return s.eventService.LogUserEvent(ctx, models.EventTypeUserLogout, userID, username, metadata)
	})
}

func (s *AuthService) findOrCreateOidcUser(ctx context.Context, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) (*models.User, bool, error) {
	user, err := s.userService.GetUserByOidcSubjectId(ctx, userInfo.Subject)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
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
		return nil, false, err
	}
	return created, true, nil
}

func (s *AuthService) updateExistingOidcUser(ctx context.Context, user *models.User, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) (*models.User, bool, error) {
	if err := s.updateOidcUser(ctx, user, userInfo, tokenResp); err != nil {
		return nil, false, err
	}
	return user, false, nil
}

func (s *AuthService) tryMergeOidcUser(ctx context.Context, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) (*models.User, bool, error) {
	if userInfo.Email == "" || !s.isOidcMergeEnabled(ctx) {
		return nil, false, nil
	}

	existingUser, emailErr := s.userService.GetUserByEmail(ctx, userInfo.Email)
	if emailErr != nil {
		if errors.Is(emailErr, ErrUserNotFound) {
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

func (s *AuthService) createOidcUser(ctx context.Context, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) (*models.User, error) {
	var username string
	if userInfo.PreferredUsername == "" {
		username = generateUsernameFromEmail(userInfo.Email, userInfo.Subject)
	} else {
		username = userInfo.PreferredUsername
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

	user := &models.User{
		BaseModel:     models.BaseModel{ID: uuid.NewString()},
		Username:      username,
		DisplayName:   displayName,
		Email:         new(userInfo.Email),
		OidcSubjectId: new(userInfo.Subject),
		LastLogin:     new(time.Now()),
	}

	s.persistOidcTokens(user, tokenResp)

	if _, err := s.userService.CreateUser(ctx, user); err != nil {
		return nil, err
	}
	if err := s.syncOidcRoleAssignments(ctx, user, userInfo, tokenResp); err != nil {
		slog.WarnContext(ctx, "failed to sync OIDC role assignments on user create", "error", err, "user_id", user.ID)
	}
	return user, nil
}

func (s *AuthService) updateOidcUser(ctx context.Context, user *models.User, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) error {
	if userInfo.Name != "" && user.DisplayName == nil {
		user.DisplayName = new(userInfo.Name)
	}
	if userInfo.Email != "" && user.Email == nil {
		user.Email = new(userInfo.Email)
	}

	s.persistOidcTokens(user, tokenResp)

	user.LastLogin = new(time.Now())
	if _, err := s.userService.UpdateUser(ctx, user); err != nil {
		return err
	}
	if err := s.syncOidcRoleAssignments(ctx, user, userInfo, tokenResp); err != nil {
		slog.WarnContext(ctx, "failed to sync OIDC role assignments on user update", "error", err, "user_id", user.ID)
	}
	return nil
}

func (s *AuthService) mergeOidcWithExistingUser(ctx context.Context, user *models.User, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) error {
	// Perform the merge atomically to avoid races when multiple OIDC subjects share the same email
	merged, err := s.userService.AttachOidcSubjectTransactional(ctx, user.ID, userInfo.Subject, func(u *models.User) {
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
func (s *AuthService) syncOidcRoleAssignments(ctx context.Context, user *models.User, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) error {
	if s.roleService == nil || user == nil {
		return nil
	}

	groups := s.extractOidcGroups(ctx, userInfo, tokenResp)
	mappings, err := s.roleService.ListOidcMappings(ctx)
	if err != nil {
		return fmt.Errorf("list oidc mappings: %w", err)
	}

	groupSet := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		groupSet[g] = struct{}{}
	}

	var desired []models.UserRoleAssignment
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
		desired = append(desired, models.UserRoleAssignment{
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
		if v, ok := jwtclaims.GetByPath(userInfo.Extra, claim); ok {
			if groups := stringValuesFromClaim(v); len(groups) > 0 {
				return groups
			}
		}
		if tokenResp != nil && tokenResp.IDToken != "" {
			if parsed := jwtclaims.ParseJWTClaims(tokenResp.IDToken); parsed != nil {
				if v, ok := jwtclaims.GetByPath(parsed, claim); ok {
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

func (s *AuthService) persistOidcTokens(user *models.User, tokenResp *auth.OidcTokenResponse) {
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
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return s.jwtSecret, nil
		})
	if err != nil {
		return nil, ErrInvalidToken
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*refreshClaims)
	if !ok {
		return nil, &common.InvalidTokenClaimsError{}
	}

	if claims.Subject != "refresh" {
		return nil, &common.RefreshTokenSubjectError{}
	}

	if claims.AppVersion != "" && claims.AppVersion != config.Version {
		slog.InfoContext(ctx, "Refresh token version mismatch — rotating to current version", "tokenVersion", claims.AppVersion, "currentVersion", config.Version)
	}

	if claims.UserID == "" {
		return nil, &common.MissingTokenUserIDError{}
	}
	if claims.ID == "" {
		return nil, &common.MissingRefreshTokenIDError{}
	}
	if claims.SessionID == "" {
		return nil, &common.MissingTokenSessionIDError{}
	}
	if s.sessionService == nil {
		return nil, &common.SessionServiceUnavailableError{}
	}

	session, err := s.sessionService.GetSessionByID(ctx, claims.SessionID)
	if err != nil {
		return nil, err
	}
	if session.UserID != claims.UserID {
		return nil, ErrInvalidToken
	}
	if err := validateSessionActiveInternal(session); err != nil {
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

func (s *AuthService) VerifyToken(ctx context.Context, accessToken string) (*models.User, string, error) {
	token, err := jwt.ParseWithClaims(accessToken, &userClaims{},
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return s.jwtSecret, nil
		})
	if err != nil {
		if strings.Contains(err.Error(), "token is expired") {
			return nil, "", ErrExpiredToken
		}
		return nil, "", ErrInvalidToken
	}

	if !token.Valid {
		return nil, "", ErrInvalidToken
	}

	claims, ok := token.Claims.(*userClaims)
	if !ok {
		return nil, "", &common.InvalidTokenClaimsError{}
	}

	if claims.Subject != "access" {
		return nil, "", &common.AccessTokenSubjectError{}
	}

	if claims.ID == "" {
		return nil, "", &common.MissingTokenUserIDError{}
	}

	if claims.AppVersion != "" && claims.AppVersion != config.Version {
		slog.InfoContext(ctx, "Token version mismatch detected", "tokenVersion", claims.AppVersion, "currentVersion", config.Version, "user", claims.Username)
		return nil, "", ErrTokenVersionMismatch
	}
	if claims.SessionID == "" {
		return nil, "", &common.MissingTokenSessionIDError{}
	}
	if s.sessionService == nil {
		return nil, "", &common.SessionServiceUnavailableError{}
	}

	tokenHash := hashTokenInternal(accessToken)
	if cached, ok := s.tokenCache.Get(tokenHash); ok {
		u := cached.User
		return &u, cached.SessionID, nil
	}

	// Verify user exists in DB
	// This ensures that if the database is wiped or user is deleted, the token becomes invalid
	// even if the JWT signature is still valid (e.g. same JWT_SECRET).
	dbUser, err := s.userService.GetUserByID(ctx, claims.ID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, "", ErrInvalidToken
		}
		return nil, "", err
	}

	session, err := s.sessionService.GetSessionByID(ctx, claims.SessionID)
	if err != nil {
		return nil, "", err
	}
	if session.UserID != dbUser.ID {
		return nil, "", ErrInvalidToken
	}
	if err := validateSessionActiveInternal(session); err != nil {
		return nil, "", err
	}

	s.tokenCache.Put(tokenHash, verifiedTokenEntry{User: *dbUser, SessionID: session.ID})

	return dbUser, session.ID, nil
}

func hashTokenInternal(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *AuthService) ChangePassword(ctx context.Context, userID, currentPassword, newPassword, currentSessionID string) error {
	if s.sessionService == nil {
		return &common.SessionServiceUnavailableError{}
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

	hashedPassword, err := s.userService.hashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	user.PasswordHash = hashedPassword
	user.RequiresPasswordChange = false
	if _, err = s.userService.UpdateUser(ctx, user); err != nil {
		return err
	}
	s.tokenCache.DeleteFunc(func(_ string, e verifiedTokenEntry) bool {
		return e.User.ID == userID && e.SessionID != currentSessionID
	})
	return s.sessionService.RevokeAllUserSessionsExcept(ctx, userID, currentSessionID)
}

// InvalidateUserTokenCache purges all cached token verifications for a user.
// Call this after admin-initiated role changes, account disable, or user
// deletion so stale verifications cannot grant access for the cache TTL.
func (s *AuthService) InvalidateUserTokenCache(userID string) {
	if s.tokenCache == nil || strings.TrimSpace(userID) == "" {
		return
	}
	s.tokenCache.DeleteFunc(func(_ string, e verifiedTokenEntry) bool {
		return e.User.ID == userID
	})
}

func (s *AuthService) RevokeSession(ctx context.Context, sessionID string) error {
	if s.sessionService == nil {
		return nil
	}
	s.tokenCache.DeleteFunc(func(_ string, e verifiedTokenEntry) bool { return e.SessionID == sessionID })
	return s.sessionService.RevokeSession(ctx, sessionID)
}

// LogoutAllOtherSessions revokes every active session for userID except
// currentSessionID, so the caller stays signed in on their current device.
func (s *AuthService) LogoutAllOtherSessions(ctx context.Context, userID, currentSessionID string) error {
	if s.sessionService == nil {
		return nil
	}
	s.tokenCache.DeleteFunc(func(_ string, e verifiedTokenEntry) bool {
		return e.User.ID == userID && e.SessionID != currentSessionID
	})
	return s.sessionService.RevokeAllUserSessionsExcept(ctx, userID, currentSessionID)
}

func (s *AuthService) createSessionAndTokensInternal(ctx context.Context, user *models.User, meta auth.SessionMeta) (*TokenPair, error) {
	if s.sessionService == nil {
		return nil, &common.SessionServiceUnavailableError{}
	}
	refreshExpiry := time.Now().Add(s.refreshExpiry)
	session, refreshJTI, err := s.sessionService.CreateSession(ctx, user.ID, refreshExpiry, meta)
	if err != nil {
		return nil, err
	}
	return s.buildTokenPairInternal(ctx, user, session, refreshJTI)
}

func (s *AuthService) buildTokenPairInternal(ctx context.Context, user *models.User, session *models.UserSession, refreshJTI string) (*TokenPair, error) {
	sessionTimeout, _ := s.GetSessionTimeout(ctx)

	accessTokenExpiry := time.Now().Add(time.Duration(sessionTimeout) * time.Minute)

	userClaims := userClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        user.ID,
			Subject:   "access",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(accessTokenExpiry),
		},
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
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        refreshJTI,
			Subject:   "refresh",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(session.ExpiresAt),
		},
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

func validateSessionActiveInternal(session *models.UserSession) error {
	if session == nil {
		return ErrInvalidToken
	}
	if session.RevokedAt != nil {
		return &common.SessionRevokedError{}
	}
	if time.Now().After(session.ExpiresAt) {
		return ErrExpiredToken
	}
	return nil
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

func (s *AuthService) runInBackground(ctx context.Context, name string, fn func(ctx context.Context) error) {
	// Detach context to prevent cancellation when the parent request finishes
	bgCtx := context.WithoutCancel(ctx)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.ErrorContext(bgCtx, "Background task panicked", "task", name, "panic", r)
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
