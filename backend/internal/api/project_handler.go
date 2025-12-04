package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
	httputil "github.com/getarcaneapp/arcane/backend/internal/utils/http"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	ws "github.com/getarcaneapp/arcane/backend/internal/utils/ws"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.getarcane.app/types/project"
)

type ProjectHandler struct {
	projectService *services.ProjectService
	wsUpgrader     websocket.Upgrader
}

type projectLogStream struct {
	hub    *ws.Hub
	cancel context.CancelFunc
	format string
	seq    atomic.Uint64
}

func NewProjectHandler(group *gin.RouterGroup, projectService *services.ProjectService, authMiddleware *middleware.AuthMiddleware, cfg *config.Config) {

	handler := &ProjectHandler{
		projectService: projectService,
		wsUpgrader: websocket.Upgrader{
			CheckOrigin:       httputil.ValidateWebSocketOrigin(cfg.AppUrl),
			ReadBufferSize:    32 * 1024,
			WriteBufferSize:   32 * 1024,
			EnableCompression: true,
		},
	}

	apiGroup := group.Group("/environments/:id/projects")
	apiGroup.Use(authMiddleware.WithAdminNotRequired().Add())
	{

		apiGroup.GET("", handler.ListProjects)
		apiGroup.GET("/counts", handler.GetProjectStatusCounts)
		apiGroup.POST("/:projectId/up", handler.DeployProject)
		apiGroup.POST("/:projectId/down", handler.DownProject)
		apiGroup.POST("", handler.CreateProject)
		apiGroup.GET("/:projectId", handler.GetProject)
		apiGroup.POST("/:projectId/pull", handler.PullProjectImages)
		apiGroup.POST("/:projectId/redeploy", handler.RedeployProject)
		apiGroup.DELETE("/:projectId/destroy", handler.DestroyProject)
		apiGroup.PUT("/:projectId", handler.UpdateProject)
		apiGroup.PUT("/:projectId/includes", handler.UpdateProjectInclude)
		apiGroup.POST("/:projectId/restart", handler.RestartProject)
		apiGroup.GET("/:projectId/logs/ws", handler.GetProjectLogsWS)

	}
}

// ListProjects godoc
//
//	@Summary		List projects
//	@Description	Get a paginated list of Docker Compose projects
//	@Tags			Projects
//	@Param			id					path		string	true	"Environment ID"
//	@Param			pagination[page]	query		int		false	"Page number for pagination"	default(1)
//	@Param			pagination[limit]	query		int		false	"Number of items per page"		default(20)
//	@Param			sort[column]		query		string	false	"Column to sort by"
//	@Param			sort[direction]		query		string	false	"Sort direction (asc or desc)"	default("asc")
//	@Success		200					{object}	base.Paginated[project.Details]
//	@Router			/api/environments/{id}/projects [get]
func (h *ProjectHandler) ListProjects(c *gin.Context) {
	params := pagination.ExtractListModifiersQueryParams(c)

	projectsResponse, paginationResp, err := h.projectService.ListProjects(c.Request.Context(), params)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			c.JSON(http.StatusRequestTimeout, gin.H{
				"success": false,
				"error":   "Request was canceled",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.ProjectListError{Err: err}).Error(),
		})
		return
	}
	if projectsResponse == nil {
		projectsResponse = []project.Details{}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"data":       projectsResponse,
		"pagination": paginationResp,
	})
}

// DeployProject godoc
//
//	@Summary		Deploy a project
//	@Description	Deploy a Docker Compose project (docker-compose up)
//	@Tags			Projects
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id			path		string	true	"Environment ID"
//	@Param			projectId	path		string	true	"Project ID"
//	@Success		200			{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		400			{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/environments/{id}/projects/{projectId}/up [post]
func (h *ProjectHandler) DeployProject(c *gin.Context) {
	projectID := c.Param("projectId")

	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   (&common.ProjectIDRequiredError{}).Error(),
		})
		return
	}

	user, _ := middleware.GetCurrentUser(c)
	if err := h.projectService.DeployProject(c.Request.Context(), projectID, *user); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   (&common.ProjectDeploymentError{Err: err}).Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "Project deployed successfully"},
	})
}

