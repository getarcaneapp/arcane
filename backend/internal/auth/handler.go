package auth

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/auth"
	"github.com/getarcaneapp/arcane/types/v2/base"
	settingstypes "github.com/getarcaneapp/arcane/types/v2/settings"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/labstack/echo/v5"
	"github.com/samber/mo"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	userdomain "github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cookie"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/validation"
)

type AuthHandler struct {
	userService            *userdomain.UserService
	authService            *AuthService
	beginMFAAuthentication func(context.Context, string, auth.SessionMeta, string) (*auth.MFAChallenge, error)
	settingsService        *settings.SettingsService
}

type LoginInput struct {
	UserAgent string `header:"User-Agent"`
	Body      auth.Login
}

type LoginOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Session cookie"`
	Body      base.ApiResponse[auth.AuthenticationResponse]
}

type LogoutOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Cleared session cookie"`
	Body      base.ApiResponse[base.MessageResponse]
}

type RefreshTokenInput struct {
	UserAgent string `header:"User-Agent"`
	Body      auth.Refresh
}

type RefreshTokenOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Updated session cookie"`
	Body      base.ApiResponse[auth.TokenRefreshResponse]
}

type ChangePasswordInput struct {
	Body auth.PasswordChange
}

type UpdateMyProfileBody struct {
	DisplayName *string           `json:"displayName,omitempty"`
	Email       *string           `json:"email,omitempty"`
	Locale      *string           `json:"locale,omitempty"`
	TimeFormat  *user.TimeFormat  `json:"timeFormat,omitempty" enum:"auto,12h,24h"`
	FontSize    *int              `json:"fontSize,omitempty" minimum:"12" maximum:"20"`
	Preferences *user.Preferences `json:"preferences,omitempty"`
}

type UpdateMyProfileInput struct {
	Body UpdateMyProfileBody
}

type UploadMyAvatarInput struct {
	RawBody multipart.Form `contentType:"multipart/form-data"`
}

// Login authenticates a user and returns tokens.
func (h *AuthHandler) Login(ctx context.Context, input *LoginInput) (*LoginOutput, error) {
	localAuthEnabled, err := h.authService.IsLocalAuthEnabled(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to check authentication settings")
	}
	if !localAuthEnabled {
		return nil, huma.Error400BadRequest("Local authentication is disabled")
	}

	meta := handlerutil.SessionMetaFromContext(ctx, input.UserAgent)
	userModel, err := h.authService.AuthenticateLocalPrimary(ctx, input.Body.Username, input.Body.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			return nil, huma.Error401Unauthorized("Invalid username or password")
		case errors.Is(err, ErrLocalAuthDisabled):
			return nil, huma.Error400BadRequest("Local authentication is disabled")
		default:
			return nil, huma.Error500InternalServerError("Authentication failed")
		}
	}

	if userModel.PasskeyMFAEnabled {
		challenge, beginMFAAuthenticationErr := h.beginMFAAuthentication(ctx, userModel.ID, meta, session.UserSessionSourceLocal)
		if beginMFAAuthenticationErr != nil {
			return nil, huma.Error500InternalServerError("Authentication failed")
		}
		return &LoginOutput{
			Body: base.ApiResponse[auth.AuthenticationResponse]{
				Success: true,
				Data: auth.AuthenticationResponse{
					Success: true,
					Status:  auth.AuthenticationStatusMFARequired,
					MFA:     challenge,
				},
			},
		}, nil
	}

	tokenPair, err := h.authService.CompleteLogin(ctx, userModel, meta, session.UserSessionSourceLocal, "")
	if err != nil {
		return nil, huma.Error500InternalServerError("Authentication failed")
	}

	userResp, err := h.userService.ToUserResponseDto(ctx, *userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to map user")
	}

	maxAge := max(int(time.Until(tokenPair.ExpiresAt).Seconds()), 0)
	maxAge += 60
	expiresAt := tokenPair.ExpiresAt

	return &LoginOutput{
		SetCookie: cookie.BuildTokenCookieStringFor(maxAge, tokenPair.BrowserToken, cookie.SecureCookieFromContext(ctx)),
		Body: base.ApiResponse[auth.AuthenticationResponse]{
			Success: true,
			Data: auth.AuthenticationResponse{
				Success:      true,
				Status:       auth.AuthenticationStatusAuthenticated,
				Token:        tokenPair.AccessToken,
				RefreshToken: tokenPair.RefreshToken,
				ExpiresAt:    &expiresAt,
				User:         &userResp,
			},
		},
	}, nil
}

// Logout clears the authentication session.
func (h *AuthHandler) Logout(ctx context.Context, input *struct{}) (*LogoutOutput, error) {
	if h.authService != nil {
		if sessionID, exists := middleware.GetCurrentSessionIDFromContext(ctx); exists {
			if err := h.authService.RevokeSession(ctx, sessionID); err != nil {
				slog.ErrorContext(ctx, "Failed to revoke session on logout; clearing cookie anyway", "sessionId", sessionID, "error", err)
			}
		}
		if userModel, exists := userdomain.CurrentUserFromContext(ctx); exists {
			h.authService.LogLogout(ctx, userModel)
		}
	}

	return &LogoutOutput{
		SetCookie: cookie.BuildClearTokenCookieStringsFor(cookie.SecureCookieFromContext(ctx)),
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Logged out successfully",
			},
		},
	}, nil
}

