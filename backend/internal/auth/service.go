package auth

import (
	"cmp"
	"context"
	"crypto/mldsa"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/lestrrat-go/jwx/v4/jwa"
	"github.com/lestrrat-go/jwx/v4/jwt"
	"go.uber.org/fx"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"

	kit "kit.getarcane.app"
	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/normalization"

	"getarcane.dev/arcane/v3/buildables"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/event"
	usergroup "getarcane.dev/arcane/v3/internal/group"
	"getarcane.dev/arcane/v3/internal/ldap"
	"getarcane.dev/arcane/v3/internal/role"
	"getarcane.dev/arcane/v3/internal/session"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/user"
	"getarcane.dev/arcane/v3/internal/version"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/cookie"
	"getarcane.dev/arcane/v3/pkg/oidc"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/types/v3"
)

type (
	TokenPair struct {
		ExpiresAt        time.Time `json:"expiresAt"`
		RefreshExpiresAt time.Time `json:"-"`
		AccessToken      string    `json:"accessToken"`
		BrowserToken     string    `json:"-"`
		RefreshToken     string    `json:"refreshToken"`
		BrowserRefresh   string    `json:"-"`
	}

	AuthService struct {
		db                *database.DB
		userService       *user.UserService
		settingsService   *settings.SettingsService
		eventService      *event.EventService
		sessionService    *session.SessionService
		roleService       *role.RoleService
		ldapService       *ldap.LdapService
		environmentLister *environment.Lister
		versionService    *version.VersionService
		signingKey        *mldsa.PrivateKey
		config            *config.Config
		tokens            *TokenCache
		dummyHash         string
		browserSigningKey []byte
		refreshExpiry     time.Duration
		signingKeyMu      sync.Mutex
		dummyMu           sync.Mutex
	}

	AuthServiceParams struct {
		fx.In

		DB                *database.DB
		UserService       *user.UserService
		SettingsService   *settings.SettingsService
		EventService      *event.EventService
		SessionService    *session.SessionService
		RoleService       *role.RoleService
		LdapService       *ldap.LdapService
		EnvironmentLister *environment.Lister
		TokenCache        *TokenCache
		VersionService    *version.VersionService
		Config            *config.Config
	}

	ChangePasswordRequest struct {
		UserID           string
		CurrentPassword  string
		NewPassword      string
		CurrentSessionID string
	}
)

const (
	sessionBootstrapVersionTimeout = 2 * time.Second
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrLocalAuthDisabled  = errors.New("local authentication is disabled")
	ErrOidcAuthDisabled   = errors.New("OIDC authentication is disabled")
	ErrMFARequired        = errors.New("multi-factor authentication is required")
	ErrUserDisabled       = common.Classify(common.ErrForbidden, errors.New("user account is disabled"))

	ErrPasswordChangeRequired = common.Classify(common.ErrForbidden, errors.New("password change required"))

	passwordChangeAllowedOperations = []string{"get-current-user", "get-session-bootstrap", "get-permissions-manifest", "change-password", "logout"}
)

func (p *TokenPair) CookieHeaders(secure bool) []string {
	return append(
		cookie.BuildTokenCookieStringFor(max(int(time.Until(p.ExpiresAt).Seconds()), 0)+60, p.BrowserToken, secure),
		cookie.BuildRefreshCookieStringFor(int(time.Until(p.RefreshExpiresAt).Seconds()), p.BrowserRefresh, secure),
	)
}

func (s *AuthService) dummyPasswordHash(ctx context.Context) (string, error) {
	s.dummyMu.Lock()
	defer s.dummyMu.Unlock()
	if s.dummyHash != "" {
		return s.dummyHash, nil
	}
	hash, err := s.userService.HashPassword(ctx, uuid.New().String())
	if err != nil {
		return "", fmt.Errorf("failed to prepare dummy password hash: %w", err)
	}
	s.dummyHash = hash
	return hash, nil
}

func NewAuthService(p AuthServiceParams) *AuthService {
	return &AuthService{
		db:                p.DB,
		userService:       p.UserService,
		settingsService:   p.SettingsService,
		eventService:      p.EventService,
		sessionService:    p.SessionService,
		roleService:       p.RoleService,
		ldapService:       p.LdapService,
		environmentLister: p.EnvironmentLister,
		versionService:    p.VersionService,
		refreshExpiry:     p.Config.JWTRefreshExpiry,
		config:            p.Config,
		tokens:            p.TokenCache,
	}
}

