package user

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/userctx"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/validation"
)

// UserHandler handles user management endpoints.
type UserHandler struct {
	userService              *UserService
	invalidateUserTokenCache func(string)
	settingsService          *settings.SettingsService
	applyDefaultRole         func(context.Context, string) error
}

type ListUsersInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type CreateUserInput struct {
	types.Create[types.User]
}

type GetUserInput struct {
	UserID string `path:"userId" doc:"User ID"`
}

type UpdateUserInput struct {
	UserID string `path:"userId" doc:"User ID"`
	types.Update[types.User]
}

type DeleteUserInput struct {
	UserID string `path:"userId" doc:"User ID"`
}

type GetUserAvatarInput struct {
	UserID string `path:"userId" doc:"User ID"`
}

type GetUserAvatarOutput struct {
	ContentType         string `header:"Content-Type"`
	CacheControl        string `header:"Cache-Control"`
	XContentTypeOptions string `header:"X-Content-Type-Options"`
	Body                []byte
}

// ListUsers returns a paginated list of users.
func (h *UserHandler) ListUsers(ctx context.Context, input *ListUsersInput) (*handlerutil.Page[types.User], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	users, paginationResp, err := h.userService.ListUsersPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list users: " + err.Error())
	}

	return &handlerutil.Page[types.User]{
		Body: types.Paginated[types.User]{
			Success:    true,
			Data:       users,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// CreateUser creates a new user.
func (h *UserHandler) CreateUser(ctx context.Context, input *CreateUserInput) (*handlerutil.Out[types.User], error) {
	username, err := normalizeUsername(input.Body.Username)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	email, err := NormalizeOptionalEmail(input.Body.Email)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	passwordPolicy := h.passwordPolicy(ctx)
	if validatePasswordPolicyErr := validation.ValidatePasswordPolicy(input.Body.Password, passwordPolicy); validatePasswordPolicyErr != nil {
		return nil, handlerutil.Error400BadRequestWithType(validatePasswordPolicyErr.Error(), validation.PasswordPolicyProblemType(passwordPolicy))
	}

	hashedPassword, err := h.userService.HashPassword(ctx, input.Body.Password)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to hash password")
	}

	createdUser, err := h.userService.CreateUser(ctx, &User{
		Username:     username,
		PasswordHash: hashedPassword,
		DisplayName:  input.Body.DisplayName,
		Email:        email,
		Locale:       input.Body.Locale,
		TimeFormat:   cmp.Or(input.Body.TimeFormat, types.TimeFormatAuto),
		CreatedAt:    time.Now(),
	})
	if err != nil {
		if errors.Is(err, ErrUsernameTaken) {
			return nil, huma.Error409Conflict(ErrUsernameTaken.Error())
		}
		if errors.Is(err, ErrEmailTaken) {
			return nil, huma.Error409Conflict(ErrEmailTaken.Error())
		}
		return nil, huma.Error500InternalServerError("Failed to create user")
	}
	if applyErr := h.applyDefaultRole(ctx, createdUser.ID); applyErr != nil {
		slog.WarnContext(ctx, "Failed to apply the default role to a new user", "userId", createdUser.ID, "error", applyErr)
	}

	out, err := h.userService.ToUserResponseDto(ctx, *createdUser)
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

// GetUser returns a user by ID.
func (h *UserHandler) GetUser(ctx context.Context, input *GetUserInput) (*handlerutil.Out[types.User], error) {
	userModel, err := h.userService.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, huma.Error404NotFound("User not found")
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

// UpdateUser updates a user.
func (h *UserHandler) UpdateUser(ctx context.Context, input *UpdateUserInput) (*handlerutil.Out[types.User], error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body")
	}

	current, err := h.userService.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, huma.Error404NotFound("User not found")
	}

	username, err := normalizeUsername(kit.Ternary(submitted["username"], input.Body.Username, current.Username))
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	email, err := NormalizeOptionalEmail(kit.Ternary(submitted["email"], input.Body.Email, current.Email))
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	callerPerms, err := h.checkUpdateUserPrivileges(ctx, input, current.ID)
	if err != nil {
		return nil, err
	}

	if input.Body.Password != "" {
		passwordPolicy := h.passwordPolicy(ctx)
		if validatePasswordPolicyErr := validation.ValidatePasswordPolicy(input.Body.Password, passwordPolicy); validatePasswordPolicyErr != nil {
			return nil, handlerutil.Error400BadRequestWithType(validatePasswordPolicyErr.Error(), validation.PasswordPolicyProblemType(passwordPolicy))
		}
	}

	updatedUser, err := h.userService.UpdateUser(ctx, &User{
		BaseModel:   current.BaseModel,
		Username:    username,
		DisplayName: kit.Ternary(submitted["displayName"], input.Body.DisplayName, current.DisplayName),
		Email:       email,
		Locale:      kit.Ternary(submitted["locale"], input.Body.Locale, current.Locale),
		TimeFormat:  kit.Ternary(submitted["timeFormat"], input.Body.TimeFormat, current.TimeFormat),
		FontSize:    current.FontSize,
		Preferences: current.Preferences,
	}, callerPerms)
	if err != nil {
		if errors.Is(err, ErrInsufficientPrivilege) {
			return nil, huma.Error403Forbidden(ErrInsufficientPrivilege.Error())
		}
		if errors.Is(err, ErrCannotRemoveLastAdmin) {
			return nil, huma.Error409Conflict(ErrCannotRemoveLastAdmin.Error())
		}
		if errors.Is(err, ErrUsernameTaken) {
			return nil, huma.Error409Conflict(ErrUsernameTaken.Error())
		}
		if errors.Is(err, ErrEmailTaken) {
			return nil, huma.Error409Conflict(ErrEmailTaken.Error())
		}
		if errors.Is(err, common.ErrUserNotFound) {
			return nil, huma.Error404NotFound("User not found")
		}
		return nil, huma.Error500InternalServerError("Failed to update user")
	}

	if input.Body.Password != "" {
		caller, _ := userctx.CurrentUserFromContext(ctx)
		currentSessionID, _ := middleware.GetCurrentSessionIDFromContext(ctx)
		exceptSessionID := kit.Ternary(caller != nil && caller.ID == updatedUser.ID, currentSessionID, "")
		if _, setPasswordErr := h.userService.SetPasswordAndRevokeSessionsExcept(ctx, updatedUser, input.Body.Password, exceptSessionID, callerPerms); setPasswordErr != nil {
			return nil, huma.Error500InternalServerError("Failed to update password")
		}
	}

	if h.invalidateUserTokenCache != nil {
		h.invalidateUserTokenCache(updatedUser.ID)
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

// DeleteUser deletes a user.
func (h *UserHandler) DeleteUser(ctx context.Context, input *DeleteUserInput) (*handlerutil.Out[types.MessageResponse], error) {
	// Privilege ordering: a non-admin caller may not delete a global admin
	// target. The service enforces the same check; this pre-check produces a
	// clean 403 without entering the delete path.
	callerPerms, _ := authz.PermissionsFromContext(ctx)
	caller, _ := userctx.CurrentUserFromContext(ctx)
	if callerPerms != nil && !callerPerms.IsGlobalAdmin() && caller != nil && caller.ID != input.UserID {
		targetPerms, err := h.userService.ResolveUserPermissions(ctx, input.UserID)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to resolve target permissions")
		}
		if targetPerms != nil && targetPerms.IsGlobalAdmin() {
			return nil, huma.Error403Forbidden(ErrInsufficientPrivilege.Error())
		}
	}

	if err := h.userService.DeleteUser(ctx, input.UserID, callerPerms); err != nil {
		if errors.Is(err, ErrInsufficientPrivilege) {
			return nil, huma.Error403Forbidden(ErrInsufficientPrivilege.Error())
		}
		if errors.Is(err, ErrCannotRemoveLastAdmin) {
			return nil, huma.Error409Conflict(ErrCannotRemoveLastAdmin.Error())
		}
		if errors.Is(err, common.ErrUserNotFound) {
			return nil, huma.Error404NotFound("User not found")
		}
		return nil, huma.Error500InternalServerError("Failed to delete user")
	}

	if h.invalidateUserTokenCache != nil {
		h.invalidateUserTokenCache(input.UserID)
	}

	return handlerutil.MessageOutput("User deleted successfully", ""), nil
}

// GetUserAvatar returns the custom profile picture for a user.
func (h *UserHandler) GetUserAvatar(ctx context.Context, input *GetUserAvatarInput) (*GetUserAvatarOutput, error) {
	data, mimeType, err := h.userService.GetAvatar(ctx, input.UserID)
	if err != nil {
		if errors.Is(err, common.ErrUserNotFound) {
			return nil, huma.Error404NotFound("User not found")
		}
		return nil, huma.Error500InternalServerError("failed to retrieve avatar")
	}

	if len(data) == 0 {
		return nil, huma.Error404NotFound("user has no custom avatar")
	}

	return &GetUserAvatarOutput{
		ContentType:         mimeType,
		CacheControl:        "public, max-age=3600, stale-while-revalidate=86400",
		XContentTypeOptions: "nosniff",
		Body:                data,
	}, nil
}

func NormalizeOptionalEmail(email *string) (*string, error) {
	if email == nil {
		return nil, nil
	}

	trimmedEmail := strings.TrimSpace(*email)
	if trimmedEmail == "" {
		return nil, nil
	}

	if !validation.IsValidUserEmail(trimmedEmail) {
		return nil, errors.New("must be a valid email")
	}

	return &trimmedEmail, nil
}

// checkUpdateUserPrivileges enforces that a non-admin caller may not
// modify a global admin target, nor set another user's password. The service
// re-enforces the target-admin check.
func (h *UserHandler) checkUpdateUserPrivileges(ctx context.Context, input *UpdateUserInput, targetID string) (*authz.PermissionSet, error) {
	callerPerms, _ := authz.PermissionsFromContext(ctx)
	caller, _ := userctx.CurrentUserFromContext(ctx)
	if callerPerms != nil && !callerPerms.IsGlobalAdmin() {
		if input.Body.Password != "" && caller != nil && caller.ID != targetID {
			return nil, huma.Error403Forbidden(ErrInsufficientPrivilege.Error())
		}
		targetPerms, err := h.userService.ResolveUserPermissions(ctx, targetID)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to resolve target permissions")
		}
		if targetPerms != nil && targetPerms.IsGlobalAdmin() {
			return nil, huma.Error403Forbidden(ErrInsufficientPrivilege.Error())
		}
	}
	return callerPerms, nil
}

func (h *UserHandler) passwordPolicy(ctx context.Context) string {
	if h.settingsService == nil {
		return validation.PasswordPolicyStrong
	}
	return h.settingsService.GetStringSetting(ctx, "authPasswordPolicy")
}