// GetCurrentUser reads the current user from the database. Uses
// ToUserResponseDto (not the generic struct mapper) so the RBAC fields
// (RoleAssignments, PermissionsByEnv) are resolved via RoleService.
func (h *AuthHandler) GetCurrentUser(ctx context.Context, input *struct{}) (*handlerutil.Out[user.User], error) {
	currentUser, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	userModel, err := h.userService.GetUser(ctx, currentUser.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get user information")
	}

	out, err := h.userService.ToUserResponseDto(ctx, *userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to map user")
	}

	return &handlerutil.Out[user.User]{
		Body: base.ApiResponse[user.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// RefreshToken obtains a new access token using a refresh token.
func (h *AuthHandler) RefreshToken(ctx context.Context, input *RefreshTokenInput) (*RefreshTokenOutput, error) {
	tokenPair, err := h.authService.RefreshToken(ctx, input.Body.RefreshToken, handlerutil.SessionMetaFromContext(ctx, input.UserAgent))
	if err != nil {
		switch {
		case errors.Is(err, common.ErrInvalidToken),
			errors.Is(err, common.ErrExpiredToken),
			errors.Is(err, common.ErrTokenValidation),
			errors.Is(err, common.ErrSessionRevoked),
			errors.Is(err, common.ErrTokenVersionMismatch):
			return nil, huma.Error401Unauthorized("Invalid or expired refresh token")
		default:
			return nil, huma.Error500InternalServerError("Failed to refresh token")
		}
	}

	maxAge := max(int(time.Until(tokenPair.ExpiresAt).Seconds()), 0)
	maxAge += 60

	return &RefreshTokenOutput{
		SetCookie: cookie.BuildTokenCookieStringFor(maxAge, tokenPair.BrowserToken, cookie.SecureCookieFromContext(ctx)),
		Body: base.ApiResponse[auth.TokenRefreshResponse]{
			Success: true,
			Data: auth.TokenRefreshResponse{
				Token:        tokenPair.AccessToken,
				RefreshToken: tokenPair.RefreshToken,
				ExpiresAt:    tokenPair.ExpiresAt,
			},
		},
	}, nil
}

// ChangePassword changes the current user's password.
func (h *AuthHandler) ChangePassword(ctx context.Context, input *ChangePasswordInput) (*handlerutil.Out[base.MessageResponse], error) {
	userModel, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	if input.Body.CurrentPassword == "" {
		return nil, huma.Error400BadRequest("Current password is required")
	}

	currentSessionID, _ := middleware.GetCurrentSessionIDFromContext(ctx)
	err = h.authService.ChangePassword(ctx, userModel.ID, input.Body.CurrentPassword, input.Body.NewPassword, currentSessionID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			return nil, huma.Error401Unauthorized("Current password is incorrect")
		case errors.Is(err, common.ErrValidation):
			passwordPolicy := h.authService.passwordPolicyInternal(ctx)
			return nil, handlerutil.Error400BadRequestWithType(err.Error(), validation.PasswordPolicyProblemType(passwordPolicy))
		default:
			return nil, huma.Error500InternalServerError("Failed to change password")
		}
	}

	return handlerutil.MessageOutput("Password changed successfully", ""), nil
}

// LogoutAllOtherSessions revokes every active session for the current user
// except the session making this request.
func (h *AuthHandler) LogoutAllOtherSessions(ctx context.Context, input *struct{}) (*handlerutil.Out[base.MessageResponse], error) {
	userModel, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	currentSessionID, _ := middleware.GetCurrentSessionIDFromContext(ctx)
	if logoutAllOtherSessionsErr := h.authService.LogoutAllOtherSessions(ctx, userModel.ID, currentSessionID); logoutAllOtherSessionsErr != nil {
		return nil, huma.Error500InternalServerError("failed to revoke sessions: " + logoutAllOtherSessionsErr.Error())
	}

	return handlerutil.MessageOutput("All other sessions signed out", ""), nil
}

// validatePreferencesInternal checks the preference values Huma's enum tags
// cannot express. Everything else is constrained by the schema.
func validatePreferencesInternal(p *user.Preferences) error {
	// The "default" sentinel means "no override"; anything else must be a color
	// literal safe to inject into CSS.
	if v := p.AccentColor; v != nil && *v != "" && *v != "default" && !settingstypes.SafeAccentColor.MatchString(*v) {
		return huma.Error400BadRequest("invalid accentColor value")
	}
	// Keep the landing page a same-origin relative path. The frontend
	// additionally validates it against the known nav pages, falling back to
	// /dashboard.
	if v := p.DefaultLandingPage; v != nil && (!strings.HasPrefix(*v, "/") || strings.HasPrefix(*v, "//")) {
		return huma.Error400BadRequest("invalid defaultLandingPage value")
	}
	return nil
}

// mergePreferencesInternal copies every set (non-nil) field of src onto dst.
// Reflection keeps this merge-only update free of per-field branches, the same
// way the settings update path walks its DTO, so a new preference needs no
// handler change.
func mergePreferencesInternal(dst, src *user.Preferences) {
	dstValue := reflect.ValueOf(dst).Elem()
	srcValue := reflect.ValueOf(src).Elem()

	for i := range srcValue.NumField() {
		field := srcValue.Field(i)
		if field.Kind() == reflect.Pointer && !field.IsNil() {
			dstValue.Field(i).Set(field)
		}
	}
}

// UpdateMyProfile lets the current user update their own displayName and email.
// OIDC-managed accounts are read-only here.
func (h *AuthHandler) UpdateMyProfile(ctx context.Context, input *UpdateMyProfileInput) (*handlerutil.Out[user.User], error) {
	currentUser, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	isOidcUser := currentUser.OidcSubjectId != nil && *currentUser.OidcSubjectId != ""
	touchesIdpFields := input.Body.DisplayName != nil || input.Body.Email != nil
	if isOidcUser && touchesIdpFields {
		return nil, huma.Error403Forbidden("display name and email are managed by your identity provider")
	}

	userModel, err := h.userService.GetUser(ctx, currentUser.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get user information")
	}

	if input.Body.DisplayName != nil {
		userModel.DisplayName = input.Body.DisplayName
	}
	if input.Body.Email != nil {
		normalized, normalizeOptionalEmailErr := userdomain.NormalizeOptionalEmail(input.Body.Email)
		if normalizeOptionalEmailErr != nil {
			return nil, huma.Error400BadRequest(normalizeOptionalEmailErr.Error())
		}
		userModel.Email = normalized
	}
	if input.Body.Locale != nil {
		userModel.Locale = input.Body.Locale
	}
	if input.Body.TimeFormat != nil {
		userModel.TimeFormat = *input.Body.TimeFormat
	}
	if input.Body.FontSize != nil {
		userModel.FontSize = input.Body.FontSize
	}
	if p := input.Body.Preferences; p != nil {
		if validatePreferencesErr := validatePreferencesInternal(p); validatePreferencesErr != nil {
			return nil, validatePreferencesErr
		}
		mergePreferencesInternal(&userModel.Preferences, p)
	}

	updated, err := h.userService.UpdateUser(ctx, userModel, nil)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to update user")
	}
	h.authService.InvalidateUserTokenCache(updated.ID)

	out, err := h.userService.ToUserResponseDto(ctx, *updated)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to map user")
	}

	return &handlerutil.Out[user.User]{
		Body: base.ApiResponse[user.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UploadMyAvatar lets the current user upload a custom profile picture.
// Accepts PNG, JPEG, or WebP images up to the configured avatar upload limit.
func (h *AuthHandler) UploadMyAvatar(ctx context.Context, input *UploadMyAvatarInput) (*handlerutil.Out[user.User], error) {
	currentUser, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	// Get file from multipart form
	files := input.RawBody.File["file"]
	if len(files) == 0 {
		return nil, huma.Error400BadRequest("no file uploaded; include a 'file' field in the multipart form")
	}

	fileHeader := files[0]

	f, err := fileHeader.Open()
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to read uploaded file: " + err.Error())
	}
	defer func() { _ = f.Close() }()

	maxSizeMb := h.avatarMaxUploadSizeMbInternal(ctx)
	maxSizeBytes := int64(maxSizeMb) * 1024 * 1024

	// Read one byte past the configured ceiling so oversized files are rejected
	// without buffering the full multipart payload.
	buf := new(bytes.Buffer)
	if _, readFromErr := buf.ReadFrom(io.LimitReader(f, maxSizeBytes+1)); readFromErr != nil {
		return nil, huma.Error500InternalServerError("failed to read file data: " + readFromErr.Error())
	}

	if int64(buf.Len()) > maxSizeBytes {
		return nil, huma.NewError(http.StatusRequestEntityTooLarge, fmt.Sprintf("avatar file must be %d MB or smaller", maxSizeMb))
	}
	data := buf.Bytes()

	// Detect and validate image format
	mimeType, err := detectAvatarMimeTypeInternal(data)
	if err != nil {
		return nil, huma.Error400BadRequest("unsupported image format: only PNG, JPEG and WebP are accepted")
	}
	data, mimeType = normalizeAvatarImageInternal(data, mimeType)

	if uploadAvatarErr := h.userService.UploadAvatar(ctx, currentUser.ID, data, mimeType); uploadAvatarErr != nil {
		slog.ErrorContext(ctx, "Failed to save avatar", "userId", currentUser.ID, "error", uploadAvatarErr)
		return nil, huma.Error500InternalServerError("failed to save avatar")
	}
	h.authService.InvalidateUserTokenCache(currentUser.ID)

	// Reload user so the response reflects the new AvatarURL
	updatedUser, err := h.userService.GetUser(ctx, currentUser.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get user information")
	}

	out, err := h.userService.ToUserResponseDto(ctx, *updatedUser)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to map user")
	}

	return &handlerutil.Out[user.User]{
		Body: base.ApiResponse[user.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

func (h *AuthHandler) avatarMaxUploadSizeMbInternal(ctx context.Context) int {
	const defaultAvatarMaxUploadSizeMb = 2
	if h.settingsService == nil {
		return defaultAvatarMaxUploadSizeMb
	}
	maxSizeMb := h.settingsService.GetIntSetting(ctx, "avatarMaxUploadSizeMb", defaultAvatarMaxUploadSizeMb)
	return kit.Ternary(maxSizeMb <= 0, defaultAvatarMaxUploadSizeMb, maxSizeMb)
}

// DeleteMyAvatar removes the current user's custom profile picture.
func (h *AuthHandler) DeleteMyAvatar(ctx context.Context, input *struct{}) (*handlerutil.Out[user.User], error) {
	currentUser, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	if deleteAvatarErr := h.userService.DeleteAvatar(ctx, currentUser.ID); deleteAvatarErr != nil {
		slog.ErrorContext(ctx, "Failed to delete avatar", "userId", currentUser.ID, "error", deleteAvatarErr)
		return nil, huma.Error500InternalServerError("failed to delete avatar")
	}
	h.authService.InvalidateUserTokenCache(currentUser.ID)

	updatedUser, err := h.userService.GetUser(ctx, currentUser.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get user information")
	}

	out, err := h.userService.ToUserResponseDto(ctx, *updatedUser)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to map user")
	}

	return &handlerutil.Out[user.User]{
		Body: base.ApiResponse[user.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// detectAvatarMimeTypeInternal validates that data is a supported image format and returns its MIME type.
// Supported formats: PNG, JPEG, WebP.
func detectAvatarMimeTypeInternal(data []byte) (string, error) {
	mimeType := http.DetectContentType(data)
	switch mimeType {
	case "image/png", "image/jpeg", "image/webp":
		return mimeType, nil
	default:
		return "", errors.New("unsupported image format: only PNG, JPEG and WebP are accepted")
	}
}

func normalizeAvatarImageInternal(data []byte, mimeType string) ([]byte, string) {
	const maxAvatarNormalizePixels = 16 * 1024 * 1024
	if mimeType != "image/png" {
		return data, mimeType
	}

	imageConfig, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || imageConfig.Width <= 0 || imageConfig.Height <= 0 {
		return data, mimeType
	}
	if imageConfig.Width > maxAvatarNormalizePixels/imageConfig.Height {
		return data, mimeType
	}

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil || imageHasTransparencyInternal(img) {
		return data, mimeType
	}

	var out bytes.Buffer
	if encodeErr := jpeg.Encode(&out, img, &jpeg.Options{Quality: 92}); encodeErr != nil {
		return data, mimeType
	}
	if out.Len() == 0 || out.Len() >= len(data) {
		return data, mimeType
	}

	return out.Bytes(), "image/jpeg"
}

func imageHasTransparencyInternal(img image.Image) bool {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := img.At(x, y).RGBA()
			if alpha != 0xffff {
				return true
			}
		}
	}
	return false
}

// securityRequirements holds parsed security requirements from an operation.
type securityRequirements struct {
	isRequired bool
	bearerAuth bool
	apiKeyAuth bool
}

type operationProvider interface {
	Operation() *huma.Operation
}

// parseSecurityRequirementsInternal extracts security requirements from a Huma operation.
func parseSecurityRequirementsInternal(api huma.API, ctx operationProvider) securityRequirements {
	reqs := securityRequirements{}
	if ctx.Operation() == nil {
		return reqs
	}

	security := ctx.Operation().Security
	if security == nil && api != nil && api.OpenAPI() != nil {
		security = api.OpenAPI().Security
	}
	if len(security) == 0 {
		return reqs
	}

	optional := false
	for _, secReq := range security {
		if len(secReq) == 0 {
			optional = true
			continue
		}
		if _, ok := secReq["BearerAuth"]; ok {
			reqs.bearerAuth = true
		}
		if _, ok := secReq["ApiKeyAuth"]; ok {
			reqs.apiKeyAuth = true
		}
	}

	reqs.isRequired = !optional && (reqs.bearerAuth || reqs.apiKeyAuth)

	return reqs
}

// tryBearerAuthInternal attempts Bearer token authentication. Returns the
// authenticated user on success, or the underlying error from VerifyToken so
// the caller can distinguish a missing/invalid token from a token-version
// mismatch (which requires clearing the stale cookie).
func tryBearerAuthInternal(ctx huma.Context, authService *AuthService) (*userdomain.User, string, error) {
	token, fromCookie := extractBearerTokenInternal(ctx)
	if token == "" {
		return nil, "", nil
	}
	var authenticatedUser *userdomain.User
	var sessionID string
	var err error
	if fromCookie {
		authenticatedUser, sessionID, err = authService.VerifyBrowserToken(ctx.Context(), token)
	} else {
		authenticatedUser, sessionID, err = authService.VerifyToken(ctx.Context(), token)
	}
	if err != nil {
		return nil, "", err
	}
	return authenticatedUser, sessionID, nil
}

// tryApiKeyAuthInternal checks if API key authentication should be allowed
// through. Returns the resolved user plus the API key record so the caller
// can resolve permissions according to the key's kind.
func tryApiKeyAuthInternal(ctx huma.Context, apiKeyService *apikey.ApiKeyService) (*userdomain.User, *apikey.ApiKey, bool) {
	apiKey := ctx.Header(middleware.HeaderApiKey)
	if apiKey == "" {
		return nil, nil, false
	}

	authenticatedUser, key, err := apiKeyService.ValidateApiKeyWithID(ctx.Context(), apiKey)
	if err != nil || authenticatedUser == nil {
		return nil, nil, false
	}

	return authenticatedUser, key, true
}

func tryEnvironmentAccessTokenAuthInternal(ctx huma.Context, resolver EnvironmentAccessTokenResolver, token string) (*userdomain.User, *environment.Environment, bool) {
	if resolver == nil || strings.TrimSpace(token) == "" {
		return nil, nil, false
	}

	env, err := resolver.ResolveEnvironmentByAccessToken(ctx.Context(), token)
	if err != nil || env == nil {
		return nil, nil, false
	}

	return createEnvironmentUserInternal(env), env, true
}

// tryAgentAuthInternal checks if the request is from an authenticated agent.
// Returns a sudo agent user if the agent token is valid.
func tryAgentAuthInternal(ctx huma.Context, cfg *config.Config) (*userdomain.User, bool) {
	if cfg == nil || !cfg.AgentMode {
		return nil, false
	}

	path := ctx.URL().Path

	// Check for agent bootstrap pairing
	if strings.HasPrefix(path, middleware.AgentPairingPrefix) &&
		AgentTokenMatches(ctx.Header(middleware.HeaderAgentBootstrap), cfg.AgentToken) {
		return createAgentSudoUserInternal(), true
	}

	// Check for agent token
	if AgentTokenMatches(ctx.Header(middleware.HeaderAgentToken), cfg.AgentToken) {
		return createAgentSudoUserInternal(), true
	}

	// Check for API key as agent token
	if AgentTokenMatches(ctx.Header(middleware.HeaderApiKey), cfg.AgentToken) {
		return createAgentSudoUserInternal(), true
	}

	return nil, false
}

// createAgentSudoUserInternal creates a sudo user for agent authentication.
// The sudo PermissionSet attached to the context by the agent token path
// bypasses every check; the user's Roles field is intentionally empty.
func createAgentSudoUserInternal() *userdomain.User {
	return &userdomain.User{
		ID:       "agent",
		Email:    new("agent@getarcane.app"),
		Username: "agent",
	}
}

// applyProxiedIconCatalogInternal copies the requesting user's icon catalog
// preference, forwarded by the manager, onto the synthetic user this request
// authenticates as. Without it the synthetic user has no preferences and every
// proxied icon resolves against the default catalog.
//
// Only called on synthetic-user auth paths (agent token, environment access
// token), where the header is set by the manager after it strips any
// client-supplied value. Real-user auth paths never read it.
func applyProxiedIconCatalogInternal(ctx huma.Context, authenticatedUser *userdomain.User) {
	if authenticatedUser == nil {
		return
	}
	catalog := strings.TrimSpace(ctx.Header(middleware.HeaderIconCatalog))
	if catalog == "" {
		return
	}
	authenticatedUser.Preferences.IconCatalog = &catalog
}

func createEnvironmentUserInternal(env *environment.Environment) *userdomain.User {
	return &userdomain.User{
		ID:       "environment:" + env.ID,
		Username: env.Name,
	}
}

// NewHumaMiddleware creates middleware that validates credentials and
// enforces security requirements defined on operations. It also resolves the
// caller's effective PermissionSet via permResolver and stashes it on the
// request context for downstream middleware.RequirePermission checks.
func NewHumaMiddleware(
	api huma.API,
	authService *AuthService,
	apiKeyService *apikey.ApiKeyService,
	permResolver PermissionResolver,
	envTokenResolver EnvironmentAccessTokenResolver,
	cfg *config.Config,
) func(
	ctx huma.Context,
	next func(
		huma.Context,
	),
) {
	return func(ctx huma.Context, next func(huma.Context)) {
		ctx = huma.WithContext(ctx, context.WithValue(ctx.Context(), middleware.ContextKeyRemoteAddr, ctx.RemoteAddr()))
		if authService == nil {
			next(ctx)
			return
		}

		if newCtx, ok := tryAgentAuthCtxInternal(ctx, cfg); ok {
			next(newCtx)
			return
		}

		reqs := parseSecurityRequirementsInternal(api, ctx)
		if !reqs.isRequired {
			next(opportunisticBearerAuthInternal(ctx, authService, permResolver))
			return
		}

		if reqs.apiKeyAuth && ctx.Header(middleware.HeaderApiKey) != "" {
			handleApiKeyAuthInternal(api, ctx, authService, apiKeyService, permResolver, envTokenResolver, reqs.bearerAuth, next)
			return
		}

		if authenticatedUser, env, ok := tryEnvironmentAccessTokenAuthInternal(ctx, envTokenResolver, ctx.Header(middleware.HeaderAgentToken)); ok {
			applyProxiedIconCatalogInternal(ctx, authenticatedUser)
			newCtx := setUserInContextInternal(ctx.Context(), authenticatedUser, authz.EnvironmentPermissionSet(env.ID))
			next(huma.WithContext(ctx, newCtx))
			return
		}

		if reqs.bearerAuth {
			nextCtx, handled := handleBearerAuthInternal(api, ctx, authService, permResolver)
			if handled {
				if nextCtx != nil {
					next(nextCtx)
				}
				return
			}
		}

		_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Unauthorized: valid authentication required")
	}
}

func tryAgentAuthCtxInternal(ctx huma.Context, cfg *config.Config) (huma.Context, bool) {
	if cfg == nil || !cfg.AgentMode {
		return ctx, false
	}
	authenticatedUser, ok := tryAgentAuthInternal(ctx, cfg)
	if !ok {
		return ctx, false
	}
	applyProxiedIconCatalogInternal(ctx, authenticatedUser)
	authCtx := setUserInContextInternal(ctx.Context(), authenticatedUser, authz.SudoPermissionSet())
	updatePath := strings.Contains(ctx.URL().Path, "/containers/") && strings.HasSuffix(ctx.URL().Path, "/update")
	if ctx.Method() == http.MethodPost && updatePath {
		if initiatorID := strings.TrimSpace(ctx.Header(middleware.HeaderUpdateInitiatorID)); initiatorID != "" {
			initiator := activity.StartedBy{
				UserID:      initiatorID,
				Username:    strings.TrimSpace(ctx.Header(middleware.HeaderUpdateInitiatorName)),
				DisplayName: strings.TrimSpace(ctx.Header(middleware.HeaderUpdateInitiatorDisplayName)),
			}
			authCtx = utils.WithUpdateInitiator(authCtx, initiator)
		}
	}
	// The agent token path is infrastructure-level and not per-user, so it
	// gets a sudo PermissionSet that bypasses every check.
	return huma.WithContext(ctx, authCtx), true
}

// opportunisticBearerAuthInternal populates the user/session context if a valid
// bearer token is present, but never fails the request. Used for public routes
// (e.g. logout) that still need to know who the caller is when a token exists.
func opportunisticBearerAuthInternal(ctx huma.Context, authService *AuthService, permResolver PermissionResolver) huma.Context {
	if token, _ := extractBearerTokenInternal(ctx); token == "" {
		return ctx
	}
	authenticatedUser, sessionID, err := tryBearerAuthInternal(ctx, authService)
	if err != nil || authenticatedUser == nil {
		return ctx
	}
	newCtx := setUserInContextInternal(ctx.Context(), authenticatedUser, resolveUserPermissionsInternal(ctx.Context(), permResolver, authenticatedUser))
	newCtx = context.WithValue(newCtx, middleware.ContextKeyCurrentSessionID, sessionID)
	return huma.WithContext(ctx, newCtx)
}

// handleApiKeyAuthInternal handles the API-key-present branch. Invalid user
// keys fail closed; only recognized environment tokens defer to bearer auth
// when both credentials are present on a bearer-capable operation.
func handleApiKeyAuthInternal(
	api huma.API,
	ctx huma.Context,
	authService *AuthService,
	apiKeyService *apikey.ApiKeyService,
	permResolver PermissionResolver,
	envTokenResolver EnvironmentAccessTokenResolver,
	allowBearerFallback bool,
	next func(
		huma.Context,
	),
) {
	if authenticatedUser, key, ok := tryApiKeyAuthInternal(ctx, apiKeyService); ok {
		// Personal keys inherit the owner's role permissions (same resolution
		// as session auth); scoped keys are limited to their own grants.
		var ps *authz.PermissionSet
		if key.Kind == apikey.ApiKeyKindPersonal {
			ps = resolveUserPermissionsInternal(ctx.Context(), permResolver, authenticatedUser)
		} else {
			ps = resolveApiKeyPermissionsInternal(ctx.Context(), permResolver, key.ID)
		}
		newCtx := setUserInContextInternal(ctx.Context(), authenticatedUser, ps)
		newCtx = context.WithValue(newCtx, middleware.ContextKeyApiKeyID, key.ID)
		next(huma.WithContext(ctx, newCtx))
		return
	}
	if authenticatedUser, env, ok := tryEnvironmentAccessTokenAuthInternal(ctx, envTokenResolver, ctx.Header(middleware.HeaderApiKey)); ok {
		if token, _ := extractBearerTokenInternal(ctx); allowBearerFallback && token != "" {
			nextCtx, handled := handleBearerAuthInternal(api, ctx, authService, permResolver)
			if handled {
				if nextCtx != nil {
					next(nextCtx)
				}
				return
			}
		}
		applyProxiedIconCatalogInternal(ctx, authenticatedUser)
		newCtx := setUserInContextInternal(ctx.Context(), authenticatedUser, authz.EnvironmentPermissionSet(env.ID))
		next(huma.WithContext(ctx, newCtx))
		return
	}
	_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Unauthorized: invalid API key")
}

func handleBearerAuthInternal(api huma.API, ctx huma.Context, authService *AuthService, permResolver PermissionResolver) (huma.Context, bool) {
	authenticatedUser, sessionID, err := tryBearerAuthInternal(ctx, authService)
	if err == nil && authenticatedUser != nil {
		ps := resolveUserPermissionsInternal(ctx.Context(), permResolver, authenticatedUser)
		newCtx := setUserInContextInternal(ctx.Context(), authenticatedUser, ps)
		newCtx = context.WithValue(newCtx, middleware.ContextKeyCurrentSessionID, sessionID)
		return huma.WithContext(ctx, newCtx), true
	}
	if errors.Is(err, common.ErrTokenVersionMismatch) {
		// The app version changed (a self-update). The session is still valid — the
		// refresh path tolerates the version change and rotates the token — so do NOT
		// clear the auth cookies. Return a recoverable 401 the frontend refreshes from.
		_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Application has been updated. Refreshing session.")
		return nil, true
	}
	if errors.Is(err, common.ErrSessionRevoked) || errors.Is(err, common.ErrTokenValidation) {
		for _, cookieHeader := range cookie.BuildClearTokenCookieStringsFor(cookie.SecureCookieFromContext(ctx.Context())) {
			ctx.AppendHeader("Set-Cookie", cookieHeader)
		}
		_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Session expired. Please log in again.")
		return nil, true
	}
	return nil, false
}

// resolveUserPermissionsInternal asks the RoleService for the user's resolved
// PermissionSet. If RoleService is unavailable or the lookup fails (boot-time
// edge cases, broken DB) it returns nil and logs a warning — handlers then
// see deny-all, which is the safe default.
func resolveUserPermissionsInternal(ctx context.Context, permResolver PermissionResolver, authenticatedUser *userdomain.User) *authz.PermissionSet {
	if permResolver == nil || authenticatedUser == nil {
		return nil
	}
	ps, err := permResolver.ResolvePermissions(ctx, authenticatedUser.ID)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve user permissions", "error", err, "userId", authenticatedUser.ID)
		return nil
	}
	return ps
}

func resolveApiKeyPermissionsInternal(ctx context.Context, permResolver PermissionResolver, apiKeyID string) *authz.PermissionSet {
	if permResolver == nil || apiKeyID == "" {
		return nil
	}
	ps, err := permResolver.ResolveApiKeyPermissions(ctx, apiKeyID)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve api key permissions", "error", err, "apiKeyId", apiKeyID)
		return nil
	}
	return ps
}

// extractBearerTokenInternal extracts the JWT token and reports whether it came from a cookie.
func extractBearerTokenInternal(ctx huma.Context) (string, bool) {
	// Try Authorization header first
	authHeader := ctx.Header("Authorization")
	if len(authHeader) > 7 && strings.ToLower(authHeader[:7]) == "bearer " {
		return authHeader[7:], false
	}

	// Try cookie as fallback
	if cookieHeader := ctx.Header("Cookie"); cookieHeader != "" {
		if token, err := cookie.GetTokenCookieFromHeader(cookieHeader); err == nil {
			return token, true
		}
	}

	return "", false
}

// setUserInContextInternal adds the authenticated user and the resolved
// PermissionSet to the context. Callers must supply a non-nil PermissionSet;
// pass authz.NewPermissionSet() to express deny-all.
func setUserInContextInternal(ctx context.Context, authenticatedUser *userdomain.User, ps *authz.PermissionSet) context.Context {
	if ps == nil {
		ps = authz.NewPermissionSet()
	}
	ctx = context.WithValue(ctx, middleware.ContextKeyUserID, authenticatedUser.ID)
	ctx = context.WithValue(ctx, user.CurrentUserContextKey{}, authenticatedUser)
	ctx = context.WithValue(ctx, middleware.ContextKeyUserPermissions, ps)
	return ctx
}

type AuthOptions struct {
	AdminRequired   bool
	SuccessOptional bool
}

type ApiKeyValidator interface {
	ValidateApiKeyWithID(ctx context.Context, rawKey string) (*userdomain.User, *apikey.ApiKey, error)
}

type EnvironmentAccessTokenResolver interface {
	ResolveEnvironmentByAccessToken(ctx context.Context, token string) (*environment.Environment, error)
}

// PermissionResolver resolves a caller's effective permission set. Implemented
// by role.RoleService; kept as an interface so tests can stub it.
type PermissionResolver interface {
	ResolvePermissions(ctx context.Context, userID string) (*authz.PermissionSet, error)
	ResolveApiKeyPermissions(ctx context.Context, apiKeyID string) (*authz.PermissionSet, error)
}

type AuthMiddleware struct {
	authService      *AuthService
	apiKeyValidator  ApiKeyValidator
	envTokenResolver EnvironmentAccessTokenResolver
	roleResolver     PermissionResolver
	cfg              *config.Config
	options          AuthOptions
}

func NewAuthMiddleware(authService *AuthService, cfg *config.Config) *AuthMiddleware {
	return &AuthMiddleware{
		authService: authService,
		cfg:         cfg,
		options:     AuthOptions{},
	}
}

func (m *AuthMiddleware) WithApiKeyValidator(validator ApiKeyValidator) *AuthMiddleware {
	clone := *m
	clone.apiKeyValidator = validator
	return &clone
}

func (m *AuthMiddleware) WithEnvironmentAccessTokenResolver(resolver EnvironmentAccessTokenResolver) *AuthMiddleware {
	clone := *m
	clone.envTokenResolver = resolver
	return &clone
}

func (m *AuthMiddleware) WithPermissionResolver(resolver PermissionResolver) *AuthMiddleware {
	clone := *m
	clone.roleResolver = resolver
	return &clone
}

func (m *AuthMiddleware) WithAdminNotRequired() *AuthMiddleware {
	clone := *m
	clone.options.AdminRequired = false
	return &clone
}

func (m *AuthMiddleware) WithAdminRequired() *AuthMiddleware {
	clone := *m
	clone.options.AdminRequired = true
	return &clone
}

func (m *AuthMiddleware) Add() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			reqCtx := c.Request().Context()
			if m.cfg != nil && m.cfg.AgentMode {
				return m.agentAuth(reqCtx, c, next)
			}
			return m.managerAuth(reqCtx, c, next)
		}
	}
}

