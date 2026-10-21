package auth

import (
	"cmp"
	"context"
	"crypto/mldsa"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/lestrrat-go/jwx/v4/jwa"
	"github.com/lestrrat-go/jwx/v4/jwt"
	"github.com/samber/hot"
	"go.getarcane.app/kit/normalization"
	"go.getarcane.app/kit/pkg"
	"golang.org/x/sync/errgroup"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/internal/version"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cookie"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/jwtclaims"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/validation"
)

const sessionBootstrapVersionTimeout = 2 * time.Second

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrLocalAuthDisabled  = errors.New("local authentication is disabled")
	ErrOidcAuthDisabled   = errors.New("OIDC authentication is disabled")
	ErrMFARequired        = errors.New("multi-factor authentication is required")
)

type TokenPair struct {
	AccessToken      string    `json:"accessToken"`
	BrowserToken     string    `json:"-"`
	RefreshToken     string    `json:"refreshToken"`
	BrowserRefresh   string    `json:"-"`
	ExpiresAt        time.Time `json:"expiresAt"`
	RefreshExpiresAt time.Time `json:"-"`
}

func (p *TokenPair) CookieHeaders(secure bool) []string {
	return append(
		cookie.BuildTokenCookieStringFor(max(int(time.Until(p.ExpiresAt).Seconds()), 0)+60, p.BrowserToken, secure),
		cookie.BuildRefreshCookieStringFor(int(time.Until(p.RefreshExpiresAt).Seconds()), p.BrowserRefresh, secure),
	)
}

type verifiedTokenEntry struct {
	User             user.User
	SessionID        string
	TokenExpiresAt   time.Time
	SessionExpiresAt time.Time
}

type AuthService struct {
	userService        *user.UserService
	settingsService    *settings.SettingsService
	eventService       *event.EventService
	sessionService     *session.SessionService
	roleService        *role.RoleService
	environmentService *environment.EnvironmentService
	versionService     *version.VersionService
	signingKeyMu       sync.Mutex
	signingKey         *mldsa.PrivateKey
	browserSigningKey  []byte
	refreshExpiry      time.Duration
	config             *config.Config
	tokenCache         *hot.HotCache[string, verifiedTokenEntry]
	cacheGen           atomic.Uint64
	cacheFillMu        sync.Mutex
	dummyMu            sync.Mutex
	dummyHash          string
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

func NewAuthService(
	userService *user.UserService,
	settingsService *settings.SettingsService,
	eventService *event.EventService,
	sessionService *session.SessionService,
	roleService *role.RoleService,
	environmentService *environment.EnvironmentService,
	versionService *version.VersionService,
	cfg *config.Config,
) *AuthService {
	return &AuthService{
		userService:        userService,
		settingsService:    settingsService,
		eventService:       eventService,
		sessionService:     sessionService,
		roleService:        roleService,
		environmentService: environmentService,
		versionService:     versionService,
		refreshExpiry:      cfg.JWTRefreshExpiry,
		config:             cfg,
		tokenCache: hot.NewHotCache[string, verifiedTokenEntry](hot.LRU, 4096).
			WithTTL(60 * time.Second).
			WithJanitor().
			Build(),
	}
}

func (s *AuthService) GetSessionBootstrap(ctx context.Context, userID string, ps *authz.PermissionSet) (*types.SessionBootstrap, error) {
	var (
		currentUser  types.User
		versionInfo  *types.VersionInfo
		environments types.Paginated[types.Environment]
	)
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		userModel, err := s.userService.GetUser(groupCtx, userID)
		if err != nil {
			return fmt.Errorf("failed to get user: %w", err)
		}
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
		var err error
		environments, err = s.environmentService.ListAccessibleEnvironments(groupCtx, ps, pagination.QueryParams{Limit: -1})
		if err != nil {
			return fmt.Errorf("failed to list environments: %w", err)
		}
		return nil
	})
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return &types.SessionBootstrap{
		User:                currentUser,
		Version:             *versionInfo,
		Environments:        environments,
		PermissionsManifest: role.BuildPermissionsManifest(),
	}, nil
}

