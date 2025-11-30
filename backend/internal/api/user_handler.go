package api

import (
	"net/http"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/dto"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	userService *services.UserService
}

func NewUserHandler(group *gin.RouterGroup, userService *services.UserService, authMiddleware *middleware.AuthMiddleware) {

	handler := &UserHandler{userService: userService}

	apiGroup := group.Group("/users")
	apiGroup.Use(authMiddleware.WithAdminRequired().Add())
	{
		apiGroup.GET("", handler.ListUsers)
		apiGroup.POST("", handler.CreateUser)
		apiGroup.GET("/:id", handler.GetUser)
		apiGroup.PUT("/:id", handler.UpdateUser)
		apiGroup.DELETE("/:id", handler.DeleteUser)
	}
}

// ListUsers godoc
// @Summary List users
// @Description Get a paginated list of all users
// @Tags Users
// @Param pagination[page] query int false "Page number for pagination" default(1)
// @Param pagination[limit] query int false "Number of items per page" default(20)
// @Param sort[column] query string false "Column to sort by"
// @Param sort[direction] query string false "Sort direction (asc or desc)" default("asc")
// @Success 200 {object} dto.Paginated[dto.UserResponseDto]
// @Router /api/users [get]
func (h *UserHandler) ListUsers(c *gin.Context) {
	params := pagination.ExtractListModifiersQueryParams(c)

	users, paginationResp, err := h.userService.ListUsersPaginated(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.UserListError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"data":       users,
		"pagination": paginationResp,
	})
}

// CreateUser godoc
// @Summary Create a user
// @Description Create a new user account
// @Tags Users
// @Accept json
// @Produce json
// @Param user body dto.CreateUserDto true "User creation data"
// @Success 201 {object} dto.UserResponseDto
// @Router /api/users [post]
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req dto.CreateUserDto
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	hashedPassword, err := h.userService.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.PasswordHashError{Err: err}).Error()},
		})
		return
	}

	user := &models.User{
		Username:     req.Username,
		PasswordHash: hashedPassword,
		DisplayName:  req.DisplayName,
		Email:        req.Email,
		Roles:        req.Roles,
		Locale:       req.Locale,
		BaseModel: models.BaseModel{
			CreatedAt: time.Now(),
		},
	}

	if user.Roles == nil {
		user.Roles = []string{"user"}
	}

	createdUser, err := h.userService.CreateUser(c.Request.Context(), user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.UserCreationError{Err: err}).Error()},
		})
		return
	}

	out, err := dto.MapOne[*models.User, dto.UserResponseDto](createdUser)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.UserMappingError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    out,
	})
}

// GetUser godoc
// @Summary Get a user
// @Description Get a user by ID
// @Tags Users
// @Param id path string true "User ID"
// @Success 200 {object} dto.UserResponseDto
// @Router /api/users/{id} [get]
func (h *UserHandler) GetUser(c *gin.Context) {
	userID := c.Param("id")

	user, err := h.userService.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.UserNotFoundError{}).Error()},
		})
		return
	}

	out, err := dto.MapOne[*models.User, dto.UserResponseDto](user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.UserMappingError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}

// UpdateUser godoc
// @Summary Update a user
// @Description Update an existing user's information
// @Tags Users
// @Accept json
// @Produce json
// @Param id path string true "User ID"
// @Param user body dto.UpdateUserDto true "User update data"
// @Success 200 {object} dto.UserResponseDto
// @Router /api/users/{id} [put]
func (h *UserHandler) UpdateUser(c *gin.Context) {
	userID := c.Param("id")

	var req dto.UpdateUserDto
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	user, err := h.userService.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.UserNotFoundError{}).Error()},
		})
		return
	}

	if req.DisplayName != nil {
		user.DisplayName = req.DisplayName
	}
	if req.Email != nil {
		user.Email = req.Email
	}
	if req.Roles != nil {
		user.Roles = req.Roles
	}
	if req.Locale != nil {
		user.Locale = req.Locale
	}

	if req.Password != nil && *req.Password != "" {
		hashedPassword, err := h.userService.HashPassword(*req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"data":    gin.H{"error": (&common.PasswordHashError{Err: err}).Error()},
			})
			return
		}
		user.PasswordHash = hashedPassword
	}

	now := time.Now()
	user.UpdatedAt = &now

	updatedUser, err := h.userService.UpdateUser(c.Request.Context(), user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.UserUpdateError{Err: err}).Error()},
		})
		return
	}

	out, err := dto.MapOne[*models.User, dto.UserResponseDto](updatedUser)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.UserMappingError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}

// DeleteUser godoc
// @Summary Delete a user
// @Description Delete a user by ID
// @Tags Users
// @Security BearerAuth
// @Security ApiKeyAuth
// @Param id path string true "User ID"
// @Success 200 {object} dto.ApiResponse[dto.MessageResponseDto]
// @Failure 500 {object} dto.ApiResponse[dto.ErrorResponse]
// @Router /api/users/{id} [delete]
func (h *UserHandler) DeleteUser(c *gin.Context) {
	userID := c.Param("id")

	err := h.userService.DeleteUser(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.UserDeletionError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "User deleted successfully"},
	})
}