func (s *AuthService) GetSessionBootstrap(ctx context.Context, userID string, ps *authz.PermissionSet) (*types.SessionBootstrap, error) {
	userModel, getUserErr := s.userService.GetUser(ctx, userID)
	if getUserErr != nil {
		return nil, fmt.Errorf("failed to get user: %w", getUserErr)
	}
	var (
		currentUser  types.User
		versionInfo  *types.VersionInfo
		environments types.Paginated[types.Environment]
	)
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		var err error
		currentUser, err = s.userService.ToUserResponseDto(groupCtx, *userModel)
		if err != nil {
			return fmt.Errorf("failed to map user: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		versionCtx, cancel := context.WithTimeout(groupCtx, sessionBootstrapVersionTimeout)
		defer cancel()
		versionInfo = s.versionService.GetAppVersionInfo(versionCtx)
		return nil
	})
	group.Go(func() error {
		if s.RequirePasswordChanged(userModel, "") != nil {
			return nil
		}
		var err error
		environments, err = s.environmentLister.ListAccessibleEnvironments(groupCtx, ps, pagination.QueryParams{Limit: -1})
		if err != nil {
			return fmt.Errorf("failed to list environments: %w", err)
		}
		return nil
	})
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return &types.SessionBootstrap{
		User:         currentUser,
		Version:      *versionInfo,
		Environments: environments,
	}, nil
}

func (s *AuthService) GetOidcConfigurationStatus(_ context.Context) (*types.OidcStatusInfo, error) {
	localSettings := s.settingsService.GetSettingsConfig()
	return &types.OidcStatusInfo{
		MergeAccounts:   localSettings.OidcMergeAccounts.IsTrue(),
		ProviderName:    localSettings.OidcProviderName.Value,
		ProviderLogoUrl: localSettings.OidcProviderLogoUrl.Value,
	}, nil
}

func (s *AuthService) IsOidcEnabled() bool {
	return s.settingsService.GetSettingsConfig().OidcEnabled.IsTrue()
}

func (s *AuthService) GetOidcConfig(ctx context.Context) (*settings.OidcConfig, error) {
	appSettings := s.settingsService.GetSettingsConfig()
	if !appSettings.OidcEnabled.IsTrue() || (appSettings.OidcClientId.Value == "" && appSettings.OidcIssuerUrl.Value == "") {
		return nil, ErrOidcAuthDisabled
	}

	return &settings.OidcConfig{
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
	}, nil
}

func (s *AuthService) AuthenticateLocalPrimary(ctx context.Context, username, password string) (*user.User, error) {
	localSettings := s.settingsService.GetSettingsConfig()
	localEnabled := localSettings.AuthLocalEnabled.IsTrue()
	ldapEnabled := localSettings.LdapEnabled.IsTrue()
	if !localEnabled && !ldapEnabled {
		return nil, ErrLocalAuthDisabled
	}

	localUser, err := s.userService.GetUserByUsername(ctx, username)
	if err != nil && !errors.Is(err, common.ErrUserNotFound) {
		return nil, err
	}
	if strings.Contains(username, "@") {
		emailUser, emailErr := s.userService.GetUserByEmail(ctx, username)
		switch {
		case errors.Is(emailErr, common.ErrAmbiguousUserEmail):
			slog.WarnContext(ctx, "Rejecting email login: multiple accounts share this email", "email", username)
			return nil, ErrInvalidCredentials
		case emailErr != nil && !errors.Is(emailErr, common.ErrUserNotFound):
			return nil, emailErr
		case emailErr == nil && localUser != nil && emailUser.ID != localUser.ID:
			slog.WarnContext(ctx, "Rejecting login: identifier matches one account's username and a different account's email", "identifier", username)
			return nil, ErrInvalidCredentials
		case emailErr == nil && localUser == nil:
			localUser = emailUser
		}
	}

	ldapID := kit.Ternary(ldapEnabled, kit.FromPtr(localUser).LdapID, nil)
	localHash := kit.Ternary(localEnabled && ldapID == nil, kit.FromPtr(localUser).PasswordHash, "")
	dummyHash, err := s.dummyPasswordHash(ctx)
	if err != nil {
		return nil, err
	}
	passwordErr := s.userService.ValidatePassword(ctx, cmp.Or(localHash, dummyHash), password)
	if ldapID != nil {
		passwordErr = s.ldapService.Authenticate(ctx, *ldapID, password)
		if passwordErr != nil && !errors.Is(passwordErr, ldap.ErrInvalidCredentials) {
			slog.ErrorContext(ctx, "LDAP login failed", "username", localUser.Username, "error", passwordErr)
		}
	}
	if passwordErr != nil || (ldapID == nil && localHash == "") {
		return nil, ErrInvalidCredentials
	}
	if localUser.Disabled {
		return nil, ErrUserDisabled
	}

	userID := localUser.ID
	now := time.Now()
	localUser.LastLogin = &now
	s.runInBackground(ctx, "update_last_login", func(ctx context.Context) error {
		if touchErr := s.userService.TouchLastLogin(ctx, userID, now); touchErr != nil {
			return fmt.Errorf("failed to update user's last login time: %w", touchErr)
		}
		return nil
	})

	return localUser, nil
}