// DownProject godoc
//
//	@Summary		Bring down a project
//	@Description	Bring down a Docker Compose project (docker-compose down)
//	@Tags			Projects
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id			path		string	true	"Environment ID"
//	@Param			projectId	path		string	true	"Project ID"
//	@Success		200			{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		500			{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/environments/{id}/projects/{projectId}/down [post]
func (h *ProjectHandler) DownProject(c *gin.Context) {
	projectID := c.Param("projectId")

	user, _ := middleware.GetCurrentUser(c)
	if err := h.projectService.DownProject(c.Request.Context(), projectID, *user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.ProjectDownError{Err: err}).Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "Project brought down successfully"},
	})
}

// CreateProject godoc
//
//	@Summary		Create a project
//	@Description	Create a new Docker Compose project
//	@Tags			Projects
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"Environment ID"
//	@Param			project	body		project.Create		true	"Project creation data"
//	@Success		201		{object}	base.ApiResponse[project.CreateReponse]
//	@Router			/api/environments/{id}/projects [post]
func (h *ProjectHandler) CreateProject(c *gin.Context) {
	var req project.Create
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.InvalidRequestFormatError{Err: err}).Error()})
		return
	}

	user, _ := middleware.GetCurrentUser(c)
	proj, err := h.projectService.CreateProject(c.Request.Context(), req.Name, req.ComposeContent, req.EnvContent, *user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": (&common.ProjectCreationError{Err: err}).Error()})
		return
	}

	var response project.CreateReponse
	if err := mapper.MapStruct(proj, &response); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "failed to map response"})
		return
	}
	response.Status = string(proj.Status)
	response.StatusReason = proj.StatusReason
	response.CreatedAt = proj.CreatedAt.Format(time.RFC3339)
	response.UpdatedAt = proj.UpdatedAt.Format(time.RFC3339)
	response.DirName = utils.DerefString(proj.DirName)

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    response,
	})
}

// GetProject godoc
//
//	@Summary		Get a project
//	@Description	Get a Docker Compose project by ID
//	@Tags			Projects
//	@Param			id			path		string	true	"Environment ID"
//	@Param			projectId	path		string	true	"Project ID"
//	@Success		200			{object}	base.ApiResponse[project.Details]
//	@Router			/api/environments/{id}/projects/{projectId} [get]
func (h *ProjectHandler) GetProject(c *gin.Context) {
	projectID := c.Param("projectId")
	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.ProjectIDRequiredError{}).Error()})
		return
	}

	details, err := h.projectService.GetProjectDetails(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": (&common.ProjectDetailsError{Err: err}).Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    details,
	})
}

// RedeployProject godoc
//
//	@Summary		Redeploy a project
//	@Description	Redeploy a Docker Compose project (down + up)
//	@Tags			Projects
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id			path		string	true	"Environment ID"
//	@Param			projectId	path		string	true	"Project ID"
//	@Success		200			{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		400			{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/environments/{id}/projects/{projectId}/redeploy [post]
func (h *ProjectHandler) RedeployProject(c *gin.Context) {
	projectID := c.Param("projectId")

	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   (&common.ProjectIDRequiredError{}).Error(),
		})
		return
	}

	user, _ := middleware.GetCurrentUser(c)
	if err := h.projectService.RedeployProject(c.Request.Context(), projectID, *user); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   (&common.ProjectRedeploymentError{Err: err}).Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "Project redeployed successfully"},
	})
}

// DestroyProject godoc
//
//	@Summary		Destroy a project
//	@Description	Destroy a Docker Compose project and optionally remove files/volumes
//	@Tags			Projects
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Accept			json
//	@Produce		json
//	@Param			id			path		string				true	"Environment ID"
//	@Param			projectId	path		string				true	"Project ID"
//	@Param			request		body		project.Destroy		false	"Destroy options"
//	@Success		200			{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		400			{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		500			{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/environments/{id}/projects/{projectId}/destroy [delete]
func (h *ProjectHandler) DestroyProject(c *gin.Context) {
	projectID := c.Param("projectId")

	var req project.Destroy
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.InvalidRequestFormatError{Err: err}).Error()})
			return
		}
	}

	user, _ := middleware.GetCurrentUser(c)
	if err := h.projectService.DestroyProject(c.Request.Context(), projectID, req.RemoveFiles, req.RemoveVolumes, *user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.ProjectDestroyError{Err: err}).Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "Project destroyed successfully"},
	})
}

