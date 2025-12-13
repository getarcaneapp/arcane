package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
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

type TokenPair struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type AuthSettings struct {
	LocalAuthEnabled bool               `json:"localAuthEnabled"`
	OidcEnabled      bool               `json:"oidcEnabled"`
	SessionTimeout   int                `json:"sessionTimeout"`
	Oidc             *models.OidcConfig `json:"oidc,omitempty"`
}

type UserClaims struct {
	jwt.RegisteredClaims
	UserID      string   `json:"user_id"`
	Username    string   `json:"username"`
	Email       string   `json:"email,omitempty"`
	DisplayName string   `json:"display_name,omitempty"`
	Roles       []string `json:"roles"`
	AppVersion  string   `json:"app_version,omitempty"`
}

type AuthService struct {
	userService     *UserService
	settingsService *SettingsService
	eventService    *EventService
	jwtSecret       []byte
	refreshExpiry   time.Duration
	config          *config.Config
}

func NewAuthService(userService *UserService, settingsService *SettingsService, eventService *EventService, jwtSecret string, cfg *config.Config) *AuthService {
	return &AuthService{
		userService:     userService,
		settingsService: settingsService,
		eventService:    eventService,
		jwtSecret:       utils.CheckOrGenerateJwtSecret(jwtSecret),
		refreshExpiry:   7 * 24 * time.Hour,
		config:          cfg,
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
			ClientID:     settings.OidcClientId.Value,
			ClientSecret: settings.OidcClientSecret.Value,
			IssuerURL:    settings.OidcIssuerUrl.Value,
			Scopes:       settings.OidcScopes.Value,
			AdminClaim:   settings.OidcAdminClaim.Value,
			AdminValue:   settings.OidcAdminValue.Value,
		}

		if oidcConfig.ClientID != "" || oidcConfig.IssuerURL != "" {
			authSettings.Oidc = oidcConfig
		}
	}

	return authSettings, nil
}