func (s *AuthService) PrepareOidcLogin(ctx context.Context, userInfo types.OidcUserInfo, tokenResp *types.OidcTokenResponse) (*user.User, bool, error) {
	if userInfo.Subject == "" {
		return nil, false, errors.New("missing OIDC subject identifier")
	}
	return s.findOrCreateOidcUser(ctx, userInfo, tokenResp)
}

func (s *AuthService) CompleteLogin(ctx context.Context, localUser *user.User, meta types.SessionMeta, source, mfaMethod string, eventMetadata ...database.JSON) (*TokenPair, error) {
	if localUser == nil {
		return nil, common.ErrUserNotFound
	}
	if localUser.Disabled {
		return nil, ErrUserDisabled
	}
	source = cmp.Or(strings.TrimSpace(source), session.UserSessionSourceLocal)
	mfaMethod = strings.TrimSpace(mfaMethod)
	if localUser.PasskeyMFAEnabled && source != session.UserSessionSourcePasskey && mfaMethod != session.PasskeyMFAMethod && mfaMethod != session.RecoveryCodeMFAMethod {
		return nil, ErrMFARequired
	}
	sessionMeta := types.SessionMeta{
		UserAgent:     meta.UserAgent,
		IPAddress:     meta.IPAddress,
		Source:        source,
		MFAMethod:     mfaMethod,
		MFAVerifiedAt: cmp.Or(meta.MFAVerifiedAt, kit.Ternary(mfaMethod != "", new(time.Now()), nil)),
	}

	localSession, refreshJTI, err := s.sessionService.CreateSession(ctx, localUser.ID, time.Now().Add(s.refreshExpiry), sessionMeta)
	if err != nil {
		return nil, err
	}
	tokenPair, err := s.buildTokenPair(ctx, localUser, localSession, refreshJTI)
	if err != nil {
		return nil, err
	}

	metadata := database.JSON{"action": "login", "method": source}
	if len(eventMetadata) > 0 {
		maps.Copy(metadata, eventMetadata[0])
	}
	if mfaMethod != "" {
		metadata["mfa"] = mfaMethod
	}
	logUserID := localUser.ID
	logUsername := localUser.Username
	s.runInBackground(ctx, "log_user_login", func(ctx context.Context) error {
		return s.eventService.LogUserEvent(ctx, event.EventTypeUserLogin, logUserID, logUsername, metadata)
	})

	return tokenPair, nil
}

func (s *AuthService) Login(ctx context.Context, username, password string, meta types.SessionMeta) (*user.User, *TokenPair, error) {
	localUser, err := s.AuthenticateLocalPrimary(ctx, username, password)
	if err != nil {
		return nil, nil, err
	}
	if localUser.PasskeyMFAEnabled {
		return nil, nil, ErrMFARequired
	}
	tokenPair, err := s.CompleteLogin(ctx, localUser, meta, session.UserSessionSourceLocal, "")
	if err != nil {
		return nil, nil, err
	}
	return localUser, tokenPair, nil
}

func (s *AuthService) LogLogout(ctx context.Context, localUser *user.User) {
	if localUser == nil {
		return
	}

	metadata := database.JSON{
		"action": "logout",
	}

	userID := localUser.ID
	username := localUser.Username
	s.runInBackground(ctx, "log_user_logout", func(ctx context.Context) error {
		return s.eventService.LogUserEvent(ctx, event.EventTypeUserLogout, userID, username, metadata)
	})
}