func (m *AuthMiddleware) agentAuth(ctx context.Context, c *echo.Context, next echo.HandlerFunc) error {
	if isPreflightInternal(c) {
		return next(c)
	}

	req := c.Request()
	if strings.HasPrefix(req.URL.Path, middleware.AgentPairingPrefix) &&
		AgentTokenMatches(req.Header.Get(middleware.HeaderAgentBootstrap), m.cfg.AgentToken) {
		slog.InfoContext(ctx, "Agent auth: bootstrap pairing accepted", "path", req.URL.Path, "method", req.Method)
		agentSudoInternal(c)
		return next(c)
	}

	if AgentTokenMatches(req.Header.Get(middleware.HeaderAgentToken), m.cfg.AgentToken) {
		agentSudoInternal(c)
		return next(c)
	}

	// Check for API key as agent token
	if AgentTokenMatches(req.Header.Get(middleware.HeaderApiKey), m.cfg.AgentToken) {
		agentSudoInternal(c)
		return next(c)
	}

	slog.WarnContext(
		ctx, "Agent auth forbidden",
		"path", req.URL.Path,
		"method", req.Method,
		"hasAgentTokenHdr", req.Header.Get(middleware.HeaderAgentToken) != "",
		"agentTokenConfigSet", m.cfg.AgentToken != "",
	)
	return c.JSON(http.StatusForbidden, common.APIError{
		Code:    "FORBIDDEN",
		Message: "Invalid or missing agent token",
	})
}