func (s *AuthService) GetOidcConfigurationStatus(ctx context.Context) (*types.OidcStatusInfo, error) {
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
			if localSettings, err := s.settingsService.GetSettings(ctx); err == nil {
				mergeAccounts = localSettings.OidcMergeAccounts.IsTrue()
				providerName = localSettings.OidcProviderName.Value
				providerLogoUrl = localSettings.OidcProviderLogoUrl.Value
			}
		}()
	}

	status := &types.OidcStatusInfo{
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
	localSettings, err := s.settingsService.GetSettings(ctx)
	if err != nil {
		return 60, err
	}

	minutes := localSettings.AuthSessionTimeout.AsInt()
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
	localSettings, err := s.settingsService.GetSettings(ctx)
	if err != nil {
		return true, err
	}
	return localSettings.AuthLocalEnabled.IsTrue(), nil
}

func (s *AuthService) IsOidcEnabled(ctx context.Context) (bool, error) {
	localSettings, err := s.settingsService.GetSettings(ctx)
	if err != nil {
		return false, err
	}
	return localSettings.OidcEnabled.IsTrue(), nil
}

func (s *AuthService) GetOidcConfig(ctx context.Context) (*settings.OidcConfig, error) {
	appSettings, err := s.settingsService.GetSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}
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