func (s *AuthService) findOrCreateOidcUser(ctx context.Context, userInfo types.OidcUserInfo, tokenResp *types.OidcTokenResponse) (*user.User, bool, error) {
	if err := normalization.Normalize(&userInfo); err != nil {
		return nil, false, err
	}
	localUser, err := s.userService.GetUserByOidcSubjectId(ctx, userInfo.Subject)
	if err != nil && !errors.Is(err, common.ErrUserNotFound) {
		return nil, false, err
	}

	if localUser != nil {
		updated, updateErr := s.updateOidcUser(ctx, localUser, userInfo, tokenResp)
		return updated, false, updateErr
	}

	if userInfo.Email != "" && s.settingsService.GetSettingsConfig().OidcMergeAccounts.IsTrue() {
		existingUser, emailErr := s.userService.GetUserByEmail(ctx, userInfo.Email)
		switch {
		case emailErr != nil && !errors.Is(emailErr, common.ErrUserNotFound):
			return nil, false, emailErr
		case existingUser == nil:
		case !userInfo.EmailVerified:
			// Only a provider-verified email may claim an existing account.
			return nil, false, common.Classify(common.ErrForbidden, errors.New("email not verified by OIDC provider; cannot merge accounts"))
		default:
			slog.InfoContext(ctx, "Merging OIDC account with existing user", "email", userInfo.Email, "subject", userInfo.Subject)
			// Perform the merge atomically to avoid races when multiple OIDC subjects share the same email
			merged, mergeErr := s.userService.AttachOidcSubjectTransactional(ctx, existingUser.ID, userInfo.Subject, func(u *user.User) {
				if userInfo.Name != "" && u.DisplayName == nil {
					u.DisplayName = new(userInfo.Name)
				}
				s.persistOidcTokens(u, tokenResp)
				u.LastLogin = new(time.Now())
			})
			if mergeErr != nil {
				return nil, false, mergeErr
			}
			if syncErr := s.syncOidcRoleAssignments(ctx, merged, userInfo, tokenResp); syncErr != nil {
				return nil, false, fmt.Errorf("failed to sync OIDC role assignments on user merge: %w", syncErr)
			}
			return merged, false, nil
		}
	}

	created, err := s.createOidcUser(ctx, userInfo, tokenResp)
	if err != nil {
		// A concurrent login for the same subject may have created the
		// account between our lookup and the insert; resolve to it
		// instead of failing the login.
		if existing, lookupErr := s.userService.GetUserByOidcSubjectId(ctx, userInfo.Subject); lookupErr == nil {
			updated, updateErr := s.updateOidcUser(ctx, existing, userInfo, tokenResp)
			return updated, false, updateErr
		}
		return nil, false, err
	}
	return created, true, nil
}

func (s *AuthService) createOidcUser(ctx context.Context, userInfo types.OidcUserInfo, tokenResp *types.OidcTokenResponse) (*user.User, error) {
	username, err := s.resolveOidcUsername(ctx, userInfo)
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

	localUser := &user.User{
		ID:            uuid.New().String(),
		Username:      username,
		DisplayName:   displayName,
		Email:         new(userInfo.Email),
		OidcSubjectId: new(userInfo.Subject),
		LastLogin:     new(time.Now()),
	}

	s.persistOidcTokens(localUser, tokenResp)

	var createErr error
	for attempt := range 3 {
		if attempt > 0 {
			if localUser.Username, err = s.resolveOidcUsername(ctx, userInfo); err != nil {
				return nil, err
			}
		}
		if _, createErr = s.userService.CreateUser(ctx, localUser); createErr == nil {
			break
		}
	}
	if createErr != nil {
		return nil, createErr
	}
	if syncOidcRoleAssignmentsErr := s.syncOidcRoleAssignments(ctx, localUser, userInfo, tokenResp); syncOidcRoleAssignmentsErr != nil {
		return nil, fmt.Errorf("failed to sync OIDC role assignments on user create: %w", syncOidcRoleAssignmentsErr)
	}
	return localUser, nil
}

func (s *AuthService) resolveOidcUsername(ctx context.Context, userInfo types.OidcUserInfo) (string, error) {
	// CreateUser trims usernames; check availability using the same value.
	username := strings.TrimSpace(cmp.Or(userInfo.PreferredUsername, generateUsernameFromEmail(userInfo.Email, userInfo.Subject)))

	if _, lookupErr := s.userService.GetUserByUsername(ctx, username); errors.Is(lookupErr, common.ErrUserNotFound) {
		return username, nil
	} else if lookupErr != nil {
		return "", lookupErr
	}

	base := uniqueOidcUsername(username, userInfo.Subject)
	unique := base
	for i := 2; ; i++ {
		_, candidateErr := s.userService.GetUserByUsername(ctx, unique)
		if errors.Is(candidateErr, common.ErrUserNotFound) {
			slog.InfoContext(ctx, "OIDC username already taken by an existing account; assigning a unique username instead",
				"username", username, "uniqueUsername", unique, "subject", userInfo.Subject)
			return unique, nil
		}
		if candidateErr != nil {
			return "", candidateErr
		}
		unique = base + "_" + strconv.Itoa(i)
	}
}

