package auth

import (
	"bytes"
	"cmp"
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

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
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
	beginMFAAuthentication func(context.Context, string, types.SessionMeta, string) (*types.MFAChallenge, error)
	settingsService        *settings.SettingsService
}

type LoginInput struct {
	UserAgent string `header:"User-Agent"`
	Body      types.AuthLogin
}

type LoginOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Session cookie"`
	Body      types.ApiResponse[types.AuthenticationResponse]
}

type LogoutInput struct {
	SecureRefreshCookie   string            `cookie:"__Secure-refresh" doc:"Refresh cookie (HTTPS)"`
	InsecureRefreshCookie string            `cookie:"refresh" doc:"Refresh cookie (HTTP)"`
	Body                  *types.AuthLogout `required:"false"`
}

type LogoutOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Cleared session cookie"`
	Body      types.ApiResponse[types.MessageResponse]
}

type RefreshTokenInput struct {
	UserAgent             string             `header:"User-Agent"`
	SecureRefreshCookie   string             `cookie:"__Secure-refresh" doc:"Refresh cookie (HTTPS)"`
	InsecureRefreshCookie string             `cookie:"refresh" doc:"Refresh cookie (HTTP)"`
	Body                  *types.AuthRefresh `required:"false"`
}

type RefreshTokenOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Updated session cookie"`
	Body      types.ApiResponse[types.TokenRefreshResponse]
}

type ChangePasswordInput struct {
	Body types.PasswordChange
}

