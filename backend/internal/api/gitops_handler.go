package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ofkm/arcane-backend/internal/common"
	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/middleware"
	"github.com/ofkm/arcane-backend/internal/models"
	"github.com/ofkm/arcane-backend/internal/services"
	"github.com/ofkm/arcane-backend/internal/utils"
	"github.com/ofkm/arcane-backend/internal/utils/git"
	"github.com/ofkm/arcane-backend/internal/utils/pagination"
)

type GitOpsRepositoryHandler struct {
	repositoryService *services.GitOpsRepositoryService
	projectService    *services.ProjectService
}

func NewGitOpsRepositoryHandler(group *gin.RouterGroup, repositoryService *services.GitOpsRepositoryService, projectService *services.ProjectService, authMiddleware *middleware.AuthMiddleware) {
	handler := &GitOpsRepositoryHandler{
		repositoryService: repositoryService,
		projectService:    projectService,
	}

	apiGroup := group.Group("/gitops")

	apiGroup.Use(authMiddleware.WithAdminNotRequired().Add())
	{
		apiGroup.GET("", handler.GetRepositories)
		apiGroup.POST("", handler.CreateRepository)
		apiGroup.POST("/sync", handler.SyncRepositories)
		apiGroup.GET("/:id", handler.GetRepository)
		apiGroup.PUT("/:id", handler.UpdateRepository)
		apiGroup.DELETE("/:id", handler.DeleteRepository)
		apiGroup.POST("/:id/test", handler.TestRepository)
		apiGroup.POST("/:id/sync-now", handler.SyncRepositoryNow)
	}
}

func (h *GitOpsRepositoryHandler) GetRepositories(c *gin.Context) {
	params := pagination.ExtractListModifiersQueryParams(c)

	repositories, paginationResp, err := h.repositoryService.GetRepositoriesPaginated(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryListError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"data":       repositories,
		"pagination": paginationResp,
	})
}

func (h *GitOpsRepositoryHandler) GetRepository(c *gin.Context) {
	id := c.Param("id")

	repository, err := h.repositoryService.GetRepositoryByID(c.Request.Context(), id)
	if err != nil {
		apiErr := models.ToAPIError(err)
		c.JSON(apiErr.HTTPStatus(), gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryRetrievalError{Err: err}).Error()},
		})
		return
	}

	out, mapErr := dto.MapOne[*models.GitOpsRepository, dto.GitOpsRepositoryDto](repository)
	if mapErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryMappingError{Err: mapErr}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}

func (h *GitOpsRepositoryHandler) CreateRepository(c *gin.Context) {
	var req models.CreateGitOpsRepositoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	repository, err := h.repositoryService.CreateRepository(c.Request.Context(), req)
	if err != nil {
		apiErr := models.ToAPIError(err)
		c.JSON(apiErr.HTTPStatus(), gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryCreationError{Err: err}).Error()},
		})
		return
	}

	out, mapErr := dto.MapOne[*models.GitOpsRepository, dto.GitOpsRepositoryDto](repository)
	if mapErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryMappingError{Err: mapErr}).Error()},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    out,
	})
}

func (h *GitOpsRepositoryHandler) UpdateRepository(c *gin.Context) {
	id := c.Param("id")

	var req models.UpdateGitOpsRepositoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	repository, err := h.repositoryService.UpdateRepository(c.Request.Context(), id, req)
	if err != nil {
		apiErr := models.ToAPIError(err)
		c.JSON(apiErr.HTTPStatus(), gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryUpdateError{Err: err}).Error()},
		})
		return
	}

	out, mapErr := dto.MapOne[*models.GitOpsRepository, dto.GitOpsRepositoryDto](repository)
	if mapErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryMappingError{Err: mapErr}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}

func (h *GitOpsRepositoryHandler) DeleteRepository(c *gin.Context) {
	id := c.Param("id")

	if err := h.repositoryService.DeleteRepository(c.Request.Context(), id); err != nil {
		apiErr := models.ToAPIError(err)
		c.JSON(apiErr.HTTPStatus(), gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryDeletionError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "GitOps repository deleted successfully"},
	})
}