func (s *AuthService) updateOidcUser(ctx context.Context, localUser *user.User, userInfo types.OidcUserInfo, tokenResp *types.OidcTokenResponse) (*user.User, error) {
	updated, err := s.userService.AttachOidcSubjectTransactional(ctx, localUser.ID, userInfo.Subject, func(u *user.User) {
		if userInfo.Name != "" && u.DisplayName == nil {
			u.DisplayName = new(userInfo.Name)
		}
		if userInfo.Email != "" && u.Email == nil {
			u.Email = new(userInfo.Email)
		}
		s.persistOidcTokens(u, tokenResp)
		u.LastLogin = new(time.Now())
	})
	if err != nil {
		return nil, err
	}
	if syncErr := s.syncOidcRoleAssignments(ctx, updated, userInfo, tokenResp); syncErr != nil {
		return nil, fmt.Errorf("failed to sync OIDC role assignments on user update: %w", syncErr)
	}
	return updated, nil
}

// Manual assignments are untouched.
func (s *AuthService) syncOidcRoleAssignments(ctx context.Context, localUser *user.User, userInfo types.OidcUserInfo, tokenResp *types.OidcTokenResponse) error {
	if localUser == nil {
		return nil
	}

	groups := s.extractOidcGroups(ctx, userInfo, tokenResp)
	mappings, err := s.roleService.ListOidcMappings(ctx)
	if err != nil {
		return fmt.Errorf("list oidc mappings: %w", err)
	}

	err = s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if assignErr := s.roleService.ReplaceUserAssignmentsInDB(ctx, tx, localUser.ID, role.RoleAssignmentSourceOidc, role.MappedAssignments(mappings, groups)); assignErr != nil {
			return assignErr
		}
		claimGroups, loadErr := usergroup.LoadClaimGroupsInDB(ctx, tx)
		if loadErr != nil {
			return loadErr
		}
		if groupErr := usergroup.SyncUserMembershipsInDB(ctx, tx, usergroup.MembershipSync{
			ClaimGroups: claimGroups,
			UserID:      localUser.ID,
			Source:      usergroup.SourceOidc,
			Claims:      groups,
		}); groupErr != nil {
			return groupErr
		}
		return s.roleService.AssertGlobalAdminExists(ctx, tx)
	})
	if errors.Is(err, common.ErrNoGlobalAdminRemains) {
		slog.WarnContext(ctx, "OIDC role sync would remove the last global admin; keeping existing assignments", "userId", localUser.ID)
		return nil
	}
	if err != nil {
		return err
	}
	s.roleService.InvalidateUser(localUser.ID)
	return nil
}

func (s *AuthService) extractOidcGroups(_ context.Context, userInfo types.OidcUserInfo, tokenResp *types.OidcTokenResponse) []string {
	claim := cmp.Or(strings.TrimSpace(s.settingsService.GetSettingsConfig().OidcGroupsClaim.Value), "groups")

	if v, ok := oidc.GetByPath(userInfo.Extra, claim).Get(); ok {
		if groups := oidc.GetStringSliceClaim(map[string]any{"groups": v}, "groups"); len(groups) > 0 {
			return groups
		}
	}
	if tokenResp != nil && tokenResp.IDToken != "" {
		if parsed := oidc.ParseJWTClaims(tokenResp.IDToken); parsed != nil {
			if v, ok := oidc.GetByPath(parsed, claim).Get(); ok {
				if groups := oidc.GetStringSliceClaim(map[string]any{"groups": v}, "groups"); len(groups) > 0 {
					return groups
				}
			}
		}
	}

	return userInfo.Groups
}

func (s *AuthService) persistOidcTokens(localUser *user.User, tokenResp *types.OidcTokenResponse) {
	if tokenResp == nil {
		return
	}
	if tokenResp.AccessToken != "" {
		localUser.OidcAccessToken = new(tokenResp.AccessToken)
	}
	if tokenResp.RefreshToken != "" {
		localUser.OidcRefreshToken = new(tokenResp.RefreshToken)
	}
	if tokenResp.ExpiresIn > 0 {
		localUser.OidcAccessTokenExpiresAt = new(time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second))
	}
}

func (s *AuthService) WithSigningKey(key *mldsa.PrivateKey) *AuthService {
	s.signingKey = key
	return s
}

func (s *AuthService) loadSigningKey(ctx context.Context) (*mldsa.PrivateKey, error) {
	s.signingKeyMu.Lock()
	defer s.signingKeyMu.Unlock()
	if s.signingKey != nil {
		return s.signingKey, nil
	}
	key, err := s.settingsService.EnsureJwtSigningKey(ctx)
	if err != nil {
		return nil, err
	}
	s.signingKey = key
	return key, nil
}

func (s *AuthService) loadBrowserSigningKey(ctx context.Context) ([]byte, error) {
	s.signingKeyMu.Lock()
	defer s.signingKeyMu.Unlock()
	if len(s.browserSigningKey) == browserSessionSigningKeySize {
		return s.browserSigningKey, nil
	}
	key, err := s.settingsService.EnsureBrowserSessionSigningKey(ctx)
	if err != nil {
		return nil, err
	}
	if len(key) != browserSessionSigningKeySize {
		return nil, common.Classify(common.ErrUnavailable, errors.New("browser session signing key has invalid length"))
	}
	s.browserSigningKey = key
	return key, nil
}