// AuthenticateLocalPrimary validates the local primary factor without
// creating a session. Callers must complete passkey MFA, when enabled, before
// issuing a bearer or refresh token.
func (s *AuthService) AuthenticateLocalPrimary(ctx context.Context, username, password string) (*user.User, error) {
	localEnabled, err := s.IsLocalAuthEnabled(ctx)
	if err != nil {
		return nil, err
	}
	if !localEnabled {
		return nil, ErrLocalAuthDisabled
	}

	localUser, err := s.userService.GetUserByUsername(ctx, username)
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
		case emailErr == nil && localUser != nil && emailUser.ID != localUser.ID:
			slog.WarnContext(ctx, "Rejecting login: identifier matches one account's username and a different account's email", "identifier", username)
			return nil, ErrInvalidCredentials
		case emailErr == nil && localUser == nil:
			localUser = emailUser
		}
	}
	if localUser == nil {
		dummyHash, dummyErr := s.dummyPasswordHash(ctx)
		if dummyErr != nil {
			return nil, dummyErr
		}
		_ = s.userService.ValidatePassword(ctx, dummyHash, password)
		return nil, ErrInvalidCredentials
	}
	passwordHash := localUser.PasswordHash
	if passwordHash == "" {
		dummyHash, dummyErr := s.dummyPasswordHash(ctx)
		if dummyErr != nil {
			return nil, dummyErr
		}
		passwordHash = dummyHash
	}
	if validatePasswordErr := s.userService.ValidatePassword(ctx, passwordHash, password); validatePasswordErr != nil {
		return nil, ErrInvalidCredentials
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

// PrepareOidcLogin reconciles the provider identity without creating a
// session. The caller must complete passkey MFA, when enabled, before issuing
// tokens.
func (s *AuthService) PrepareOidcLogin(ctx context.Context, userInfo types.OidcUserInfo, tokenResp *types.OidcTokenResponse) (*user.User, bool, error) {
	if userInfo.Subject == "" {
		return nil, false, errors.New("missing OIDC subject identifier")
	}
	return s.findOrCreateOidcUser(ctx, userInfo, tokenResp)
}

// CompleteLogin creates the authenticated session after all required factors
// have succeeded. Source is server-selected and is persisted with the session.
func (s *AuthService) CompleteLogin(ctx context.Context, localUser *user.User, meta types.SessionMeta, source, mfaMethod string, eventMetadata ...database.JSON) (*TokenPair, error) {
	if localUser == nil {
		return nil, common.ErrUserNotFound
	}
	if strings.TrimSpace(source) == "" {
		source = session.UserSessionSourceLocal
	}
	mfaMethod = strings.TrimSpace(mfaMethod)
	if localUser.PasskeyMFAEnabled && source != session.UserSessionSourcePasskey && mfaMethod != session.PasskeyMFAMethod && mfaMethod != session.RecoveryCodeMFAMethod {
		return nil, ErrMFARequired
	}
	meta.Source = source
	meta.MFAMethod = mfaMethod
	if meta.MFAMethod != "" && meta.MFAVerifiedAt == nil {
		now := time.Now()
		meta.MFAVerifiedAt = &now
	}

	if s.sessionService == nil {
		return nil, common.Classify(common.ErrUnavailable, errors.New("Session service is not configured")) //nolint:staticcheck // Preserve the existing error message.
	}
	localSession, refreshJTI, err := s.sessionService.CreateSession(ctx, localUser.ID, time.Now().Add(s.refreshExpiry), meta)
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
	if meta.MFAMethod != "" {
		metadata["mfa"] = meta.MFAMethod
	}
	if s.eventService != nil {
		logUserID := localUser.ID
		logUsername := localUser.Username
		s.runInBackground(ctx, "log_user_login", func(ctx context.Context) error {
			return s.eventService.LogUserEvent(ctx, event.EventTypeUserLogin, logUserID, logUsername, metadata)
		})
	}

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

func (s *AuthService) OidcLogin(ctx context.Context, userInfo types.OidcUserInfo, tokenResp *types.OidcTokenResponse, meta types.SessionMeta) (*user.User, *TokenPair, error) {
	localUser, isNewUser, err := s.PrepareOidcLogin(ctx, userInfo, tokenResp)
	if err != nil {
		return nil, nil, err
	}
	if localUser.PasskeyMFAEnabled {
		return nil, nil, ErrMFARequired
	}
	tokenPair, err := s.CompleteLogin(ctx, localUser, meta, session.UserSessionSourceOidc, "", database.JSON{
		"newUser": isNewUser,
		"subject": userInfo.Subject,
	})
	if err != nil {
		return nil, nil, err
	}
	return localUser, tokenPair, nil
}

func (s *AuthService) LogLogout(ctx context.Context, localUser *user.User) {
	if s.eventService == nil || localUser == nil {
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

	if userInfo.Email != "" {
		if localSettings, settingsErr := s.settingsService.GetSettings(ctx); settingsErr == nil && localSettings.OidcMergeAccounts.IsTrue() {
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

	// The username probe is not atomic with the insert: a concurrent
	// first-time login can claim the candidate in between, failing the
	// unique constraint. Probe again for a fresh candidate and retry.
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

// resolveOidcUsername returns the preferred username for a new OIDC
// user, or a currently unclaimed collision-free candidate when an existing
// account already owns it.
func (s *AuthService) resolveOidcUsername(ctx context.Context, userInfo types.OidcUserInfo) (string, error) {
	var username string
	if userInfo.PreferredUsername == "" {
		username = generateUsernameFromEmail(userInfo.Email, userInfo.Subject)
	} else {
		username = userInfo.PreferredUsername
	}

	// CreateUser trims usernames; check availability using the same value.
	username = strings.TrimSpace(username)

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

// syncOidcRoleAssignments rebuilds the user's `source='oidc'` role assignments
// based on the OIDC group claim and the configured OidcRoleMapping rows.
// Manual assignments are untouched.
func (s *AuthService) syncOidcRoleAssignments(ctx context.Context, localUser *user.User, userInfo types.OidcUserInfo, tokenResp *types.OidcTokenResponse) error {
	if s.roleService == nil || localUser == nil {
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

	err = s.roleService.ReplaceUserAssignments(ctx, localUser.ID, role.RoleAssignmentSourceOidc, desired)
	if errors.Is(err, common.ErrNoGlobalAdminRemains) {
		slog.WarnContext(ctx, "OIDC role sync would remove the last global admin; keeping existing assignments", "userId", localUser.ID)
		return nil
	}
	return err
}

// extractOidcGroups reads the user's group memberships from the OIDC userinfo
// and ID token, using the claim path configured in OidcGroupsClaim (defaults
// to "groups"). Falls back to userInfo.Groups if no value is found at the
// configured path.
func (s *AuthService) extractOidcGroups(ctx context.Context, userInfo types.OidcUserInfo, tokenResp *types.OidcTokenResponse) []string {
	claim := "groups"
	if localSettings, err := s.settingsService.GetSettings(ctx); err == nil {
		claim = cmp.Or(strings.TrimSpace(localSettings.OidcGroupsClaim.Value), "groups")
	}

	if v, ok := jwtclaims.GetByPath(userInfo.Extra, claim).Get(); ok {
		if groups := jwtclaims.GetStringSliceClaim(map[string]any{"groups": v}, "groups"); len(groups) > 0 {
			return groups
		}
	}
	if tokenResp != nil && tokenResp.IDToken != "" {
		if parsed := jwtclaims.ParseJWTClaims(tokenResp.IDToken); parsed != nil {
			if v, ok := jwtclaims.GetByPath(parsed, claim).Get(); ok {
				if groups := jwtclaims.GetStringSliceClaim(map[string]any{"groups": v}, "groups"); len(groups) > 0 {
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
	if s.settingsService == nil {
		return nil, common.Classify(common.ErrUnavailable, errors.New("JWT signing key is not configured"))
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
	if s.settingsService == nil {
		return nil, common.Classify(common.ErrUnavailable, errors.New("browser session signing key is not configured"))
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

	if s.sessionService == nil {
		return nil, common.Classify(common.ErrUnavailable, errors.New("Session service is not configured")) //nolint:staticcheck // Preserve the existing error message.
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

	rotatedSession, refreshJTI, err := s.sessionService.RotateRefreshToken(ctx, claims.SessionID, claims.ID, meta)
	if err != nil {
		return nil, err
	}

	return s.buildTokenPair(ctx, localUser, rotatedSession, refreshJTI)
}

func (s *AuthService) VerifyToken(ctx context.Context, accessToken string) (*user.User, string, error) {
	tokenHash := kit.SHA256Hex(accessToken)
	if localUser, sessionID, ok := s.cachedVerification(tokenHash); ok {
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
	if localUser, sessionID, ok := s.cachedVerification(tokenHash); ok {
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

func (s *AuthService) cachedVerification(tokenHash string) (*user.User, string, bool) {
	cached, ok, _ := s.tokenCache.Get(tokenHash)
	if !ok {
		return nil, "", false
	}
	now := time.Now()
	if now.After(cached.TokenExpiresAt) || now.After(cached.SessionExpiresAt) {
		s.tokenCache.Delete(tokenHash)
		return nil, "", false
	}
	return new(cached.User), cached.SessionID, true
}

func (s *AuthService) verifyTokenClaims(ctx context.Context, tokenHash string, claims *accessTokenClaims) (*user.User, string, error) {
	gen := s.cacheGen.Load()
	if claims.AppVersion != "" && claims.AppVersion != config.Version {
		slog.InfoContext(ctx, "Token version mismatch detected", "tokenVersion", claims.AppVersion, "currentVersion", config.Version, "user", claims.Username)
		return nil, "", common.ErrTokenVersionMismatch
	}
	if s.sessionService == nil {
		return nil, "", common.Classify(common.ErrUnavailable, errors.New("Session service is not configured")) //nolint:staticcheck // Preserve the existing error message.
	}

	// Verify user exists in DB
	// This ensures that if the database is wiped or user is deleted, the token becomes invalid
	// even if the JWT signature is still valid (e.g. same signing key).
	dbUser, err := s.userService.GetUserByID(ctx, claims.UserID)
	if err != nil {
		return nil, "", kit.Ternary[error](errors.Is(err, common.ErrUserNotFound), common.ErrInvalidToken, err)
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

	s.cacheFillMu.Lock()
	if s.cacheGen.Load() == gen {
		s.tokenCache.Set(tokenHash, verifiedTokenEntry{User: *dbUser, SessionID: userSession.ID, TokenExpiresAt: claims.ExpiresAt, SessionExpiresAt: userSession.ExpiresAt})
	}
	s.cacheFillMu.Unlock()

	return dbUser, userSession.ID, nil
}

func (s *AuthService) ChangePassword(ctx context.Context, userID, currentPassword, newPassword, currentSessionID string) error {
	if s.sessionService == nil {
		return common.Classify(common.ErrUnavailable, errors.New("Session service is not configured")) //nolint:staticcheck // Preserve the existing error message.
	}

	localUser, err := s.userService.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	if localUser.PasswordHash == "" && kit.FromPtr(localUser.OidcSubjectId) != "" {
		return common.Classify(common.ErrForbidden, errors.New("local passwords cannot be set for accounts managed by an identity provider"))
	}
	if localUser.PasswordHash != "" {
		if validatePasswordErr := s.userService.ValidatePassword(ctx, localUser.PasswordHash, currentPassword); validatePasswordErr != nil {
			return ErrInvalidCredentials
		}
	}

	policy := s.passwordPolicy(ctx)
	if validatePasswordPolicyErr := validation.ValidatePasswordPolicy(newPassword, policy); validatePasswordPolicyErr != nil {
		return common.Classify(common.ErrValidation, validatePasswordPolicyErr)
	}

	if _, err = s.userService.SetPasswordAndRevokeSessionsExcept(ctx, localUser, newPassword, currentSessionID, nil); err != nil {
		return err
	}
	// The current session's cached user still carries the old password flags.
	s.InvalidateUserTokenCache(userID)
	return nil
}

func (s *AuthService) passwordPolicy(ctx context.Context) string {
	if s.settingsService == nil {
		return validation.PasswordPolicyStrong
	}
	return s.settingsService.GetStringSetting(ctx, "authPasswordPolicy", validation.PasswordPolicyStrong)
}

// InvalidateUserTokenCache purges all cached token verifications for a user.
// Call this after admin-initiated role changes, account disable, or user
// deletion so stale verifications cannot grant access for the cache TTL.
func (s *AuthService) InvalidateUserTokenCache(userID string) {
	if s.tokenCache == nil || strings.TrimSpace(userID) == "" {
		return
	}
	s.invalidateTokenCache(func(entry verifiedTokenEntry) bool { return entry.User.ID == userID })
}

func (s *AuthService) invalidateTokenCache(matches func(verifiedTokenEntry) bool) {
	s.cacheFillMu.Lock()
	defer s.cacheFillMu.Unlock()
	s.cacheGen.Add(1)
	keys := make([]string, 0)
	s.tokenCache.Range(func(key string, entry verifiedTokenEntry) bool {
		if matches(entry) {
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
	if err := s.sessionService.RevokeSession(ctx, sessionID); err != nil {
		return err
	}
	s.invalidateTokenCache(func(entry verifiedTokenEntry) bool { return entry.SessionID == sessionID })
	return nil
}

func (s *AuthService) Stop() {
	s.tokenCache.StopJanitor()
}

func (s *AuthService) RevokeSessionByRefreshToken(ctx context.Context, refreshToken string) error {
	claims, err := s.parseAnyRefreshToken(ctx, refreshToken)
	if err != nil {
		return err
	}
	return s.RevokeSession(ctx, claims.SessionID)
}

// LogoutAllOtherSessions revokes every active session for userID except
// currentSessionID, so the caller stays signed in on their current device.
func (s *AuthService) LogoutAllOtherSessions(ctx context.Context, userID, currentSessionID string) error {
	if s.sessionService == nil {
		return nil
	}
	if err := s.sessionService.RevokeAllUserSessionsExcept(ctx, userID, currentSessionID); err != nil {
		return err
	}
	s.invalidateTokenCache(func(entry verifiedTokenEntry) bool {
		return entry.User.ID == userID && entry.SessionID != currentSessionID
	})
	return nil
}

func (s *AuthService) buildTokenPair(ctx context.Context, localUser *user.User, localSession *session.UserSession, refreshJTI string) (*TokenPair, error) {
	sessionTimeout, _ := s.GetSessionTimeout(ctx)
	now := time.Now()
	accessTokenExpiry := now.Add(time.Duration(sessionTimeout) * time.Minute)

	signingKey, err := s.loadSigningKey(ctx)
	if err != nil {
		return nil, err
	}

	accessBuilder := jwt.NewBuilder().
		JwtID(localUser.ID).
		Subject(accessTokenSubject).
		IssuedAt(now).
		Expiration(accessTokenExpiry).
		Claim(claimSessionID, localSession.ID).
		Claim(claimUserID, localUser.ID)
	if localUser.Username != "" {
		accessBuilder.Claim(claimUsername, localUser.Username)
	}
	if localUser.Email != nil && *localUser.Email != "" {
		accessBuilder.Claim(claimEmail, *localUser.Email)
	}
	if localUser.DisplayName != nil && *localUser.DisplayName != "" {
		accessBuilder.Claim(claimDisplayName, *localUser.DisplayName)
	}
	if config.Version != "" {
		accessBuilder.Claim(claimAppVersion, config.Version)
	}
	accessToken, err := accessBuilder.Build()
	if err != nil {
		return nil, err
	}
	accessTokenBytes, err := jwt.Sign(accessToken, jwt.WithKey(jwa.MLDSA87(), signingKey))
	if err != nil {
		return nil, err
	}

	refreshBuilder := jwt.NewBuilder().
		JwtID(refreshJTI).
		Subject(refreshTokenSubject).
		IssuedAt(now).
		Expiration(localSession.ExpiresAt).
		Claim(claimUserID, localUser.ID).
		Claim(claimSessionID, localSession.ID)
	if config.Version != "" {
		refreshBuilder.Claim(claimAppVersion, config.Version)
	}
	refreshToken, err := refreshBuilder.Build()
	if err != nil {
		return nil, err
	}
	refreshTokenBytes, err := jwt.Sign(refreshToken, jwt.WithKey(jwa.MLDSA87(), signingKey))
	if err != nil {
		return nil, err
	}

	browserSigningKey, err := s.loadBrowserSigningKey(ctx)
	if err != nil {
		return nil, err
	}
	browserBuilder := jwt.NewBuilder().
		JwtID(uuid.New().String()).
		Subject(browserTokenSubject).
		IssuedAt(now).
		Expiration(accessTokenExpiry).
		Claim(claimSessionID, localSession.ID).
		Claim(claimUserID, localUser.ID)
	if config.Version != "" {
		browserBuilder.Claim(claimAppVersion, config.Version)
	}
	browserToken, err := browserBuilder.Build()
	if err != nil {
		return nil, err
	}
	browserTokenBytes, err := jwt.Sign(browserToken, jwt.WithKey(jwa.HS512(), browserSigningKey))
	if err != nil {
		return nil, err
	}

	browserRefreshBuilder := jwt.NewBuilder().
		JwtID(refreshJTI).
		Subject(browserRefreshSubject).
		IssuedAt(now).
		Expiration(localSession.ExpiresAt).
		Claim(claimUserID, localUser.ID).
		Claim(claimSessionID, localSession.ID)
	if config.Version != "" {
		browserRefreshBuilder.Claim(claimAppVersion, config.Version)
	}
	browserRefresh, err := browserRefreshBuilder.Build()
	if err != nil {
		return nil, err
	}
	browserRefreshBytes, err := jwt.Sign(browserRefresh, jwt.WithKey(jwa.HS512(), browserSigningKey))
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:      string(accessTokenBytes),
		BrowserToken:     string(browserTokenBytes),
		RefreshToken:     string(refreshTokenBytes),
		BrowserRefresh:   string(browserRefreshBytes),
		ExpiresAt:        accessTokenExpiry,
		RefreshExpiresAt: localSession.ExpiresAt,
	}, nil
}

func (s *AuthService) IssueFederatedToken(ctx context.Context, localUser *user.User, credentialID string, ttlSeconds int) (*TokenPair, error) {
	if s.sessionService == nil {
		return nil, common.Classify(common.ErrUnavailable, errors.New("Session service is not configured")) //nolint:staticcheck // Preserve the existing error message.
	}
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
	builder := jwt.NewBuilder().
		JwtID(localUser.ID).
		Subject(accessTokenSubject).
		IssuedAt(now).
		Expiration(accessTokenExpiry).
		Claim(claimSessionID, federatedSession.ID).
		Claim(claimUserID, localUser.ID).
		Claim(claimTokenType, session.UserSessionSourceFederated).
		Claim(claimFederatedCredentialID, credentialID)
	if localUser.Username != "" {
		builder.Claim(claimUsername, localUser.Username)
	}
	if localUser.Email != nil && *localUser.Email != "" {
		builder.Claim(claimEmail, *localUser.Email)
	}
	if localUser.DisplayName != nil && *localUser.DisplayName != "" {
		builder.Claim(claimDisplayName, *localUser.DisplayName)
	}
	if config.Version != "" {
		builder.Claim(claimAppVersion, config.Version)
	}
	accessToken, err := builder.Build()
	if err != nil {
		return nil, err
	}
	accessTokenBytes, err := jwt.Sign(accessToken, jwt.WithKey(jwa.MLDSA87(), signingKey))
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken: string(accessTokenBytes),
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

// uniqueOidcUsername derives a collision-free username candidate by
// suffixing the tail of the OIDC subject, mirroring generateUsernameFromEmail's
// fallback format.
func uniqueOidcUsername(base, subject string) string {
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
			if panicErr := utils.PanicToError(recover()); panicErr != nil {
				slog.ErrorContext(bgCtx, "Unhandled error", "error", fmt.Errorf("Background task panicked: %w", panicErr), "task", name) //nolint:staticcheck // Preserve the existing error message.
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
	return kit.Ternary(ttlSeconds > 3600, 3600, ttlSeconds)
}