func (h *GitOpsRepositoryHandler) TestRepository(c *gin.Context) {
	id := c.Param("id")

	repository, err := h.repositoryService.GetRepositoryByID(c.Request.Context(), id)
	if err != nil {
		apiErr := models.ToAPIError(err)
		c.JSON(apiErr.HTTPStatus(), gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryRetrievalError{Err: err}).Error()},
		})
		return
	}

	var decryptedToken string
	if repository.Token != "" {
		decrypted, err := utils.Decrypt(repository.Token)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"data":    gin.H{"error": (&common.TokenDecryptionError{Err: err}).Error()},
			})
			return
		}
		decryptedToken = decrypted
	}

	testResult, err := h.performRepositoryTest(c.Request.Context(), repository, decryptedToken)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"data":    gin.H{"message": fmt.Sprintf("Connection test failed: %s", err.Error())},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    testResult,
	})
}

func (h *GitOpsRepositoryHandler) SyncRepositoryNow(c *gin.Context) {
	id := c.Param("id")

	repository, err := h.repositoryService.GetRepositoryByID(c.Request.Context(), id)
	if err != nil {
		apiErr := models.ToAPIError(err)
		c.JSON(apiErr.HTTPStatus(), gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryRetrievalError{Err: err}).Error()},
		})
		return
	}

	var decryptedToken string
	if repository.Token != "" {
		decrypted, err := utils.Decrypt(repository.Token)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"data":    gin.H{"error": (&common.TokenDecryptionError{Err: err}).Error()},
			})
			return
		}
		decryptedToken = decrypted
	}

	syncResult, err := h.performRepositorySync(c.Request.Context(), repository, decryptedToken)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"data":    gin.H{"message": fmt.Sprintf("Sync failed: %s", err.Error())},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    syncResult,
	})
}

func (h *GitOpsRepositoryHandler) SyncRepositories(c *gin.Context) {
	var req dto.SyncGitOpsRepositoriesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	if err := h.repositoryService.SyncRepositories(c.Request.Context(), req.Repositories); err != nil {
		apiErr := models.ToAPIError(err)
		c.JSON(apiErr.HTTPStatus(), gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistrySyncError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "GitOps repositories synced successfully"},
	})
}

func (h *GitOpsRepositoryHandler) performRepositoryTest(ctx context.Context, repository *models.GitOpsRepository, decryptedToken string) (map[string]interface{}, error) {
	// Test git repository connection
	err := git.TestConnection(ctx, repository.URL, repository.Branch, repository.Username, decryptedToken)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"message": "Git repository connection successful",
	}, nil
}

func (h *GitOpsRepositoryHandler) performRepositorySync(ctx context.Context, repository *models.GitOpsRepository, decryptedToken string) (map[string]interface{}, error) {
	results, err := h.repositoryService.SyncAllEnabledRepositories(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to sync repositories: %w", err)
	}

	var targetResult *services.GitOpsSyncResult
	for i := range results {
		if results[i].RepositoryID == repository.ID {
			targetResult = &results[i]
			break
		}
	}

	if targetResult == nil {
		return nil, fmt.Errorf("repository not found in sync results - ensure repository is enabled")
	}

	if !targetResult.Success {
		return nil, fmt.Errorf("%s", targetResult.Error)
	}

	projectName := h.deriveProjectName(repository)
	
	response := map[string]interface{}{
		"message":         "Repository synced successfully and project created/updated",
		"gitops_path":     targetResult.ProjectPath,
		"project_name":    projectName,
	}

	return response, nil
}

func (h *GitOpsRepositoryHandler) deriveProjectName(repo *models.GitOpsRepository) string {
	if repo.ProjectName != nil && strings.TrimSpace(*repo.ProjectName) != "" {
		return strings.TrimSpace(*repo.ProjectName)
	}

	repoURL := strings.TrimSuffix(repo.URL, ".git")
	parts := strings.Split(repoURL, "/")
	if len(parts) > 0 {
		repoName := parts[len(parts)-1]
		return fmt.Sprintf("gitops_%s", repoName)
	}
	return "gitops-project"
}