func (s *AuthService) parseAnyRefreshToken(ctx context.Context, refreshToken string) (*refreshTokenClaims, error) {
	if algorithm, _ := signedTokenAlgorithm(refreshToken); algorithm == jwa.HS512().String() {
		browserKey, err := s.loadBrowserSigningKey(ctx)
		if err != nil {
			return nil, err
		}
		return parseRefreshToken(ctx, refreshToken, browserRefreshSubject, jwt.WithKey(jwa.HS512(), browserKey))
	}
	signingKey, err := s.loadSigningKey(ctx)
	if err != nil {
		return nil, err
	}
	return parseRefreshToken(ctx, refreshToken, refreshTokenSubject, jwt.WithKey(jwa.MLDSA87(), signingKey.PublicKey()))
}

func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string, meta types.SessionMeta) (*TokenPair, error) {
	claims, err := s.parseAnyRefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}

	if claims.AppVersion != "" && claims.AppVersion != config.Version {
		slog.InfoContext(ctx, "Refresh token version mismatch — rotating to current version", "tokenVersion", claims.AppVersion, "currentVersion", config.Version)
	}

	userSession, err := s.sessionService.GetSessionByID(ctx, claims.SessionID)
	if err != nil {
		return nil, err
	}
	if userSession.UserID != claims.UserID {
		return nil, common.ErrInvalidToken
	}
	if validateActiveErr := session.ValidateActive(userSession); validateActiveErr != nil {
		return nil, validateActiveErr
	}

	localUser, err := s.userService.GetUserByID(ctx, claims.UserID)
	if err != nil {
		return nil, err
	}
	if localUser.Disabled {
		return nil, common.ErrInvalidToken
	}

	rotatedSession, refreshJTI, err := s.sessionService.RotateRefreshToken(ctx, claims.SessionID, claims.ID, meta)
	if err != nil {
		return nil, err
	}

	return s.buildTokenPair(ctx, localUser, rotatedSession, refreshJTI)
}

func (s *AuthService) VerifyToken(ctx context.Context, accessToken string) (*user.User, string, error) {
	tokenHash := kit.SHA256Hex(accessToken)
	if localUser, sessionID, ok := s.tokens.lookup(tokenHash); ok {
		return localUser, sessionID, nil
	}
	signingKey, err := s.loadSigningKey(ctx)
	if err != nil {
		return nil, "", err
	}
	claims, err := parseAccessToken(ctx, accessToken, signingKey.PublicKey())
	if err != nil {
		return nil, "", err
	}
	return s.verifyTokenClaims(ctx, tokenHash, claims)
}

func (s *AuthService) VerifyBrowserToken(ctx context.Context, browserToken string) (*user.User, string, error) {
	tokenHash := "browser:" + kit.SHA256Hex(browserToken)
	if localUser, sessionID, ok := s.tokens.lookup(tokenHash); ok {
		return localUser, sessionID, nil
	}
	algorithm, ok := signedTokenAlgorithm(browserToken)
	if !ok {
		return nil, "", common.ErrInvalidToken
	}
	switch algorithm {
	case jwa.HS512().String():
		key, err := s.loadBrowserSigningKey(ctx)
		if err != nil {
			return nil, "", err
		}
		claims, err := parseBrowserToken(ctx, browserToken, key)
		if err != nil {
			return nil, "", err
		}
		return s.verifyTokenClaims(ctx, tokenHash, claims)
	case jwa.MLDSA87().String():
		return s.VerifyToken(ctx, browserToken)
	default:
		return nil, "", common.ErrInvalidToken
	}
}

func (s *AuthService) verifyTokenClaims(ctx context.Context, tokenHash string, claims *accessTokenClaims) (*user.User, string, error) {
	gen := s.tokens.gen.Load()
	if claims.AppVersion != "" && claims.AppVersion != config.Version {
		slog.InfoContext(ctx, "Token version mismatch detected", "tokenVersion", claims.AppVersion, "currentVersion", config.Version, "user", claims.Username)
		return nil, "", common.ErrTokenVersionMismatch
	}

	dbUser, err := s.userService.GetUserByID(ctx, claims.UserID)
	if err != nil {
		return nil, "", kit.Ternary[error](errors.Is(err, common.ErrUserNotFound), common.ErrInvalidToken, err)
	}
	if dbUser.Disabled {
		return nil, "", common.ErrInvalidToken
	}

	userSession, err := s.sessionService.GetSessionByID(ctx, claims.SessionID)
	if err != nil {
		return nil, "", err
	}
	if userSession.UserID != dbUser.ID {
		return nil, "", common.ErrInvalidToken
	}
	if validateActiveErr := session.ValidateActive(userSession); validateActiveErr != nil {
		return nil, "", validateActiveErr
	}

	s.tokens.store(gen, tokenHash, verifiedTokenEntry{
		User:             *dbUser,
		SessionID:        userSession.ID,
		TokenExpiresAt:   claims.ExpiresAt,
		SessionExpiresAt: userSession.ExpiresAt,
	})

	return dbUser, userSession.ID, nil
}

