package user

import (
	"context"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/validation"
	"github.com/getarcaneapp/arcane/types/v2/base"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
)

// UserHandler handles user management endpoints.
type UserHandler struct {
	userService              *UserService
	invalidateUserTokenCache func(string)
	settingsService          *settings.SettingsService
}

// ============================================================================
// Input/Output Types
// ============================================================================

type ListUsersInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type CreateUserInput struct {
	Body usertypes.CreateUser
}

type GetUserInput struct {
	UserID string `path:"userId" doc:"User ID"`
}

type UpdateUserInput struct {
	UserID string `path:"userId" doc:"User ID"`
	Body   usertypes.UpdateUser
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

// ============================================================================
// Registration
// ============================================================================

// RegisterUsers registers all user management endpoints.
func RegisterUsers(api huma.API, userService *UserService, invalidateUserTokenCache func(string), settingsService *settings.SettingsService) {
	h := &UserHandler{userService: userService, invalidateUserTokenCache: invalidateUserTokenCache, settingsService: settingsService}

	huma.Register(api, huma.Operation{
		OperationID: "listUsers",
		Method:      "GET",
		Path:        "/users",
		Summary:     "List users",
		Description: "Get a paginated list of all users",
		Tags:        []string{"Users"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermUsersList),
	}, h.ListUsers)

	huma.Register(api, huma.Operation{
		OperationID: "createUser",
		Method:      "POST",
		Path:        "/users",
		Summary:     "Create a user",
		Description: "Create a new user account",
		Tags:        []string{"Users"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermUsersCreate),
	}, h.CreateUser)

	huma.Register(api, huma.Operation{
		OperationID: "getUser",
		Method:      "GET",
		Path:        "/users/{userId}",
		Summary:     "Get a user",
		Description: "Get a user by ID",
		Tags:        []string{"Users"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermUsersRead),
	}, h.GetUser)

	huma.Register(api, huma.Operation{
		OperationID: "updateUser",
		Method:      "PUT",
		Path:        "/users/{userId}",
		Summary:     "Update a user",
		Description: "Update an existing user's information",
		Tags:        []string{"Users"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermUsersUpdate),
	}, h.UpdateUser)

	huma.Register(api, huma.Operation{
		OperationID: "deleteUser",
		Method:      "DELETE",
		Path:        "/users/{userId}",
		Summary:     "Delete a user",
		Description: "Delete a user by ID",
		Tags:        []string{"Users"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermUsersDelete),
	}, h.DeleteUser)

	// Unauthenticated by design: profile pictures are publicly visible
	// so they can be displayed without requiring a session token.
	huma.Register(api, huma.Operation{
		OperationID: "getUserAvatar",
		Method:      "GET",
		Path:        "/users/{userId}/avatar",
		Summary:     "Get user avatar",
		Description: "Get the custom profile picture for a user",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{},
	}, h.GetUserAvatar)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListUsers returns a paginated list of users.
func (h *UserHandler) ListUsers(ctx context.Context, input *ListUsersInput) (*handlerutil.Page[usertypes.User], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	users, paginationResp, err := h.userService.ListUsersPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to list users").Error())
	}

	return &handlerutil.Page[usertypes.User]{
		Body: base.Paginated[usertypes.User]{
			Success:    true,
			Data:       users,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// CreateUser creates a new usertypes.
func (h *UserHandler) CreateUser(ctx context.Context, input *CreateUserInput) (*handlerutil.Out[usertypes.User], error) {
	normalizedEmail, err := NormalizeOptionalEmail(input.Body.Email)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	input.Body.Email = normalizedEmail

	if err := validation.ValidatePasswordPolicy(input.Body.Password, h.passwordPolicyInternal(ctx)); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	hashedPassword, err := h.userService.HashPassword(input.Body.Password)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to hash password")
	}

	userModel := &common.User{
		Username:     input.Body.Username,
		PasswordHash: hashedPassword,
		DisplayName:  input.Body.DisplayName,
		Email:        input.Body.Email,
		Locale:       input.Body.Locale,
		TimeFormat:   usertypes.TimeFormatAuto,
		CreatedAt:    time.Now(),
	}
	if input.Body.TimeFormat != nil {
		userModel.TimeFormat = *input.Body.TimeFormat
	}

	createdUser, err := h.userService.CreateUser(ctx, userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to create user")
	}

	out, err := h.userService.ToUserResponseDto(ctx, *createdUser)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to map user")
	}

	return &handlerutil.Out[usertypes.User]{
		Body: base.ApiResponse[usertypes.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetUser returns a user by ID.
func (h *UserHandler) GetUser(ctx context.Context, input *GetUserInput) (*handlerutil.Out[usertypes.User], error) {
	userModel, err := h.userService.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, huma.Error404NotFound("User not found")
	}

	out, err := h.userService.ToUserResponseDto(ctx, *userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to map user")
	}

	return &handlerutil.Out[usertypes.User]{
		Body: base.ApiResponse[usertypes.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateUser updates a usertypes.
func (h *UserHandler) UpdateUser(ctx context.Context, input *UpdateUserInput) (*handlerutil.Out[usertypes.User], error) {
	userModel, err := h.userService.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, huma.Error404NotFound("User not found")
	}

	normalizedEmail, err := NormalizeOptionalEmail(input.Body.Email)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	input.Body.Email = normalizedEmail

	if input.Body.Username != nil {
		userModel.Username = *input.Body.Username
	}
	if input.Body.DisplayName != nil {
		userModel.DisplayName = input.Body.DisplayName
	}
	if input.Body.Email != nil {
		userModel.Email = input.Body.Email
	}
	if input.Body.Locale != nil {
		userModel.Locale = input.Body.Locale
	}
	if input.Body.TimeFormat != nil {
		userModel.TimeFormat = *input.Body.TimeFormat
	}

	userModel.UpdatedAt = new(time.Now())

	callerPerms, err := h.checkUpdateUserPrivilegesInternal(ctx, input, userModel.ID)
	if err != nil {
		return nil, err
	}

	if input.Body.Password != nil && *input.Body.Password != "" {
		if err := validation.ValidatePasswordPolicy(*input.Body.Password, h.passwordPolicyInternal(ctx)); err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		hashedPassword, err := h.userService.HashPassword(*input.Body.Password)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to hash password")
		}
		userModel.PasswordHash = hashedPassword
	}

	updatedUser, err := h.userService.UpdateUser(ctx, userModel, callerPerms)
	if err != nil {
		if errors.Is(err, ErrInsufficientPrivilege) {
			return nil, huma.Error403Forbidden(ErrInsufficientPrivilege.Error())
		}
		if errors.Is(err, ErrCannotRemoveLastAdmin) {
			return nil, huma.Error409Conflict(ErrCannotRemoveLastAdmin.Error())
		}
		if errors.Is(err, common.ErrUserNotFound) {
			return nil, huma.Error404NotFound("User not found")
		}
		return nil, huma.Error500InternalServerError("Failed to update user")
	}

	if h.invalidateUserTokenCache != nil {
		h.invalidateUserTokenCache(updatedUser.ID)
	}

	out, err := h.userService.ToUserResponseDto(ctx, *updatedUser)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to map user")
	}

	return &handlerutil.Out[usertypes.User]{
		Body: base.ApiResponse[usertypes.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// DeleteUser deletes a usertypes.
func (h *UserHandler) DeleteUser(ctx context.Context, input *DeleteUserInput) (*handlerutil.Out[base.MessageResponse], error) {
	// Privilege ordering: a non-admin caller may not delete a global admin
	// target. The service enforces the same check; this pre-check produces a
	// clean 403 without entering the delete path.
	callerPerms, _ := middleware.PermissionsFromContext(ctx)
	caller, _ := common.CurrentUserFromContext(ctx)
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

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "User deleted successfully",
			},
		},
	}, nil
}

// GetUserAvatar returns the custom profile picture for a usertypes.
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

// checkUpdateUserPrivilegesInternal enforces that a non-admin caller may not
// modify a global admin target, nor set another user's password. The service
// re-enforces the target-admin check.
func (h *UserHandler) checkUpdateUserPrivilegesInternal(ctx context.Context, input *UpdateUserInput, targetID string) (*authz.PermissionSet, error) {
	callerPerms, _ := middleware.PermissionsFromContext(ctx)
	caller, _ := common.CurrentUserFromContext(ctx)
	if callerPerms != nil && !callerPerms.IsGlobalAdmin() {
		if input.Body.Password != nil && *input.Body.Password != "" && caller != nil && caller.ID != targetID {
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

func (h *UserHandler) passwordPolicyInternal(ctx context.Context) string {
	if h.settingsService == nil {
		return validation.PasswordPolicyStrong
	}
	return h.settingsService.GetStringSetting(ctx, "authPasswordPolicy", validation.PasswordPolicyStrong)
}
