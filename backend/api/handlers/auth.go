package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	stdimage "image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cookie"
	"github.com/getarcaneapp/arcane/types/v2/auth"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/user"
)

type AuthHandler struct {
	userService     *services.UserService
	authService     *services.AuthService
	oidcService     *services.OidcService
	settingsService *services.SettingsService
}

// --- Huma Input/Output Wrappers ---
// These wrap the types from the types package for Huma's input/output handling.

type LoginInput struct {
	UserAgent string `header:"User-Agent"`
	Body      auth.Login
}

type LoginOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Session cookie"`
	Body      base.ApiResponse[auth.LoginResponse]
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

type ChangePasswordOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type LogoutAllOtherSessionsOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type UpdateMyProfileInput struct {
	Body struct {
		DisplayName *string `json:"displayName,omitempty"`
		Email       *string `json:"email,omitempty"`
		Locale      *string `json:"locale,omitempty"`
		FontSize    *int    `json:"fontSize,omitempty" minimum:"12" maximum:"20"`
	}
}

type UpdateMyProfileOutput struct {
	Body base.ApiResponse[user.User]
}

type GetCurrentUserOutput struct {
	Body base.ApiResponse[user.User]
}

type UploadMyAvatarInput struct {
	RawBody multipart.Form `contentType:"multipart/form-data"`
}

type UploadMyAvatarOutput struct {
	Body base.ApiResponse[user.User]
}

type DeleteMyAvatarOutput struct {
	Body base.ApiResponse[user.User]
}