// CheckSessionActive fails once the session is revoked or expired, or its user is disabled or deleted.
func (s *AuthService) CheckSessionActive(ctx context.Context, userID, sessionID string) error {
	localUser, err := s.userService.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if localUser.Disabled {
		return ErrUserDisabled
	}
	userSession, err := s.sessionService.GetSessionByID(ctx, sessionID)
	if err != nil {
		return err
	}
	return session.ValidateActive(userSession)
}

func (s *AuthService) RequirePasswordChanged(localUser *user.User, operationID string) error {
	if !localUser.RequiresPasswordChange || buildables.HasBuildFeature("autologin") || slices.Contains(passwordChangeAllowedOperations, operationID) {
		return nil
	}
	return ErrPasswordChangeRequired
}

func (s *AuthService) ChangePassword(ctx context.Context, req ChangePasswordRequest) error {
	localUser, err := s.userService.GetUserByID(ctx, req.UserID)
	if err != nil {
		return err
	}

	if localUser.PasswordHash == "" && kit.FromPtr(localUser.OidcSubjectId) != "" {
		return common.Classify(common.ErrForbidden, errors.New("local passwords cannot be set for accounts managed by an identity provider"))
	}
	if localUser.PasswordHash != "" {
		if validatePasswordErr := s.userService.ValidatePassword(ctx, localUser.PasswordHash, req.CurrentPassword); validatePasswordErr != nil {
			return ErrInvalidCredentials
		}
	}

	if validatePasswordPolicyErr := s.settingsService.GetSettingsConfig().PasswordPolicy().Validate(req.NewPassword); validatePasswordPolicyErr != nil {
		return common.Classify(common.ErrValidation, &types.FieldError{Field: "newPassword", Err: validatePasswordPolicyErr})
	}

	if _, err = s.userService.SetPasswordAndRevokeSessionsExcept(ctx, localUser, req.NewPassword, req.CurrentSessionID, nil); err != nil {
		return err
	}
	// The current session's cached user still carries the old password flags.
	s.tokens.InvalidateUser(req.UserID)
	return nil
}

func (s *AuthService) RevokeSession(ctx context.Context, sessionID string) error {
	if err := s.sessionService.RevokeSession(ctx, sessionID); err != nil {
		return err
	}
	s.tokens.invalidate(func(entry verifiedTokenEntry) bool { return entry.SessionID == sessionID })
	return nil
}

func (s *AuthService) RevokeSessionByRefreshToken(ctx context.Context, refreshToken string) error {
	claims, err := s.parseAnyRefreshToken(ctx, refreshToken)
	if err != nil {
		return err
	}
	return s.RevokeSession(ctx, claims.SessionID)
}

func (s *AuthService) LogoutAllOtherSessions(ctx context.Context, userID, currentSessionID string) error {
	if err := s.sessionService.RevokeAllUserSessionsExcept(ctx, userID, currentSessionID); err != nil {
		return err
	}
	s.tokens.invalidate(func(entry verifiedTokenEntry) bool {
		return entry.User.ID == userID && entry.SessionID != currentSessionID
	})
	return nil
}

func signSessionJWT(jwtID, subject, userID, sessionID string, issuedAt, expiresAt time.Time, extra map[string]any, signOption jwt.SignOption) (string, error) {
	builder := jwt.NewBuilder().
		JwtID(jwtID).
		Subject(subject).
		IssuedAt(issuedAt).
		Expiration(expiresAt).
		Claim(claimSessionID, sessionID).
		Claim(claimUserID, userID)
	for key, val := range extra {
		builder.Claim(key, val)
	}
	if config.Version != "" {
		builder.Claim(claimAppVersion, config.Version)
	}
	token, err := builder.Build()
	if err != nil {
		return "", err
	}
	signed, err := jwt.Sign(token, signOption)
	if err != nil {
		return "", err
	}
	return string(signed), nil
}