// PullProjectImages godoc
//
//	@Summary		Pull project images
//	@Description	Pull all images for a Docker Compose project
//	@Tags			Projects
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Accept			json
//	@Produce		application/x-json-stream
//	@Param			id			path		string						true	"Environment ID"
//	@Param			projectId	path		string						true	"Project ID"
//	@Param			request		body		project.ImagePullRequest	false	"Pull options with optional credentials"
//	@Success		200			{string}	string						"Streaming JSON response"
//	@Failure		400			{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/environments/{id}/projects/{projectId}/pull [post]
func (h *ProjectHandler) PullProjectImages(c *gin.Context) {
	projectID := c.Param("projectId")
	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.ProjectIDRequiredError{}).Error()})
		return
	}

	var req project.ImagePullRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.InvalidRequestFormatError{Err: err}).Error()})
			return
		}
	}

	c.Writer.Header().Set("Content-Type", "application/x-json-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	_, _ = fmt.Fprintln(c.Writer, `{"status":"starting project image pull"}`)

	if err := h.projectService.PullProjectImages(c.Request.Context(), projectID, c.Writer, req.Credentials); err != nil {
		_, _ = fmt.Fprintf(c.Writer, `{"error":%q}`+"\n", err.Error())
		return
	}

	_, _ = fmt.Fprintln(c.Writer, `{"status":"complete"}`)
}

// UpdateProject godoc
//
//	@Summary		Update a project
//	@Description	Update a Docker Compose project configuration
//	@Tags			Projects
//	@Accept			json
//	@Produce		json
//	@Param			id			path		string				true	"Environment ID"
//	@Param			projectId	path		string				true	"Project ID"
//	@Param			project		body		project.Update		true	"Project update data"
//	@Success		200			{object}	base.ApiResponse[project.Details]
//	@Router			/api/environments/{id}/projects/{projectId} [put]
func (h *ProjectHandler) UpdateProject(c *gin.Context) {
	projectID := c.Param("projectId")
	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.ProjectIDRequiredError{}).Error()})
		return
	}

	var req project.Update
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.InvalidRequestFormatError{Err: err}).Error()})
		return
	}

	if _, err := h.projectService.UpdateProject(c.Request.Context(), projectID, req.Name, req.ComposeContent, req.EnvContent); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.ProjectUpdateError{Err: err}).Error()})
		return
	}

	details, err := h.projectService.GetProjectDetails(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": (&common.ProjectDetailsError{Err: err}).Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    details,
	})
}

// UpdateProjectInclude godoc
//
//	@Summary		Update project include file
//	@Description	Update an include file within a Docker Compose project
//	@Tags			Projects
//	@Accept			json
//	@Produce		json
//	@Param			id			path		string					true	"Environment ID"
//	@Param			projectId	path		string					true	"Project ID"
//	@Param			include		body		project.UpdateIncludeFile	true	"Include file update data"
//	@Success		200			{object}	base.ApiResponse[project.Details]
//	@Router			/api/environments/{id}/projects/{projectId}/includes [put]
func (h *ProjectHandler) UpdateProjectInclude(c *gin.Context) {
	projectID := c.Param("projectId")
	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.ProjectIDRequiredError{}).Error()})
		return
	}

	var req project.UpdateIncludeFile
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.InvalidRequestFormatError{Err: err}).Error()})
		return
	}

	if err := h.projectService.UpdateProjectIncludeFile(c.Request.Context(), projectID, req.RelativePath, req.Content); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.ProjectUpdateError{Err: err}).Error()})
		return
	}

	details, err := h.projectService.GetProjectDetails(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": (&common.ProjectDetailsError{Err: err}).Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    details,
	})
}

// RestartProject godoc
//
//	@Summary		Restart a project
//	@Description	Restart all containers in a Docker Compose project
//	@Tags			Projects
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id			path		string	true	"Environment ID"
//	@Param			projectId	path		string	true	"Project ID"
//	@Success		200			{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		400			{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/environments/{id}/projects/{projectId}/restart [post]
func (h *ProjectHandler) RestartProject(c *gin.Context) {
	projectID := c.Param("projectId")
	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.ProjectIDRequiredError{}).Error()})
		return
	}

	user, _ := middleware.GetCurrentUser(c)
	if err := h.projectService.RestartProject(c.Request.Context(), projectID, *user); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.ProjectRestartError{Err: err}).Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "Project restarted successfully"},
	})
}

