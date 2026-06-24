package handlers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/validation"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/user"
)

// userHandler handles user management endpoints.
type userHandler struct {
	userService *services.UserService
	authService *services.AuthService
}

// ============================================================================
// Input/Output Types
// ============================================================================

// userPaginatedResponse is the paginated response for users.
type userPaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []user.User             `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type listUsersInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type listUsersOutput struct {
	Body userPaginatedResponse
}

type createUserInput struct {
	Body user.CreateUser
}

type createUserOutput struct {
	Body base.ApiResponse[user.User]
}

type getUserInput struct {
	UserID string `path:"userId" doc:"User ID"`
}

type getUserOutput struct {
	Body base.ApiResponse[user.User]
}

type updateUserInput struct {
	UserID string `path:"userId" doc:"User ID"`
	Body   user.UpdateUser
}

type updateUserOutput struct {
	Body base.ApiResponse[user.User]
}

type deleteUserInput struct {
	UserID string `path:"userId" doc:"User ID"`
}

type deleteUserOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type getUserAvatarInput struct {
	UserID string `path:"userId" doc:"User ID"`
}

type getUserAvatarOutput struct {
	ContentType         string `header:"Content-Type"`
	CacheControl        string `header:"Cache-Control"`
	XContentTypeOptions string `header:"X-Content-Type-Options"`
	Body                []byte
}

// ============================================================================
// Registration
// ============================================================================

// RegisterUsers registers all user management endpoints.
func RegisterUsers(api huma.API, userService *services.UserService, authService *services.AuthService) {
	h := &userHandler{userService: userService, authService: authService}

	huma.Register(api, huma.Operation{
		OperationID: "listUsers",
		Method:      "GET",
		Path:        "/users",
		Summary:     "List users",
		Description: "Get a paginated list of all users",
		Tags:        []string{"Users"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermUsersList),
	}, h.listUsersInternal)

	huma.Register(api, huma.Operation{
		OperationID: "createUser",
		Method:      "POST",
		Path:        "/users",
		Summary:     "Create a user",
		Description: "Create a new user account",
		Tags:        []string{"Users"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermUsersCreate),
	}, h.createUserInternal)

	huma.Register(api, huma.Operation{
		OperationID: "getUser",
		Method:      "GET",
		Path:        "/users/{userId}",
		Summary:     "Get a user",
		Description: "Get a user by ID",
		Tags:        []string{"Users"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermUsersRead),
	}, h.getUserInternal)

	huma.Register(api, huma.Operation{
		OperationID: "updateUser",
		Method:      "PUT",
		Path:        "/users/{userId}",
		Summary:     "Update a user",
		Description: "Update an existing user's information",
		Tags:        []string{"Users"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermUsersUpdate),
	}, h.updateUserInternal)

	huma.Register(api, huma.Operation{
		OperationID: "deleteUser",
		Method:      "DELETE",
		Path:        "/users/{userId}",
		Summary:     "Delete a user",
		Description: "Delete a user by ID",
		Tags:        []string{"Users"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermUsersDelete),
	}, h.deleteUserInternal)

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
	}, h.getUserAvatarInternal)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListUsers returns a paginated list of users.
func (h *userHandler) listUsersInternal(ctx context.Context, input *listUsersInput) (*listUsersOutput, error) {
	if h.userService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	users, paginationResp, err := h.userService.ListUsersPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserListError{Err: err}).Error())
	}

	return &listUsersOutput{
		Body: userPaginatedResponse{
			Success:    true,
			Data:       users,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

// CreateUser creates a new user.
func (h *userHandler) createUserInternal(ctx context.Context, input *createUserInput) (*createUserOutput, error) {
	if h.userService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	normalizedEmail, err := normalizeOptionalEmailInternal(input.Body.Email)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	input.Body.Email = normalizedEmail

	hashedPassword, err := h.userService.HashPassword(input.Body.Password)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.PasswordHashError{Err: err}).Error())
	}

	userModel := &models.User{
		Username:     input.Body.Username,
		PasswordHash: hashedPassword,
		DisplayName:  input.Body.DisplayName,
		Email:        input.Body.Email,
		Locale:       input.Body.Locale,
		BaseModel: models.BaseModel{
			CreatedAt: time.Now(),
		},
	}

	createdUser, err := h.userService.CreateUser(ctx, userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserCreationError{Err: err}).Error())
	}

	out, err := h.userService.ToUserResponseDto(ctx, *createdUser)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserMappingError{Err: err}).Error())
	}

	return &createUserOutput{
		Body: base.ApiResponse[user.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetUser returns a user by ID.
func (h *userHandler) getUserInternal(ctx context.Context, input *getUserInput) (*getUserOutput, error) {
	if h.userService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	userModel, err := h.userService.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, huma.Error404NotFound((&common.UserNotFoundError{}).Error())
	}

	out, err := h.userService.ToUserResponseDto(ctx, *userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserMappingError{Err: err}).Error())
	}

	return &getUserOutput{
		Body: base.ApiResponse[user.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateUser updates a user.
func (h *userHandler) updateUserInternal(ctx context.Context, input *updateUserInput) (*updateUserOutput, error) {
	if h.userService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	userModel, err := h.userService.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, huma.Error404NotFound((&common.UserNotFoundError{}).Error())
	}

	normalizedEmail, err := normalizeOptionalEmailInternal(input.Body.Email)
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

	if input.Body.Password != nil && *input.Body.Password != "" {
		hashedPassword, err := h.userService.HashPassword(*input.Body.Password)
		if err != nil {
			return nil, huma.Error500InternalServerError((&common.PasswordHashError{Err: err}).Error())
		}
		userModel.PasswordHash = hashedPassword
	}

	userModel.UpdatedAt = new(time.Now())

	updatedUser, err := h.userService.UpdateUser(ctx, userModel)
	if err != nil {
		if errors.Is(err, services.ErrCannotRemoveLastAdmin) {
			return nil, huma.Error409Conflict(services.ErrCannotRemoveLastAdmin.Error())
		}
		if errors.Is(err, services.ErrUserNotFound) {
			return nil, huma.Error404NotFound((&common.UserNotFoundError{}).Error())
		}
		return nil, huma.Error500InternalServerError((&common.UserUpdateError{Err: err}).Error())
	}

	if h.authService != nil {
		h.authService.InvalidateUserTokenCache(updatedUser.ID)
	}

	out, err := h.userService.ToUserResponseDto(ctx, *updatedUser)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserMappingError{Err: err}).Error())
	}

	return &updateUserOutput{
		Body: base.ApiResponse[user.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// DeleteUser deletes a user.
func (h *userHandler) deleteUserInternal(ctx context.Context, input *deleteUserInput) (*deleteUserOutput, error) {
	if h.userService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.userService.DeleteUser(ctx, input.UserID); err != nil {
		if errors.Is(err, services.ErrCannotRemoveLastAdmin) {
			return nil, huma.Error409Conflict(services.ErrCannotRemoveLastAdmin.Error())
		}
		if errors.Is(err, services.ErrUserNotFound) {
			return nil, huma.Error404NotFound((&common.UserNotFoundError{}).Error())
		}
		return nil, huma.Error500InternalServerError((&common.UserDeletionError{Err: err}).Error())
	}

	if h.authService != nil {
		h.authService.InvalidateUserTokenCache(input.UserID)
	}

	return &deleteUserOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "User deleted successfully",
			},
		},
	}, nil
}

// getUserAvatarInternal returns the custom profile picture for a user.
func (h *userHandler) getUserAvatarInternal(ctx context.Context, input *getUserAvatarInput) (*getUserAvatarOutput, error) {
	if h.userService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	data, mimeType, err := h.userService.GetAvatar(ctx, input.UserID)
	if err != nil {
		if errors.Is(err, services.ErrUserNotFound) {
			return nil, huma.Error404NotFound((&common.UserNotFoundError{}).Error())
		}
		return nil, huma.Error500InternalServerError("failed to retrieve avatar")
	}

	if len(data) == 0 {
		return nil, huma.Error404NotFound("user has no custom avatar")
	}

	return &getUserAvatarOutput{
		ContentType:         mimeType,
		CacheControl:        "public, max-age=3600, stale-while-revalidate=86400",
		XContentTypeOptions: "nosniff",
		Body:                data,
	}, nil
}

func normalizeOptionalEmailInternal(email *string) (*string, error) {
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
