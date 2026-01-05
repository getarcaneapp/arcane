package handlers

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/user"
)

// UserHandler handles user management endpoints.
type UserHandler struct {
	userService *services.UserService
}

// ============================================================================
// Input/Output Types
// ============================================================================

// UserPaginatedResponse is the paginated response for users.
type UserPaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []user.User             `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type ListUsersInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type ListUsersOutput struct {
	Body UserPaginatedResponse
}

type CreateUserInput struct {
	Body user.CreateUser
}

type CreateUserOutput struct {
	Body base.ApiResponse[user.User]
}

type GetUserInput struct {
	UserID string `path:"userId" doc:"User ID"`
}

type GetUserOutput struct {
	Body base.ApiResponse[user.User]
}

type UpdateUserInput struct {
	UserID string `path:"userId" doc:"User ID"`
	Body   user.UpdateUser
}

type UpdateUserOutput struct {
	Body base.ApiResponse[user.User]
}

type DeleteUserInput struct {
	UserID string `path:"userId" doc:"User ID"`
}

type DeleteUserOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterUsers registers all user management endpoints.
func RegisterUsers(api huma.API, userService *services.UserService) {
	h := &UserHandler{userService: userService}

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
	}, h.ListUsers)

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
	}, h.CreateUser)

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
	}, h.GetUser)

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
	}, h.UpdateUser)

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
	}, h.DeleteUser)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListUsers returns a paginated list of users.
func (h *UserHandler) ListUsers(ctx context.Context, input *ListUsersInput) (*ListUsersOutput, error) {
	if h.userService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	params := buildPaginationParams(0, input.Start, input.Limit, input.Sort, input.Order, input.Search)

	users, paginationResp, err := h.userService.ListUsersPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserListError{Err: err}).Error())
	}

	return &ListUsersOutput{
		Body: UserPaginatedResponse{
			Success: true,
			Data:    users,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}

// CreateUser creates a new user.
func (h *UserHandler) CreateUser(ctx context.Context, input *CreateUserInput) (*CreateUserOutput, error) {
	if h.userService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	hashedPassword, err := h.userService.HashPassword(input.Body.Password)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.PasswordHashError{Err: err}).Error())
	}

	userModel := &models.User{
		Username:     input.Body.Username,
		PasswordHash: hashedPassword,
		DisplayName:  input.Body.DisplayName,
		Email:        input.Body.Email,
		Roles:        input.Body.Roles,
		Locale:       input.Body.Locale,
		BaseModel: models.BaseModel{
			CreatedAt: time.Now(),
		},
	}

	if userModel.Roles == nil {
		userModel.Roles = []string{"user"}
	}

	createdUser, err := h.userService.CreateUser(ctx, userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserCreationError{Err: err}).Error())
	}

	out, err := mapper.MapOne[*models.User, user.User](createdUser)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserMappingError{Err: err}).Error())
	}

	return &CreateUserOutput{
		Body: base.ApiResponse[user.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetUser returns a user by ID.
func (h *UserHandler) GetUser(ctx context.Context, input *GetUserInput) (*GetUserOutput, error) {
	if h.userService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	userModel, err := h.userService.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, huma.Error404NotFound((&common.UserNotFoundError{}).Error())
	}

	out, err := mapper.MapOne[*models.User, user.User](userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserMappingError{Err: err}).Error())
	}

	return &GetUserOutput{
		Body: base.ApiResponse[user.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateUser updates a user.
func (h *UserHandler) UpdateUser(ctx context.Context, input *UpdateUserInput) (*UpdateUserOutput, error) {
	if h.userService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	userModel, err := h.userService.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, huma.Error404NotFound((&common.UserNotFoundError{}).Error())
	}

	if input.Body.DisplayName != nil {
		userModel.DisplayName = input.Body.DisplayName
	}
	if input.Body.Email != nil {
		userModel.Email = input.Body.Email
	}
	if input.Body.Roles != nil {
		userModel.Roles = input.Body.Roles
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

	now := time.Now()
	userModel.UpdatedAt = &now

	updatedUser, err := h.userService.UpdateUser(ctx, userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserUpdateError{Err: err}).Error())
	}

	out, err := mapper.MapOne[*models.User, user.User](updatedUser)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UserMappingError{Err: err}).Error())
	}

	return &UpdateUserOutput{
		Body: base.ApiResponse[user.User]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// DeleteUser deletes a user.
func (h *UserHandler) DeleteUser(ctx context.Context, input *DeleteUserInput) (*DeleteUserOutput, error) {
	if h.userService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.userService.DeleteUser(ctx, input.UserID); err != nil {
		return nil, huma.Error500InternalServerError((&common.UserDeletionError{Err: err}).Error())
	}

	return &DeleteUserOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "User deleted successfully",
			},
		},
	}, nil
}