// RegisterAuth registers authentication routes using Huma.
func RegisterAuth(api huma.API, userService *services.UserService, authService *services.AuthService, oidcService *services.OidcService, settingsService *services.SettingsService) {
	h := &AuthHandler{
		userService:     userService,
		authService:     authService,
		oidcService:     oidcService,
		settingsService: settingsService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "login",
		Method:      http.MethodPost,
		Path:        "/auth/login",
		Summary:     "Login",
		Description: "Authenticate a user with username and password",
		Tags:        []string{"Auth"},
		Security:    []map[string][]string{},
	}, h.Login)

	huma.Register(api, huma.Operation{
		OperationID: "logout",
		Method:      http.MethodPost,
		Path:        "/auth/logout",
		Summary:     "Logout",
		Description: "Clear authentication session",
		Tags:        []string{"Auth"},
		Security:    []map[string][]string{},
	}, h.Logout)

	huma.Register(api, huma.Operation{
		OperationID: "get-current-user",
		Method:      http.MethodGet,
		Path:        "/auth/me",
		Summary:     "Get current user",
		Description: "Get the currently authenticated user's information",
		Tags:        []string{"Auth"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetCurrentUser)

	huma.Register(api, huma.Operation{
		OperationID: "refresh-token",
		Method:      http.MethodPost,
		Path:        "/auth/refresh",
		Summary:     "Refresh token",
		Description: "Obtain a new access token using a refresh token",
		Tags:        []string{"Auth"},
		Security:    []map[string][]string{},
	}, h.RefreshToken)

	huma.Register(api, huma.Operation{
		OperationID: "change-password",
		Method:      http.MethodPost,
		Path:        "/auth/password",
		Summary:     "Change password",
		Description: "Change the current user's password",
		Tags:        []string{"Auth"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ChangePassword)

	huma.Register(api, huma.Operation{
		OperationID: "logout-all-other-sessions",
		Method:      http.MethodPost,
		Path:        "/auth/sessions/logout-all",
		Summary:     "Logout all other sessions",
		Description: "Revoke every session for the current user except the one making this request",
		Tags:        []string{"Auth"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.LogoutAllOtherSessions)

	huma.Register(api, huma.Operation{
		OperationID: "update-my-profile",
		Method:      http.MethodPut,
		Path:        "/auth/me/profile",
		Summary:     "Update own profile",
		Description: "Update the current user's display name and email. Forbidden for OIDC-managed accounts.",
		Tags:        []string{"Auth"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.UpdateMyProfile)

	huma.Register(api, huma.Operation{
		OperationID: "upload-my-avatar",
		Method:      http.MethodPost,
		Path:        "/auth/me/avatar",
		Summary:     "Upload own avatar",
		Description: "Upload a custom profile picture (PNG, JPEG or WebP). Replaces any existing avatar.",
		Tags:        []string{"Auth"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		RequestBody: &huma.RequestBody{
			Required: true,
			Content: map[string]*huma.MediaType{
				"multipart/form-data": {
					Schema: &huma.Schema{
						Type: "object",
						Properties: map[string]*huma.Schema{
							"file": {Type: "string", Format: "binary"},
						},
					},
				},
			},
		},
	}, h.UploadMyAvatar)

	huma.Register(api, huma.Operation{
		OperationID: "delete-my-avatar",
		Method:      http.MethodDelete,
		Path:        "/auth/me/avatar",
		Summary:     "Delete own avatar",
		Description: "Remove the current user's custom profile picture, reverting to the default avatar.",
		Tags:        []string{"Auth"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.DeleteMyAvatar)
}

// Login authenticates a user and returns tokens.
func (h *AuthHandler) Login(ctx context.Context, input *LoginInput) (*LoginOutput, error) {
	if h.authService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	localAuthEnabled, err := h.authService.IsLocalAuthEnabled(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.AuthSettingsCheckError{Err: err}).Error())
	}
	if !localAuthEnabled {
		return nil, huma.Error400BadRequest((&common.LocalAuthDisabledError{}).Error())
	}

	userModel, tokenPair, err := h.authService.Login(ctx, input.Body.Username, input.Body.Password, sessionMetaFromContextInternal(ctx, input.UserAgent))
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidCredentials):
			return nil, huma.Error401Unauthorized((&common.InvalidCredentialsError{}).Error())
		case errors.Is(err, services.ErrLocalAuthDisabled):
			return nil, huma.Error400BadRequest((&common.LocalAuthDisabledError{}).Error())
		default:
			return nil, huma.Error500InternalServerError((&common.AuthFailedError{Err: err}).Error())
		}
	}

	userResp, err := h.userService.ToUserResponseDto(ctx, *userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserMappingError{Err: err}).Error())
	}

	maxAge := max(int(time.Until(tokenPair.ExpiresAt).Seconds()), 0)
	maxAge += 60

	return &LoginOutput{
		SetCookie: []string{cookie.BuildTokenCookieStringFor(maxAge, tokenPair.AccessToken, cookie.SecureCookieFromContext(ctx))},
		Body: base.ApiResponse[auth.LoginResponse]{
			Success: true,
			Data: auth.LoginResponse{
				Token:        tokenPair.AccessToken,
				RefreshToken: tokenPair.RefreshToken,
				ExpiresAt:    tokenPair.ExpiresAt,
				User:         userResp,
			},
		},
	}, nil
}

// Logout clears the authentication session.
func (h *AuthHandler) Logout(ctx context.Context, input *struct{}) (*LogoutOutput, error) {
	if h.authService != nil {
		if sessionID, exists := humamw.GetCurrentSessionIDFromContext(ctx); exists {
			if err := h.authService.RevokeSession(ctx, sessionID); err != nil {
				slog.ErrorContext(ctx, "Failed to revoke session on logout; clearing cookie anyway", "sessionID", sessionID, "error", err)
			}
		}
		if userModel, exists := humamw.GetCurrentUserFromContext(ctx); exists {
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

// GetCurrentUser returns the currently authenticated user's information.
// Uses ToUserResponseDto (not the generic struct mapper) so the RBAC fields
// (RoleAssignments, PermissionsByEnv) are resolved via RoleService.
func (h *AuthHandler) GetCurrentUser(ctx context.Context, input *struct{}) (*GetCurrentUserOutput, error) {
	if h.userService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	userID, exists := humamw.GetUserIDFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	userModel, err := h.userService.GetUser(ctx, userID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserRetrievalError{Err: err}).Error())
	}

	out, err := h.userService.ToUserResponseDto(ctx, *userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserMappingError{Err: err}).Error())
	}

	return &GetCurrentUserOutput{
		Body: base.ApiResponse[user.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// RefreshToken obtains a new access token using a refresh token.
func (h *AuthHandler) RefreshToken(ctx context.Context, input *RefreshTokenInput) (*RefreshTokenOutput, error) {
	if h.authService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	tokenPair, err := h.authService.RefreshToken(ctx, input.Body.RefreshToken, sessionMetaFromContextInternal(ctx, input.UserAgent))
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidToken), errors.Is(err, services.ErrExpiredToken), common.IsTokenValidationError(err), common.IsSessionRevokedError(err), errors.Is(err, services.ErrTokenVersionMismatch):
			return nil, huma.Error401Unauthorized((&common.InvalidTokenError{}).Error())
		default:
			return nil, huma.Error500InternalServerError((&common.TokenRefreshError{Err: err}).Error())
		}
	}

	maxAge := max(int(time.Until(tokenPair.ExpiresAt).Seconds()), 0)
	maxAge += 60

	return &RefreshTokenOutput{
		SetCookie: []string{cookie.BuildTokenCookieStringFor(maxAge, tokenPair.AccessToken, cookie.SecureCookieFromContext(ctx))},
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

func sessionMetaFromContextInternal(ctx context.Context, userAgent string) auth.SessionMeta {
	return auth.SessionMeta{
		UserAgent: userAgent,
		IPAddress: humamw.GetRemoteAddrFromContext(ctx),
	}
}

// ChangePassword changes the current user's password.
func (h *AuthHandler) ChangePassword(ctx context.Context, input *ChangePasswordInput) (*ChangePasswordOutput, error) {
	if h.authService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	userModel, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	if input.Body.CurrentPassword == "" {
		return nil, huma.Error400BadRequest((&common.PasswordRequiredError{}).Error())
	}

	currentSessionID, _ := humamw.GetCurrentSessionIDFromContext(ctx)
	err = h.authService.ChangePassword(ctx, userModel.ID, input.Body.CurrentPassword, input.Body.NewPassword, currentSessionID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidCredentials):
			return nil, huma.Error401Unauthorized((&common.IncorrectPasswordError{}).Error())
		default:
			return nil, huma.Error500InternalServerError((&common.PasswordChangeError{Err: err}).Error())
		}
	}

	return &ChangePasswordOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Password changed successfully",
			},
		},
	}, nil
}