// AgentTokenMatches compares a presented token against the configured
// agent token in constant time to avoid timing side channels.
func AgentTokenMatches(presented, configured string) bool {
	if presented == "" || configured == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(configured)) == 1
}

func (m *AuthMiddleware) managerAuth(ctx context.Context, c *echo.Context, next echo.HandlerFunc) error {
	req := c.Request()
	if agentToken := req.Header.Get(middleware.HeaderAgentToken); agentToken != "" {
		if env, ok := m.resolveEnvironmentAccessToken(ctx, agentToken).Get(); ok {
			ps := environmentScopedInternal(c, env)
			return m.authorizeAndContinueInternal(c, next, ps)
		}
	}

	// First, check for API key in X-API-Key header
	if apiKey := req.Header.Get(middleware.HeaderApiKey); apiKey != "" {
		return m.apiKeyHeaderAuth(ctx, c, next, apiKey)
	}

	token, fromCookie := extractBearerOrCookieTokenInternal(c)
	if token == "" {
		if m.options.SuccessOptional {
			return next(c)
		}
		return c.JSON(http.StatusUnauthorized, common.APIError{
			Code:    common.APIErrorCodeUnauthorized,
			Message: "Authentication required",
		})
	}

	var authenticatedUser *userdomain.User
	var sessionID string
	var err error
	if fromCookie {
		authenticatedUser, sessionID, err = m.authService.VerifyBrowserToken(ctx, token)
	} else {
		authenticatedUser, sessionID, err = m.authService.VerifyToken(ctx, token)
	}
	if err != nil {
		// A version mismatch means the app self-updated; the session is still valid
		// (the refresh path tolerates the version change and rotates the token), so do
		// NOT clear the cookies. Return a recoverable 401 the frontend refreshes from.
		if errors.Is(err, common.ErrTokenVersionMismatch) {
			return c.JSON(http.StatusUnauthorized, common.APIError{
				Code:    common.APIErrorCodeUnauthorized,
				Message: "Application has been updated. Refreshing session.",
			})
		}

		if errors.Is(err, common.ErrSessionRevoked) || errors.Is(err, common.ErrTokenValidation) {
			cookie.ClearTokenCookie(c.Response(), req)
			return c.JSON(http.StatusUnauthorized, common.APIError{
				Code:    common.APIErrorCodeUnauthorized,
				Message: "Session expired. Please log in again.",
			})
		}

		if m.options.SuccessOptional {
			return next(c)
		}
		return c.JSON(http.StatusUnauthorized, common.APIError{
			Code:    common.APIErrorCodeUnauthorized,
			Message: "Invalid or expired token",
		})
	}

	ps := m.resolvePermissionsOrDeny(ctx, authenticatedUser)
	c.Set(string(middleware.ContextKeyUserID), authenticatedUser.ID)
	c.Set(string(middleware.ContextKeyCurrentUser), authenticatedUser)
	c.Set(string(middleware.ContextKeyCurrentSessionID), sessionID)
	c.Set(string(middleware.ContextKeyUserPermissions), ps)
	return m.authorizeAndContinueInternal(c, next, ps)
}