func userIdentityClaims(localUser *user.User) map[string]any {
	claims := map[string]any{}
	if localUser.Username != "" {
		claims[claimUsername] = localUser.Username
	}
	if email := kit.FromPtr(localUser.Email); email != "" {
		claims[claimEmail] = email
	}
	if displayName := kit.FromPtr(localUser.DisplayName); displayName != "" {
		claims[claimDisplayName] = displayName
	}
	return claims
}

func (s *AuthService) buildTokenPair(ctx context.Context, localUser *user.User, localSession *session.UserSession, refreshJTI string) (*TokenPair, error) {
	minutes := s.settingsService.GetSettingsConfig().AuthSessionTimeout.AsInt()
	sessionTimeout := kit.Ternary(minutes <= 0, 60, min(max(minutes, 15), 525600))
	now := time.Now()
	accessTokenExpiry := now.Add(time.Duration(sessionTimeout) * time.Minute)

	signingKey, err := s.loadSigningKey(ctx)
	if err != nil {
		return nil, err
	}
	mldsaOpt := jwt.WithKey(jwa.MLDSA87(), signingKey)

	accessToken, err := signSessionJWT(localUser.ID, accessTokenSubject, localUser.ID, localSession.ID, now, accessTokenExpiry, userIdentityClaims(localUser), mldsaOpt)
	if err != nil {
		return nil, err
	}
	refreshToken, err := signSessionJWT(refreshJTI, refreshTokenSubject, localUser.ID, localSession.ID, now, localSession.ExpiresAt, nil, mldsaOpt)
	if err != nil {
		return nil, err
	}

	browserSigningKey, err := s.loadBrowserSigningKey(ctx)
	if err != nil {
		return nil, err
	}
	hs512Opt := jwt.WithKey(jwa.HS512(), browserSigningKey)

	browserToken, err := signSessionJWT(uuid.New().String(), browserTokenSubject, localUser.ID, localSession.ID, now, accessTokenExpiry, nil, hs512Opt)
	if err != nil {
		return nil, err
	}
	browserRefresh, err := signSessionJWT(refreshJTI, browserRefreshSubject, localUser.ID, localSession.ID, now, localSession.ExpiresAt, nil, hs512Opt)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:      accessToken,
		BrowserToken:     browserToken,
		RefreshToken:     refreshToken,
		BrowserRefresh:   browserRefresh,
		ExpiresAt:        accessTokenExpiry,
		RefreshExpiresAt: localSession.ExpiresAt,
	}, nil
}

func (s *AuthService) IssueFederatedToken(ctx context.Context, localUser *user.User, credentialID string, ttlSeconds int) (*TokenPair, error) {
	if localUser == nil {
		return nil, common.ErrUserNotFound
	}

	ttlSeconds = ClampFederatedTokenTTLSeconds(ttlSeconds)
	now := time.Now()
	accessTokenExpiry := now.Add(time.Duration(ttlSeconds) * time.Second)

	federatedSession, err := s.sessionService.CreateFederatedSession(ctx, localUser.ID, accessTokenExpiry, credentialID)
	if err != nil {
		return nil, err
	}

	signingKey, err := s.loadSigningKey(ctx)
	if err != nil {
		return nil, err
	}
	claims := userIdentityClaims(localUser)
	claims[claimTokenType] = session.UserSessionSourceFederated
	claims[claimFederatedCredentialID] = credentialID

	accessToken, err := signSessionJWT(localUser.ID, accessTokenSubject, localUser.ID, federatedSession.ID, now, accessTokenExpiry, claims, jwt.WithKey(jwa.MLDSA87(), signingKey))
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken: accessToken,
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

func uniqueOidcUsername(base, subject string) string {
	suffix := subject
	if len(suffix) > 8 {
		suffix = suffix[len(suffix)-8:]
	}
	return base + "_" + suffix
}

func (s *AuthService) runInBackground(ctx context.Context, name string, fn func(ctx context.Context) error) {
	bgCtx := context.WithoutCancel(ctx)

	go func() {
		defer func() {
			if panicErr := concurrency.PanicToError(recover()); panicErr != nil {
				slog.ErrorContext(bgCtx, "Unhandled error", "error", fmt.Errorf("Background task panicked: %w", panicErr), "task", name) //nolint:staticcheck // Preserve the existing error message.
			}
		}()

		taskCtx, cancel := context.WithTimeout(bgCtx, 1*time.Minute)
		defer cancel()

		if err := fn(taskCtx); err != nil {
			slog.ErrorContext(taskCtx, "Background task failed", "task", name, "error", err)
		}
	}()
}

func ClampFederatedTokenTTLSeconds(ttlSeconds int) int {
	if ttlSeconds <= 0 {
		return 900
	}
	if ttlSeconds < 60 {
		return 60
	}
	return kit.Ternary(ttlSeconds > 3600, 3600, ttlSeconds)
}