// LogoutAllOtherSessions revokes every active session for the current user
// except the session making this request.
func (h *AuthHandler) LogoutAllOtherSessions(ctx context.Context, input *struct{}) (*LogoutAllOtherSessionsOutput, error) {
	if h.authService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	userModel, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	currentSessionID, _ := humamw.GetCurrentSessionIDFromContext(ctx)
	if err := h.authService.LogoutAllOtherSessions(ctx, userModel.ID, currentSessionID); err != nil {
		return nil, huma.Error500InternalServerError("failed to revoke sessions: " + err.Error())
	}

	return &LogoutAllOtherSessionsOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "All other sessions signed out",
			},
		},
	}, nil
}

// UpdateMyProfile lets the current user update their own displayName and email.
// OIDC-managed accounts are read-only here.
func (h *AuthHandler) UpdateMyProfile(ctx context.Context, input *UpdateMyProfileInput) (*UpdateMyProfileOutput, error) {
	if h.userService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	currentUser, err := requireUserInternal(ctx)
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
		return nil, huma.Error500InternalServerError((&common.UserRetrievalError{Err: err}).Error())
	}

	if input.Body.DisplayName != nil {
		userModel.DisplayName = input.Body.DisplayName
	}
	if input.Body.Email != nil {
		normalized, err := normalizeOptionalEmailInternal(input.Body.Email)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		userModel.Email = normalized
	}
	if input.Body.Locale != nil {
		userModel.Locale = input.Body.Locale
	}
	if input.Body.FontSize != nil {
		userModel.FontSize = input.Body.FontSize
	}

	updated, err := h.userService.UpdateUser(ctx, userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserUpdateError{Err: err}).Error())
	}

	out, err := h.userService.ToUserResponseDto(ctx, *updated)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserMappingError{Err: err}).Error())
	}

	return &UpdateMyProfileOutput{
		Body: base.ApiResponse[user.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UploadMyAvatar lets the current user upload a custom profile picture.
// Accepts PNG, JPEG, or WebP images up to the configured avatar upload limit.
func (h *AuthHandler) UploadMyAvatar(ctx context.Context, input *UploadMyAvatarInput) (*UploadMyAvatarOutput, error) {
	if h.userService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	currentUser, err := requireUserInternal(ctx)
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
	if _, err := buf.ReadFrom(io.LimitReader(f, maxSizeBytes+1)); err != nil {
		return nil, huma.Error500InternalServerError("failed to read file data: " + err.Error())
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

	if err := h.userService.UploadAvatar(ctx, currentUser.ID, data, mimeType); err != nil {
		slog.ErrorContext(ctx, "Failed to save avatar", "user_id", currentUser.ID, "error", err)
		return nil, huma.Error500InternalServerError("failed to save avatar")
	}

	// Reload user so the response reflects the new AvatarURL
	updatedUser, err := h.userService.GetUser(ctx, currentUser.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserRetrievalError{Err: err}).Error())
	}

	out, err := h.userService.ToUserResponseDto(ctx, *updatedUser)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserMappingError{Err: err}).Error())
	}

	return &UploadMyAvatarOutput{
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
	if maxSizeMb <= 0 {
		return defaultAvatarMaxUploadSizeMb
	}
	return maxSizeMb
}

// DeleteMyAvatar removes the current user's custom profile picture.
func (h *AuthHandler) DeleteMyAvatar(ctx context.Context, input *struct{}) (*DeleteMyAvatarOutput, error) {
	if h.userService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	currentUser, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	if err := h.userService.DeleteAvatar(ctx, currentUser.ID); err != nil {
		slog.ErrorContext(ctx, "Failed to delete avatar", "user_id", currentUser.ID, "error", err)
		return nil, huma.Error500InternalServerError("failed to delete avatar")
	}

	updatedUser, err := h.userService.GetUser(ctx, currentUser.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserRetrievalError{Err: err}).Error())
	}

	out, err := h.userService.ToUserResponseDto(ctx, *updatedUser)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserMappingError{Err: err}).Error())
	}

	return &DeleteMyAvatarOutput{
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

	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return data, mimeType
	}
	if config.Width > maxAvatarNormalizePixels/config.Height {
		return data, mimeType
	}

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil || imageHasTransparencyInternal(img) {
		return data, mimeType
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 92}); err != nil {
		return data, mimeType
	}
	if out.Len() == 0 || out.Len() >= len(data) {
		return data, mimeType
	}

	return out.Bytes(), "image/jpeg"
}

func imageHasTransparencyInternal(img stdimage.Image) bool {
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