// apiKeyHeaderAuth authenticates an X-API-Key credential: a user-owned API key
// first, then an environment access token presented through the same header.
func (m *AuthMiddleware) apiKeyHeaderAuth(ctx context.Context, c *echo.Context, next echo.HandlerFunc, apiKey string) error {
	if m.apiKeyValidator != nil {
		authenticatedUser, key, err := m.apiKeyValidator.ValidateApiKeyWithID(ctx, apiKey)
		if err == nil && authenticatedUser != nil {
			// Personal keys inherit the owner's role permissions (same
			// resolution as session auth); scoped keys use their own grants.
			var ps *authz.PermissionSet
			if key.Kind == apikey.ApiKeyKindPersonal {
				ps = m.resolvePermissionsOrDeny(ctx, authenticatedUser)
			} else {
				ps = m.resolveApiKeyPermissionsOrDeny(ctx, key.ID)
			}
			c.Set(string(middleware.ContextKeyUserID), authenticatedUser.ID)
			c.Set(string(middleware.ContextKeyCurrentUser), authenticatedUser)
			c.Set(string(middleware.ContextKeyUserPermissions), ps)
			c.Set(string(middleware.ContextKeyAuthMethod), "api_key")
			return m.authorizeAndContinueInternal(c, next, ps)
		}
	}
	if env, ok := m.resolveEnvironmentAccessToken(ctx, apiKey).Get(); ok {
		ps := environmentScopedInternal(c, env)
		return m.authorizeAndContinueInternal(c, next, ps)
	}
	return c.JSON(http.StatusUnauthorized, common.APIError{
		Code:    common.APIErrorCodeUnauthorized,
		Message: "Invalid or expired API key",
	})
}