func (h *ProjectHandler) getOrStartProjectLogHub(projectID, format string, batched bool, follow bool, tail, since string, timestamps bool) *ws.Hub {
	// Create a new hub for each connection to ensure every client gets historical logs
	ls := &projectLogStream{
		hub:    ws.NewHub(1024),
		format: format,
	}

	ctx, cancel := context.WithCancel(context.Background())
	ls.cancel = cancel

	ls.hub.SetOnEmpty(func() {
		slog.Debug("client disconnected, cleaning up project log hub", "projectID", projectID, "format", format, "tail", tail)
		cancel()
	})

	go ls.hub.Run(ctx)

	lines := make(chan string, 256)
	go func() {
		defer close(lines)
		_ = h.projectService.StreamProjectLogs(ctx, projectID, lines, follow, tail, since, timestamps)
	}()

	if format == "json" {
		msgs := make(chan ws.LogMessage, 256)
		go func() {
			defer close(msgs)
			for line := range lines {
				level, service, msg, ts := ws.NormalizeProjectLine(line)
				seq := ls.seq.Add(1)
				timestamp := ts
				if timestamp == "" {
					timestamp = ws.NowRFC3339()
				}
				msgs <- ws.LogMessage{
					Seq:       seq,
					Level:     level,
					Message:   msg,
					Service:   service,
					Timestamp: timestamp,
				}
			}
		}()
		if batched {
			go ws.ForwardLogJSONBatched(ctx, ls.hub, msgs, 50, 400*time.Millisecond)
		} else {
			go ws.ForwardLogJSON(ctx, ls.hub, msgs)
		}
	} else {
		cleanChan := make(chan string, 256)
		go func() {
			defer close(cleanChan)
			for line := range lines {
				_, _, msg, _ := ws.NormalizeProjectLine(line)
				cleanChan <- msg
			}
		}()
		go ws.ForwardLines(ctx, ls.hub, cleanChan)
	}

	return ls.hub
}

// GetProjectLogsWS godoc
//
//	@Summary		Get project logs via WebSocket
//	@Description	Stream project logs over WebSocket connection
//	@Tags			Projects
//	@Param			id			path	string	true	"Environment ID"
//	@Param			projectId	path	string	true	"Project ID"
//	@Param			follow		query	bool	false	"Follow log output"						default(true)
//	@Param			tail		query	string	false	"Number of lines to show from the end"	default(100)
//	@Param			since		query	string	false	"Show logs since timestamp"
//	@Param			timestamps	query	bool	false	"Show timestamps"				default(false)
//	@Param			format		query	string	false	"Output format (text or json)"	default(text)
//	@Param			batched		query	bool	false	"Batch log messages"			default(false)
//	@Router			/api/environments/{id}/projects/{projectId}/logs/ws [get]
func (h *ProjectHandler) GetProjectLogsWS(c *gin.Context) {
	projectID := c.Param("projectId")
	if strings.TrimSpace(projectID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.ProjectIDRequiredError{}).Error()})
		return
	}

	follow := c.DefaultQuery("follow", "true") == "true"
	tail := c.DefaultQuery("tail", "100")
	since := c.Query("since")
	timestamps := c.DefaultQuery("timestamps", "false") == "true"
	format := c.DefaultQuery("format", "text")
	batched := c.DefaultQuery("batched", "false") == "true"

	conn, err := h.wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	hub := h.getOrStartProjectLogHub(projectID, format, batched, follow, tail, since, timestamps)
	ws.ServeClient(context.Background(), hub, conn)
}

// GetProjectStatusCounts godoc
//
//	@Summary		Get project status counts
//	@Description	Get counts of running, stopped, and total projects
//	@Tags			Projects
//	@Param			id	path		string	true	"Environment ID"
//	@Success		200	{object}	base.ApiResponse[project.StatusCounts]
//	@Router			/api/environments/{id}/projects/counts [get]
func (h *ProjectHandler) GetProjectStatusCounts(c *gin.Context) {
	_, running, stopped, total, err := h.projectService.GetProjectStatusCounts(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.ProjectStatusCountsError{Err: err}).Error()},
		})
		return
	}

	out := project.StatusCounts{
		RunningProjects: int(running),
		StoppedProjects: int(stopped),
		TotalProjects:   int(total),
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}
