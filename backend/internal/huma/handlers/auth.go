package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/cookie"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/types/auth"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/user"
)

type AuthHandler struct {
	userService *services.UserService
	authService *services.AuthService
	oidcService *services.OidcService
}

// --- Huma Input/Output Wrappers ---
// These wrap the types from the types package for Huma's input/output handling.

type LoginInput struct {
	Body auth.Login
}

type LoginOutput struct {
	SetCookie string `header:"Set-Cookie" doc:"Session cookie"`
	Body      base.ApiResponse[auth.LoginResponse]
}

type LogoutOutput struct {
	SetCookie string `header:"Set-Cookie" doc:"Cleared session cookie"`
	Body      base.ApiResponse[base.MessageResponse]
}

type RefreshTokenInput struct {
	Body auth.Refresh
}

type RefreshTokenOutput struct {
	SetCookie string `header:"Set-Cookie" doc:"Updated session cookie"`
	Body      base.ApiResponse[auth.TokenRefreshResponse]
}

type ChangePasswordInput struct {
	Body auth.PasswordChange
}

type ChangePasswordOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type GetCurrentUserOutput struct {
	Body base.ApiResponse[user.User]
}

// RegisterAuth registers authentication routes using Huma.
func RegisterAuth(api huma.API, userService *services.UserService, authService *services.AuthService, oidcService *services.OidcService) {
	h := &AuthHandler{
		userService: userService,
		authService: authService,
		oidcService: oidcService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "login",
		Method:      http.MethodPost,
		Path:        "/auth/login",
		Summary:     "Login",
		Description: "Authenticate a user with username and password",
		Tags:        []string{"Auth"},
	}, h.Login)

	huma.Register(api, huma.Operation{
		OperationID: "logout",
		Method:      http.MethodPost,
		Path:        "/auth/logout",
		Summary:     "Logout",
		Description: "Clear authentication session",
		Tags:        []string{"Auth"},
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

	userModel, tokenPair, err := h.authService.Login(ctx, input.Body.Username, input.Body.Password)
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

	var userResp user.User
	if mapErr := mapper.MapStruct(userModel, &userResp); mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.UserMappingError{Err: mapErr}).Error())
	}

	maxAge := max(int(time.Until(tokenPair.ExpiresAt).Seconds()), 0)
	maxAge += 60

	return &LoginOutput{
		SetCookie: cookie.BuildTokenCookieString(maxAge, tokenPair.AccessToken),
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
	return &LogoutOutput{
		SetCookie: cookie.BuildClearTokenCookieString(),
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Logged out successfully",
			},
		},
	}, nil
}

// GetCurrentUser returns the currently authenticated user's information.
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

	var out user.User
	if mapErr := mapper.MapStruct(userModel, &out); mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.UserMappingError{Err: mapErr}).Error())
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

	tokenPair, err := h.authService.RefreshToken(ctx, input.Body.RefreshToken)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidToken), errors.Is(err, services.ErrExpiredToken):
			return nil, huma.Error401Unauthorized((&common.InvalidTokenError{}).Error())
		default:
			return nil, huma.Error500InternalServerError((&common.TokenRefreshError{Err: err}).Error())
		}
	}

	maxAge := max(int(time.Until(tokenPair.ExpiresAt).Seconds()), 0)
	maxAge += 60

	return &RefreshTokenOutput{
		SetCookie: cookie.BuildTokenCookieString(maxAge, tokenPair.AccessToken),
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
func (h *AuthHandler) ChangePassword(ctx context.Context, input *ChangePasswordInput) (*ChangePasswordOutput, error) {
	if h.authService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	userModel, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	if input.Body.CurrentPassword == "" {
		return nil, huma.Error400BadRequest((&common.PasswordRequiredError{}).Error())
	}

	err := h.authService.ChangePassword(ctx, userModel.ID, input.Body.CurrentPassword, input.Body.NewPassword)
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