func (m *AuthMiddleware) authorizeAndContinueInternal(c *echo.Context, next echo.HandlerFunc, ps *authz.PermissionSet) error {
	if m.options.AdminRequired && !ps.IsGlobalAdmin() {
		return c.JSON(http.StatusForbidden, common.APIError{
			Code:    "FORBIDDEN",
			Message: "You don't have permission to access this resource",
		})
	}
	return next(c)
}

// resolvePermissionsOrDeny returns the user's permission set, or an empty
// (deny-all) set if the resolver is unavailable or fails. Resolver failures
// are logged.
func (m *AuthMiddleware) resolvePermissionsOrDeny(ctx context.Context, authenticatedUser *userdomain.User) *authz.PermissionSet {
	if m.roleResolver == nil || authenticatedUser == nil {
		return authz.NewPermissionSet()
	}
	ps, err := m.roleResolver.ResolvePermissions(ctx, authenticatedUser.ID)
	if err != nil || ps == nil {
		slog.WarnContext(ctx, "failed to resolve user permissions for Echo auth", "error", err)
		return authz.NewPermissionSet()
	}
	return ps
}

// resolveApiKeyPermissionsOrDeny returns the API key's per-key PermissionSet,
// or an empty (deny-all) set on failure. Falling back to the owner's role
// permissions would defeat per-key scoping, so failures are explicitly denied.
func (m *AuthMiddleware) resolveApiKeyPermissionsOrDeny(ctx context.Context, apiKeyID string) *authz.PermissionSet {
	if m.roleResolver == nil || apiKeyID == "" {
		return authz.NewPermissionSet()
	}
	ps, err := m.roleResolver.ResolveApiKeyPermissions(ctx, apiKeyID)
	if err != nil || ps == nil {
		slog.WarnContext(ctx, "failed to resolve api key permissions for Echo auth", "error", err)
		return authz.NewPermissionSet()
	}
	return ps
}