func (s *AuthService) GetOidcConfigurationStatus(ctx context.Context) (*auth.OidcStatusInfo, error) {
	mergeAccounts := false
	if s.settingsService != nil {
		func() {
			defer func() {
				// In tests, a zero-valued SettingsService may panic; treat as merge disabled
				_ = recover()
			}()
			if settings, err := s.settingsService.GetSettings(ctx); err == nil {
				mergeAccounts = settings.OidcMergeAccounts.IsTrue()
			}
		}()
	}

	status := &auth.OidcStatusInfo{
		EnvForced:     s.config.OidcEnabled,
		MergeAccounts: mergeAccounts,
	}
	if s.config.OidcEnabled {
		status.EnvConfigured = s.config.OidcClientID != "" && s.config.OidcIssuerURL != ""
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

func (s *AuthService) Login(ctx context.Context, username, password string) (*models.User, *TokenPair, error) {
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

	now := time.Now()
	user.LastLogin = &now

	// Run last login update in background
	// Use utils.Ptr to create a safe copy of the user struct to avoid data race
	userCopy := utils.Ptr(*user)
	s.runInBackground(ctx, "update_last_login", func(ctx context.Context) error {
		if _, err := s.userService.UpdateUser(ctx, userCopy); err != nil {
			return fmt.Errorf("failed to update user's last login time: %w", err)
		}
		return nil
	})

	tokenPair, err := s.generateTokenPair(ctx, user)
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

func (s *AuthService) OidcLogin(ctx context.Context, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) (*models.User, *TokenPair, error) {
	if userInfo.Subject == "" {
		return nil, nil, errors.New("missing OIDC subject identifier")
	}

	user, isNewUser, err := s.findOrCreateOidcUser(ctx, userInfo, tokenResp)
	if err != nil {
		return nil, nil, err
	}

	tokenPair, err := s.generateTokenPair(ctx, user)
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

func (s *AuthService) findOrCreateOidcUser(ctx context.Context, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) (*models.User, bool, error) {
	user, err := s.userService.GetUserByOidcSubjectId(ctx, userInfo.Subject)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return nil, false, err
	}

	if user == nil {
		// Check if merge accounts is enabled in settings
		settings, settingsErr := s.settingsService.GetSettings(ctx)
		mergeEnabled := settingsErr == nil && settings.OidcMergeAccounts.IsTrue()

		// If merge accounts is enabled, try to find existing user by email
		if mergeEnabled && userInfo.Email != "" {
			existingUser, emailErr := s.userService.GetUserByEmail(ctx, userInfo.Email)
			if emailErr == nil && existingUser != nil {
				// Only require email verification when we are actually merging into an existing account.
				// Some providers omit `email_verified` for first-time users; that should not prevent user creation.
				if !userInfo.EmailVerified {
					return nil, false, errors.New("email not verified by OIDC provider; cannot merge accounts")
				}

				// Found existing user with matching email - merge the accounts
				slog.Info("Merging OIDC account with existing user", "email", userInfo.Email, "subject", userInfo.Subject)
				if mergeErr := s.mergeOidcWithExistingUser(ctx, existingUser, userInfo, tokenResp); mergeErr != nil {
					return nil, false, mergeErr
				}
				return existingUser, false, nil
			}
		}

		// No existing user found, create new OIDC user
		created, err := s.createOidcUser(ctx, userInfo, tokenResp)
		if err != nil {
			return nil, false, err
		}
		return created, true, nil
	}

	if err := s.updateOidcUser(ctx, user, userInfo, tokenResp); err != nil {
		return nil, false, err
	}

	return user, false, nil
}

func (s *AuthService) createOidcUser(ctx context.Context, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) (*models.User, error) {
	now := time.Now()

	var username string
	if userInfo.PreferredUsername == "" {
		username = generateUsernameFromEmail(userInfo.Email, userInfo.Subject)
	} else {
		username = userInfo.PreferredUsername
	}

	var displayName string
	switch {
	case userInfo.Name != "":
		displayName = userInfo.Name
	case userInfo.GivenName != "" || userInfo.FamilyName != "":
		displayName = strings.TrimSpace(fmt.Sprintf("%s %s", userInfo.GivenName, userInfo.FamilyName))
	default:
		displayName = username
	}

	email := userInfo.Email

	roles := models.StringSlice{"user"}
	if s.isAdminFromOidc(ctx, userInfo, tokenResp) {
		roles = append(roles, "admin")
	}

	user := &models.User{
		BaseModel:     models.BaseModel{ID: uuid.NewString()},
		Username:      username,
		DisplayName:   &displayName,
		Email:         &email,
		Roles:         roles,
		OidcSubjectId: &userInfo.Subject,
		LastLogin:     &now,
	}

	s.persistOidcTokens(user, tokenResp)

	if _, err := s.userService.CreateUser(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *AuthService) updateOidcUser(ctx context.Context, user *models.User, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) error {
	if userInfo.Name != "" && user.DisplayName == nil {
		user.DisplayName = &userInfo.Name
	}
	if userInfo.Email != "" && user.Email == nil {
		user.Email = &userInfo.Email
	}

	wantAdmin := s.isAdminFromOidc(ctx, userInfo, tokenResp)
	hasAdmin := hasRole(user.Roles, "admin")
	switch {
	case wantAdmin && !hasAdmin:
		user.Roles = addRole(user.Roles, "admin")
	case !wantAdmin && hasAdmin:
		user.Roles = removeRole(user.Roles, "admin")
	}

	s.persistOidcTokens(user, tokenResp)

	now := time.Now()
	user.LastLogin = &now
	_, err := s.userService.UpdateUser(ctx, user)
	return err
}

func (s *AuthService) mergeOidcWithExistingUser(ctx context.Context, user *models.User, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) error {
	// Perform the merge atomically to avoid races when multiple OIDC subjects share the same email
	_, err := s.userService.AttachOidcSubjectTransactional(ctx, user.ID, userInfo.Subject, func(u *models.User) {
		// Update display name if not set
		if userInfo.Name != "" && u.DisplayName == nil {
			u.DisplayName = &userInfo.Name
		}

		// Update admin role based on OIDC claims
		wantAdmin := s.isAdminFromOidc(ctx, userInfo, tokenResp)
		hasAdmin := hasRole(u.Roles, "admin")
		switch {
		case wantAdmin && !hasAdmin:
			u.Roles = addRole(u.Roles, "admin")
		case !wantAdmin && hasAdmin:
			u.Roles = removeRole(u.Roles, "admin")
		}

		// Persist OIDC tokens
		s.persistOidcTokens(u, tokenResp)

		now := time.Now()
		u.LastLogin = &now
	})
	return err
}

func hasRole(roles models.StringSlice, role string) bool {
	for _, r := range roles {
		if strings.EqualFold(r, role) {
			return true
		}
	}
	return false
}

func addRole(roles models.StringSlice, role string) models.StringSlice {
	if hasRole(roles, role) {
		return roles
	}
	return append(roles, role)
}

func removeRole(roles models.StringSlice, role string) models.StringSlice {
	out := make(models.StringSlice, 0, len(roles))
	for _, r := range roles {
		if !strings.EqualFold(r, role) {
			out = append(out, r)
		}
	}
	return out
}

func (s *AuthService) isAdminFromOidc(ctx context.Context, userInfo auth.OidcUserInfo, tokenResp *auth.OidcTokenResponse) bool {
	claimKey, values := s.getAdminClaimConfig(ctx)
	if claimKey == "" {
		return false
	}

	if v, ok := utils.GetByPath(userInfo.Extra, claimKey); ok && utils.EvalMatch(v, values) {
		return true
	}

	if tokenResp != nil && tokenResp.IDToken != "" {
		if claims := utils.ParseJWTClaims(tokenResp.IDToken); claims != nil {
			if v, ok := utils.GetByPath(claims, claimKey); ok && utils.EvalMatch(v, values) {
				return true
			}
		}
	}

	return false
}

func (s *AuthService) getAdminClaimConfig(ctx context.Context) (claim string, values []string) {
	as, err := s.getAuthSettings(ctx)
	if err != nil || as.Oidc == nil {
		return "", nil
	}
	claim = strings.TrimSpace(as.Oidc.AdminClaim)
	raw := strings.TrimSpace(as.Oidc.AdminValue)
	if claim == "" {
		return "", nil
	}
	if raw == "" {
		return claim, nil
	}
	parts := strings.Split(raw, ",")
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v != "" {
			values = append(values, v)
		}
	}
	return claim, values
}

func (s *AuthService) persistOidcTokens(user *models.User, tokenResp *auth.OidcTokenResponse) {
	if tokenResp == nil {
		return
	}
	if tokenResp.AccessToken != "" {
		user.OidcAccessToken = &tokenResp.AccessToken
	}
	if tokenResp.RefreshToken != "" {
		user.OidcRefreshToken = &tokenResp.RefreshToken
	}
	if tokenResp.ExpiresIn > 0 {
		expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		user.OidcAccessTokenExpiresAt = &expiresAt
	}
}

func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error) {
	token, err := jwt.ParseWithClaims(refreshToken, &jwt.RegisteredClaims{},
		func(t *jwt.Token) (interface{}, error) {
			return s.jwtSecret, nil
		})

	if err != nil {
		return nil, ErrInvalidToken
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}

	if claims.Subject != "refresh" {
		return nil, errors.New("not a refresh token")
	}

	userId := claims.ID
	if userId == "" {
		return nil, errors.New("missing user ID in token")
	}

	user, err := s.userService.GetUserByID(ctx, userId)
	if err != nil {
		return nil, err
	}

	tokenPair, err := s.generateTokenPair(ctx, user)
	if err != nil {
		return nil, err
	}

	return tokenPair, nil
}

func (s *AuthService) VerifyToken(ctx context.Context, accessToken string) (*models.User, error) {
	token, err := jwt.ParseWithClaims(accessToken, &UserClaims{},
		func(t *jwt.Token) (interface{}, error) {
			return s.jwtSecret, nil
		})

	if err != nil {
		if strings.Contains(err.Error(), "token is expired") {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*UserClaims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}

	if claims.Subject != "access" {
		return nil, errors.New("not an access token")
	}

	if claims.ID == "" {
		return nil, errors.New("missing user ID in token")
	}

	if claims.AppVersion != "" && claims.AppVersion != config.Version {
		slog.InfoContext(ctx, "Token version mismatch detected",
			"tokenVersion", claims.AppVersion,
			"currentVersion", config.Version,
			"user", claims.Username)
		return nil, ErrTokenVersionMismatch
	}

	user := &models.User{
		BaseModel: models.BaseModel{ID: claims.ID},
		Username:  claims.Username,
		Roles:     models.StringSlice(claims.Roles),
	}

	if claims.Email != "" {
		user.Email = &claims.Email
	}

	if claims.DisplayName != "" {
		user.DisplayName = &claims.DisplayName
	}

	return user, nil
}

func (s *AuthService) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
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
	_, err = s.userService.UpdateUser(ctx, user)
	return err
}

func (s *AuthService) generateTokenPair(ctx context.Context, user *models.User) (*TokenPair, error) {
	sessionTimeout, _ := s.GetSessionTimeout(ctx)

	accessTokenExpiry := time.Now().Add(time.Duration(sessionTimeout) * time.Minute)
	slog.WarnContext(ctx, "accessTokenExpiry", "expiry", accessTokenExpiry)

	userClaims := UserClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        user.ID,
			Subject:   "access",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(accessTokenExpiry),
		},
		UserID:     user.ID,
		Username:   user.Username,
		Roles:      []string(user.Roles),
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

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		ID:        user.ID,
		Subject:   "refresh",
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.refreshExpiry)),
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