type UpdateMyProfileInput struct {
	types.Update[types.User]
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
			Body: types.ApiResponse[types.AuthenticationResponse]{
				Success: true,
				Data: types.AuthenticationResponse{
					Success: true,
					Status:  types.AuthenticationStatusMFARequired,
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

	expiresAt := tokenPair.ExpiresAt

	return &LoginOutput{
		SetCookie: tokenPair.CookieHeaders(cookie.SecureCookieFromContext(ctx)),
		Body: types.ApiResponse[types.AuthenticationResponse]{
			Success: true,
			Data: types.AuthenticationResponse{
				Success:      true,
				Status:       types.AuthenticationStatusAuthenticated,
				Token:        tokenPair.AccessToken,
				RefreshToken: tokenPair.RefreshToken,
				ExpiresAt:    &expiresAt,
				User:         &userResp,
			},
		},
	}, nil
}

// Logout clears the authentication session.
func (h *AuthHandler) Logout(ctx context.Context, input *LogoutInput) (*LogoutOutput, error) {
	if h.authService != nil {
		if sessionID, exists := middleware.GetCurrentSessionIDFromContext(ctx); exists {
			if err := h.authService.RevokeSession(ctx, sessionID); err != nil {
				slog.ErrorContext(ctx, "Failed to revoke session on logout; clearing cookie anyway", "sessionId", sessionID, "error", err)
			}
		}
		if refreshToken := cmp.Or(kit.FromPtr(input.Body).RefreshToken, input.SecureRefreshCookie, input.InsecureRefreshCookie); refreshToken != "" {
			if err := h.authService.RevokeSessionByRefreshToken(ctx, refreshToken); err != nil {
				slog.WarnContext(ctx, "Failed to revoke refresh token session on logout", "error", err)
			}
		}
		if userModel, exists := userdomain.CurrentUserFromContext(ctx); exists {
			h.authService.LogLogout(ctx, userModel)
		}
	}

	return &LogoutOutput{
		SetCookie: append(
			cookie.BuildClearTokenCookieStringsFor(cookie.SecureCookieFromContext(ctx)),
			cookie.BuildClearRefreshCookieStringsFor(cookie.SecureCookieFromContext(ctx))...,
		),
		Body: types.ApiResponse[types.MessageResponse]{
			Success: true,
			Data: types.MessageResponse{
				Message: "Logged out successfully",
			},
		},
	}, nil
}

// GetCurrentUser reads the current user from the database. Uses
// ToUserResponseDto (not the generic struct mapper) so the RBAC fields
// (RoleAssignments, PermissionsByEnv) are resolved via RoleService.
func (h *AuthHandler) GetCurrentUser(ctx context.Context, input *struct{}) (*handlerutil.Out[types.User], error) {
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

	return &handlerutil.Out[types.User]{
		Body: types.ApiResponse[types.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

func (h *AuthHandler) GetSessionBootstrap(ctx context.Context, _ *struct{}) (*handlerutil.Out[types.SessionBootstrap], error) {
	currentUser, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	ps, _ := middleware.PermissionsFromContext(ctx)
	bootstrap, err := h.authService.GetSessionBootstrap(ctx, currentUser.ID, ps)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to load session")
	}

	return &handlerutil.Out[types.SessionBootstrap]{
		Body: types.ApiResponse[types.SessionBootstrap]{
			Success: true,
			Data:    *bootstrap,
		},
	}, nil
}

// RefreshToken obtains a new access token using a refresh token.
func (h *AuthHandler) RefreshToken(ctx context.Context, input *RefreshTokenInput) (*RefreshTokenOutput, error) {
	refreshToken := cmp.Or(kit.FromPtr(input.Body).RefreshToken, input.SecureRefreshCookie, input.InsecureRefreshCookie)
	if refreshToken == "" {
		return nil, huma.Error401Unauthorized("Invalid or expired refresh token")
	}
	tokenPair, err := h.authService.RefreshToken(ctx, refreshToken, handlerutil.SessionMetaFromContext(ctx, input.UserAgent))
	if err != nil {
		switch {
		case errors.Is(err, common.ErrInvalidToken),
			errors.Is(err, common.ErrExpiredToken),
			errors.Is(err, common.ErrTokenValidation),
			errors.Is(err, common.ErrSessionRevoked),
			errors.Is(err, common.ErrTokenVersionMismatch),
			errors.Is(err, common.ErrUserNotFound):
			return nil, huma.Error401Unauthorized("Invalid or expired refresh token")
		default:
			return nil, huma.Error500InternalServerError("Failed to refresh token")
		}
	}

	return &RefreshTokenOutput{
		SetCookie: tokenPair.CookieHeaders(cookie.SecureCookieFromContext(ctx)),
		Body: types.ApiResponse[types.TokenRefreshResponse]{
			Success: true,
			Data: types.TokenRefreshResponse{
				Token:        tokenPair.AccessToken,
				RefreshToken: tokenPair.RefreshToken,
				ExpiresAt:    tokenPair.ExpiresAt,
			},
		},
	}, nil
}

// ChangePassword changes the current user's password.
func (h *AuthHandler) ChangePassword(ctx context.Context, input *ChangePasswordInput) (*handlerutil.Out[types.MessageResponse], error) {
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
		case errors.Is(err, common.ErrForbidden):
			return nil, huma.Error403Forbidden(err.Error())
		case errors.Is(err, common.ErrValidation):
			passwordPolicy := h.authService.passwordPolicy(ctx)
			return nil, handlerutil.Error400BadRequestWithType(err.Error(), validation.PasswordPolicyProblemType(passwordPolicy))
		default:
			return nil, huma.Error500InternalServerError("Failed to change password")
		}
	}

	return handlerutil.MessageOutput("Password changed successfully", ""), nil
}

// LogoutAllOtherSessions revokes every active session for the current user
// except the session making this request.
func (h *AuthHandler) LogoutAllOtherSessions(ctx context.Context, input *struct{}) (*handlerutil.Out[types.MessageResponse], error) {
	userModel, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	currentSessionID, _ := middleware.GetCurrentSessionIDFromContext(ctx)
	if logoutAllOtherSessionsErr := h.authService.LogoutAllOtherSessions(ctx, userModel.ID, currentSessionID); logoutAllOtherSessionsErr != nil {
		slog.ErrorContext(ctx, "Failed to revoke other sessions", "userId", userModel.ID, "error", logoutAllOtherSessionsErr)
		return nil, huma.Error500InternalServerError("failed to revoke sessions")
	}

	return handlerutil.MessageOutput("All other sessions signed out", ""), nil
}

// UpdateMyProfile lets the current user update their own displayName and email.
// OIDC-managed accounts are read-only here.
func (h *AuthHandler) UpdateMyProfile(ctx context.Context, input *UpdateMyProfileInput) (*handlerutil.Out[types.User], error) {
	currentUser, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body")
	}

	if kit.FromPtr(currentUser.OidcSubjectId) != "" && (submitted["displayName"] || submitted["email"]) {
		return nil, huma.Error403Forbidden("display name and email are managed by your identity provider")
	}

	userModel, err := h.userService.GetUser(ctx, currentUser.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get user information")
	}

	email, err := userdomain.NormalizeOptionalEmail(kit.Ternary(submitted["email"], input.Body.Email, userModel.Email))
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if !strings.EqualFold(kit.FromPtr(email), kit.FromPtr(userModel.Email)) && h.settingsService.GetBoolSetting(ctx, "oidcMergeAccounts", false) {
		return nil, huma.Error403Forbidden("email cannot be changed while OIDC account merging is enabled")
	}

	preferences := userModel.Preferences
	if p := input.Body.Preferences; submitted["preferences"] {
		// Huma's enum tags cannot express these checks. The "default" accent
		// sentinel means "no override"; anything else must be a color literal
		// safe to inject into CSS.
		if v := p.AccentColor; v != nil && *v != "" && *v != "default" && !types.SafeAccentColor.MatchString(*v) {
			return nil, huma.Error400BadRequest("invalid accentColor value")
		}
		// Keys are environment IDs; the caps keep one user's preferences from growing without bound.
		if v := p.EnvironmentColors; v != nil {
			for id, color := range *v {
				if len(*v) > 1000 || id == "" || len(id) > 64 || !types.SafeAccentColor.MatchString(color) {
					return nil, huma.Error400BadRequest("invalid environmentColors value")
				}
			}
		}
		// Keep the landing page a same-origin relative path. The frontend
		// additionally validates it against the known nav pages, falling back to
		// /dashboard.
		if v := p.DefaultLandingPage; v != nil && (!strings.HasPrefix(*v, "/") || strings.HasPrefix(*v, "//")) {
			return nil, huma.Error400BadRequest("invalid defaultLandingPage value")
		}
		// Copy every set (non-nil) field. Reflection keeps this merge-only update
		// free of per-field branches, so a new preference needs no handler change.
		dstValue := reflect.ValueOf(&preferences).Elem()
		srcValue := reflect.ValueOf(p)
		for i := range srcValue.NumField() {
			if field := srcValue.Field(i); field.Kind() == reflect.Pointer && !field.IsNil() {
				dstValue.Field(i).Set(field)
			}
		}
	}

	updated, err := h.userService.UpdateUser(ctx, &userdomain.User{
		BaseModel:   userModel.BaseModel,
		Username:    userModel.Username,
		DisplayName: kit.Ternary(submitted["displayName"], input.Body.DisplayName, userModel.DisplayName),
		Email:       email,
		Locale:      kit.Ternary(submitted["locale"], input.Body.Locale, userModel.Locale),
		TimeFormat:  kit.Ternary(submitted["timeFormat"], input.Body.TimeFormat, userModel.TimeFormat),
		FontSize:    kit.Ternary(submitted["fontSize"], input.Body.FontSize, userModel.FontSize),
		Preferences: preferences,
	}, nil)
	if err != nil {
		if errors.Is(err, userdomain.ErrEmailTaken) {
			return nil, huma.Error409Conflict(userdomain.ErrEmailTaken.Error())
		}
		return nil, huma.Error500InternalServerError("Failed to update user")
	}
	h.authService.InvalidateUserTokenCache(updated.ID)

	out, err := h.userService.ToUserResponseDto(ctx, *updated)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to map user")
	}

	return &handlerutil.Out[types.User]{
		Body: types.ApiResponse[types.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UploadMyAvatar lets the current user upload a custom profile picture.
// Accepts PNG, JPEG, or WebP images up to the configured avatar upload limit.
func (h *AuthHandler) UploadMyAvatar(ctx context.Context, input *UploadMyAvatarInput) (*handlerutil.Out[types.User], error) {
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

	const defaultAvatarMaxUploadSizeMb = 2
	maxSizeMb := defaultAvatarMaxUploadSizeMb
	if h.settingsService != nil {
		maxSizeMb = h.settingsService.GetIntSetting(ctx, "avatarMaxUploadSizeMb", defaultAvatarMaxUploadSizeMb)
		maxSizeMb = kit.Ternary(maxSizeMb <= 0, defaultAvatarMaxUploadSizeMb, maxSizeMb)
	}
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

	// Only PNG, JPEG, and WebP avatars are accepted.
	mimeType := http.DetectContentType(data)
	if mimeType != "image/png" && mimeType != "image/jpeg" && mimeType != "image/webp" {
		return nil, huma.Error400BadRequest("unsupported image format: only PNG, JPEG and WebP are accepted")
	}
	data, mimeType = normalizeAvatarImage(data, mimeType)

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

	return &handlerutil.Out[types.User]{
		Body: types.ApiResponse[types.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// DeleteMyAvatar removes the current user's custom profile picture.
func (h *AuthHandler) DeleteMyAvatar(ctx context.Context, input *struct{}) (*handlerutil.Out[types.User], error) {
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

	return &handlerutil.Out[types.User]{
		Body: types.ApiResponse[types.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

func normalizeAvatarImage(data []byte, mimeType string) ([]byte, string) {
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
	if err != nil || imageHasTransparency(img) {
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

func imageHasTransparency(img image.Image) bool {
	if opaque, ok := img.(interface{ Opaque() bool }); ok {
		return !opaque.Opaque()
	}
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

// parseSecurityRequirements extracts security requirements from a Huma operation.
func parseSecurityRequirements(api huma.API, ctx operationProvider) securityRequirements {
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

// tryBearerAuth attempts Bearer token authentication. Returns the
// authenticated user on success, or the underlying error from VerifyToken so
// the caller can distinguish a missing/invalid token from a token-version
// mismatch (which requires clearing the stale cookie).
func tryBearerAuth(ctx huma.Context, authService *AuthService) (*userdomain.User, string, error) {
	token, fromCookie := extractBearerToken(ctx)
	if token == "" {
		return nil, "", nil
	}
	return kit.Ternary(fromCookie, authService.VerifyBrowserToken, authService.VerifyToken)(ctx.Context(), token)
}

func tryEnvironmentAccessTokenAuth(ctx huma.Context, resolver EnvironmentAccessTokenResolver, token string) (*userdomain.User, *environment.Environment, bool) {
	if resolver == nil || strings.TrimSpace(token) == "" {
		return nil, nil, false
	}

	env, err := resolver.ResolveEnvironmentByAccessToken(ctx.Context(), token)
	if err != nil || env == nil {
		return nil, nil, false
	}

	return createEnvironmentUser(env), env, true
}

// createAgentSudoUser creates a sudo user for agent authentication.
// The sudo PermissionSet attached to the context by the agent token path
// bypasses every check; the user's Roles field is intentionally empty.
func createAgentSudoUser() *userdomain.User {
	return &userdomain.User{
		ID:       "agent",
		Email:    new("agent@getarcane.app"),
		Username: "agent",
	}
}

// applyProxiedIconCatalog copies the requesting user's icon catalog
// preference, forwarded by the manager, onto the synthetic user this request
// authenticates as. Without it the synthetic user has no preferences and every
// proxied icon resolves against the default catalog.
//
// Only called on synthetic-user auth paths (agent token, environment access
// token), where the header is set by the manager after it strips any
// client-supplied value. Real-user auth paths never read it.
func applyProxiedIconCatalog(ctx huma.Context, authenticatedUser *userdomain.User) {
	if authenticatedUser == nil {
		return
	}
	catalog := strings.TrimSpace(ctx.Header(middleware.HeaderIconCatalog))
	if catalog == "" {
		return
	}
	authenticatedUser.Preferences.IconCatalog = &catalog
}

func createEnvironmentUser(env *environment.Environment) *userdomain.User {
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

		if newCtx, ok := tryAgentAuth(ctx, cfg); ok {
			next(newCtx)
			return
		}

		reqs := parseSecurityRequirements(api, ctx)
		if !reqs.isRequired {
			// Public routes (e.g. logout) still learn who the caller is when a
			// valid bearer token is present, but never fail the request.
			if authenticatedUser, sessionID, err := tryBearerAuth(ctx, authService); err == nil && authenticatedUser != nil {
				newCtx := setUserInContext(ctx.Context(), authenticatedUser, resolveUserPermissions(ctx.Context(), permResolver, authenticatedUser))
				ctx = huma.WithContext(ctx, context.WithValue(newCtx, middleware.ContextKeyCurrentSessionID, sessionID))
			}
			next(ctx)
			return
		}

		if reqs.apiKeyAuth && ctx.Header(middleware.HeaderApiKey) != "" {
			handleApiKeyAuth(api, ctx, authService, apiKeyService, permResolver, envTokenResolver, reqs.bearerAuth, next)
			return
		}

		if authenticatedUser, env, ok := tryEnvironmentAccessTokenAuth(ctx, envTokenResolver, ctx.Header(middleware.HeaderAgentToken)); ok {
			applyProxiedIconCatalog(ctx, authenticatedUser)
			newCtx := setUserInContext(ctx.Context(), authenticatedUser, authz.EnvironmentPermissionSet(env.ID))
			next(huma.WithContext(ctx, newCtx))
			return
		}

		if reqs.bearerAuth {
			nextCtx, handled := handleBearerAuth(api, ctx, authService, permResolver)
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

// tryAgentAuth authenticates an agent-mode request presenting the
// agent token as a bootstrap pairing, agent token, or API key header.
func tryAgentAuth(ctx huma.Context, cfg *config.Config) (huma.Context, bool) {
	if cfg == nil || !cfg.AgentMode {
		return ctx, false
	}
	bootstrapPairing := strings.HasPrefix(ctx.URL().Path, middleware.AgentPairingPrefix) &&
		AgentTokenMatches(ctx.Header(middleware.HeaderAgentBootstrap), cfg.AgentToken)
	if !bootstrapPairing &&
		!AgentTokenMatches(ctx.Header(middleware.HeaderAgentToken), cfg.AgentToken) &&
		!AgentTokenMatches(ctx.Header(middleware.HeaderApiKey), cfg.AgentToken) {
		return ctx, false
	}
	authenticatedUser := createAgentSudoUser()
	applyProxiedIconCatalog(ctx, authenticatedUser)
	authCtx := setUserInContext(ctx.Context(), authenticatedUser, authz.SudoPermissionSet())
	updatePath := strings.Contains(ctx.URL().Path, "/containers/") && strings.HasSuffix(ctx.URL().Path, "/update")
	if ctx.Method() == http.MethodPost && updatePath {
		if initiatorID := strings.TrimSpace(ctx.Header(middleware.HeaderUpdateInitiatorID)); initiatorID != "" {
			initiator := types.ActivityStartedBy{
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

// handleApiKeyAuth handles the API-key-present branch. Invalid user
// keys fail closed; only recognized environment tokens defer to bearer auth
// when both credentials are present on a bearer-capable operation.
func handleApiKeyAuth(
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
	if authenticatedUser, key, err := apiKeyService.ValidateApiKeyWithID(ctx.Context(), ctx.Header(middleware.HeaderApiKey)); err == nil && authenticatedUser != nil {
		// Personal keys inherit the owner's role permissions (same resolution
		// as session auth); scoped keys are limited to their own grants.
		var ps *authz.PermissionSet
		if key.Kind == apikey.ApiKeyKindPersonal {
			ps = resolveUserPermissions(ctx.Context(), permResolver, authenticatedUser)
		} else if permResolver != nil && key.ID != "" {
			if ps, err = permResolver.ResolveApiKeyPermissions(ctx.Context(), key.ID); err != nil {
				slog.WarnContext(ctx.Context(), "failed to resolve api key permissions", "error", err, "apiKeyId", key.ID)
				ps = nil
			}
		}
		newCtx := setUserInContext(ctx.Context(), authenticatedUser, ps)
		newCtx = context.WithValue(newCtx, middleware.ContextKeyApiKeyID, key.ID)
		next(huma.WithContext(ctx, newCtx))
		return
	}
	if authenticatedUser, env, ok := tryEnvironmentAccessTokenAuth(ctx, envTokenResolver, ctx.Header(middleware.HeaderApiKey)); ok {
		if token, _ := extractBearerToken(ctx); allowBearerFallback && token != "" {
			nextCtx, handled := handleBearerAuth(api, ctx, authService, permResolver)
			if handled {
				if nextCtx != nil {
					next(nextCtx)
				}
				return
			}
		}
		applyProxiedIconCatalog(ctx, authenticatedUser)
		newCtx := setUserInContext(ctx.Context(), authenticatedUser, authz.EnvironmentPermissionSet(env.ID))
		next(huma.WithContext(ctx, newCtx))
		return
	}
	_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Unauthorized: invalid API key")
}

func handleBearerAuth(api huma.API, ctx huma.Context, authService *AuthService, permResolver PermissionResolver) (huma.Context, bool) {
	authenticatedUser, sessionID, err := tryBearerAuth(ctx, authService)
	if err == nil && authenticatedUser != nil {
		ps := resolveUserPermissions(ctx.Context(), permResolver, authenticatedUser)
		newCtx := setUserInContext(ctx.Context(), authenticatedUser, ps)
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

// resolveUserPermissions asks the RoleService for the user's resolved
// PermissionSet. If RoleService is unavailable or the lookup fails (boot-time
// edge cases, broken DB) it returns nil and logs a warning — handlers then
// see deny-all, which is the safe default.
func resolveUserPermissions(ctx context.Context, permResolver PermissionResolver, authenticatedUser *userdomain.User) *authz.PermissionSet {
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

// extractBearerToken extracts the JWT token and reports whether it came from a cookie.
func extractBearerToken(ctx huma.Context) (string, bool) {
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

// setUserInContext adds the authenticated user and the resolved
// PermissionSet to the context. Callers must supply a non-nil PermissionSet;
// pass authz.NewPermissionSet() to express deny-all.
func setUserInContext(ctx context.Context, authenticatedUser *userdomain.User, ps *authz.PermissionSet) context.Context {
	if ps == nil {
		ps = authz.NewPermissionSet()
	}
	ctx = context.WithValue(ctx, middleware.ContextKeyUserID, authenticatedUser.ID)
	ctx = context.WithValue(ctx, types.CurrentUserContextKey{}, authenticatedUser)
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
	if c.Request().Method == http.MethodOptions {
		return next(c)
	}

	req := c.Request()
	if strings.HasPrefix(req.URL.Path, middleware.AgentPairingPrefix) &&
		AgentTokenMatches(req.Header.Get(middleware.HeaderAgentBootstrap), m.cfg.AgentToken) {
		slog.InfoContext(ctx, "Agent auth: bootstrap pairing accepted", "path", req.URL.Path, "method", req.Method)
		agentSudo(c)
		return next(c)
	}

	if AgentTokenMatches(req.Header.Get(middleware.HeaderAgentToken), m.cfg.AgentToken) {
		agentSudo(c)
		return next(c)
	}

	// Check for API key as agent token
	if AgentTokenMatches(req.Header.Get(middleware.HeaderApiKey), m.cfg.AgentToken) {
		agentSudo(c)
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
			ps := environmentScoped(c, env)
			return m.authorizeAndContinue(c, next, ps)
		}
	}

	// First, check for API key in X-API-Key header
	if apiKey := req.Header.Get(middleware.HeaderApiKey); apiKey != "" {
		return m.apiKeyHeaderAuth(ctx, c, next, apiKey)
	}

	token, hasBearer := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	if !hasBearer {
		token, _ = cookie.GetTokenCookie(req)
	}
	if token == "" {
		if m.options.SuccessOptional {
			return next(c)
		}
		return c.JSON(http.StatusUnauthorized, common.APIError{
			Code:    common.APIErrorCodeUnauthorized,
			Message: "Authentication required",
		})
	}

	verify := kit.Ternary(hasBearer, m.authService.VerifyToken, m.authService.VerifyBrowserToken)
	authenticatedUser, sessionID, err := verify(ctx, token)
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
	return m.authorizeAndContinue(c, next, ps)
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
			// Falling back to the owner's role permissions would defeat per-key
			// scoping, so scoped-key resolution failures are explicitly denied.
			switch {
			case key.Kind == apikey.ApiKeyKindPersonal:
				ps = m.resolvePermissionsOrDeny(ctx, authenticatedUser)
			case m.roleResolver == nil || key.ID == "":
				ps = authz.NewPermissionSet()
			default:
				if ps, err = m.roleResolver.ResolveApiKeyPermissions(ctx, key.ID); err != nil || ps == nil {
					slog.WarnContext(ctx, "failed to resolve api key permissions for Echo auth", "error", err)
					ps = authz.NewPermissionSet()
				}
			}
			c.Set(string(middleware.ContextKeyUserID), authenticatedUser.ID)
			c.Set(string(middleware.ContextKeyCurrentUser), authenticatedUser)
			c.Set(string(middleware.ContextKeyUserPermissions), ps)
			c.Set(string(middleware.ContextKeyAuthMethod), "api_key")
			return m.authorizeAndContinue(c, next, ps)
		}
	}
	if env, ok := m.resolveEnvironmentAccessToken(ctx, apiKey).Get(); ok {
		ps := environmentScoped(c, env)
		return m.authorizeAndContinue(c, next, ps)
	}
	return c.JSON(http.StatusUnauthorized, common.APIError{
		Code:    common.APIErrorCodeUnauthorized,
		Message: "Invalid or expired API key",
	})
}

func (m *AuthMiddleware) authorizeAndContinue(c *echo.Context, next echo.HandlerFunc, ps *authz.PermissionSet) error {
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

func agentSudo(c *echo.Context) {
	agentUser := createAgentSudoUser()
	c.Set(string(middleware.ContextKeyUserID), agentUser.ID)
	c.Set(string(middleware.ContextKeyCurrentUser), agentUser)
	c.Set(string(middleware.ContextKeyUserPermissions), authz.SudoPermissionSet())
	c.Set(string(middleware.ContextKeyAuthMethod), "agent_token")
}

func environmentScoped(c *echo.Context, env *environment.Environment) *authz.PermissionSet {
	envUser := createEnvironmentUser(env)
	c.Set(string(middleware.ContextKeyUserID), envUser.ID)
	c.Set(string(middleware.ContextKeyCurrentUser), envUser)
	ps := authz.EnvironmentPermissionSet(env.ID)
	c.Set(string(middleware.ContextKeyUserPermissions), ps)
	c.Set(string(middleware.ContextKeyAuthMethod), "environment_access_token")
	return ps
}