func (m *AuthMiddleware) resolveEnvironmentAccessToken(ctx context.Context, token string) mo.Option[*environment.Environment] {
	if m.envTokenResolver == nil {
		return mo.None[*environment.Environment]()
	}

	env, err := m.envTokenResolver.ResolveEnvironmentByAccessToken(ctx, token)
	if err != nil || env == nil {
		return mo.None[*environment.Environment]()
	}

	return mo.Some(env)
}

func isPreflightInternal(c *echo.Context) bool {
	return c.Request().Method == http.MethodOptions
}

func agentSudoInternal(c *echo.Context) {
	agentUser := &userdomain.User{
		ID:       "agent",
		Email:    new("agent@getarcane.app"),
		Username: "agent",
	}
	c.Set(string(middleware.ContextKeyUserID), agentUser.ID)
	c.Set(string(middleware.ContextKeyCurrentUser), agentUser)
	c.Set(string(middleware.ContextKeyUserPermissions), authz.SudoPermissionSet())
	c.Set(string(middleware.ContextKeyAuthMethod), "agent_token")
}

func environmentScopedInternal(c *echo.Context, env *environment.Environment) *authz.PermissionSet {
	envUser := &userdomain.User{
		ID:       "environment:" + env.ID,
		Username: env.Name,
	}
	c.Set(string(middleware.ContextKeyUserID), envUser.ID)
	c.Set(string(middleware.ContextKeyCurrentUser), envUser)
	ps := authz.EnvironmentPermissionSet(env.ID)
	c.Set(string(middleware.ContextKeyUserPermissions), ps)
	c.Set(string(middleware.ContextKeyAuthMethod), "environment_access_token")
	return ps
}

func extractBearerOrCookieTokenInternal(c *echo.Context) (string, bool) {
	req := c.Request()
	authHeader := req.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
		return after, false
	}
	if tok, err := cookie.GetTokenCookie(req); err == nil && tok != "" {
		return tok, true
	}
	return "", false
}
