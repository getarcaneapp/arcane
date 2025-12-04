package api

import (
	"net/http"

	"github.com/docker/docker/api/types/volume"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/gin-gonic/gin"
	volumetypes "go.getarcane.app/types/volume"
)

type VolumeHandler struct {
	volumeService *services.VolumeService
	dockerService *services.DockerClientService
}

func NewVolumeHandler(group *gin.RouterGroup, dockerService *services.DockerClientService, volumeService *services.VolumeService, authMiddleware *middleware.AuthMiddleware) {
	handler := &VolumeHandler{dockerService: dockerService, volumeService: volumeService}

	apiGroup := group.Group("/environments/:id/volumes")
	apiGroup.Use(authMiddleware.WithAdminNotRequired().Add())
	{
		apiGroup.GET("/counts", handler.GetVolumeUsageCounts)
		apiGroup.GET("", handler.List)
		apiGroup.GET("/:volumeName", handler.GetByName)
		apiGroup.POST("", handler.Create)
		apiGroup.DELETE("/:volumeName", handler.Remove)
		apiGroup.POST("/prune", handler.Prune)
		apiGroup.GET("/:volumeName/usage", handler.GetUsage)
	}
}

// List godoc
//
//	@Summary		List volumes
//	@Description	Get a paginated list of Docker volumes
//	@Tags			Volumes
//	@Param			id					path		string	true	"Environment ID"
//	@Param			pagination[page]	query		int		false	"Page number for pagination"	default(1)
//	@Param			pagination[limit]	query		int		false	"Number of items per page"		default(20)
//	@Param			sort[column]		query		string	false	"Column to sort by"
//	@Param			sort[direction]		query		string	false	"Sort direction (asc or desc)"	default("asc")
//	@Success		200					{object}	base.Paginated[volume.Volume]
//	@Router			/api/environments/{id}/volumes [get]
func (h *VolumeHandler) List(c *gin.Context) {
	params := pagination.ExtractListModifiersQueryParams(c)

	if params.Limit == 0 {
		params.Limit = 20
	}

	volumes, paginationResp, counts, err := h.volumeService.ListVolumesPaginated(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.VolumeListError{Err: err}).Error()},
		})
		return
	}

	pagination.ApplyFilterResultsHeaders(&c.Writer, pagination.FilterResult[volumetypes.Volume]{
		Items:          volumes,
		TotalCount:     paginationResp.TotalItems,
		TotalAvailable: paginationResp.GrandTotalItems,
	})

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"data":       volumes,
		"counts":     counts,
		"pagination": paginationResp,
	})
}

// GetByName godoc
//
//	@Summary		Get volume by name
//	@Description	Get a Docker volume by its name
//	@Tags			Volumes
//	@Param			id			path		string	true	"Environment ID"
//	@Param			volumeName	path		string	true	"Volume name"
//	@Success		200			{object}	base.ApiResponse[volume.Volume]
//	@Router			/api/environments/{id}/volumes/{volumeName} [get]
func (h *VolumeHandler) GetByName(c *gin.Context) {
	name := c.Param("volumeName")

	vol, err := h.volumeService.GetVolumeByName(c.Request.Context(), name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.VolumeNotFoundError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    vol,
	})
}

// Create godoc
//
//	@Summary		Create a volume
//	@Description	Create a new Docker volume
//	@Tags			Volumes
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string			true	"Environment ID"
//	@Param			volume	body		volume.Create	true	"Volume creation data"
//	@Success		201		{object}	base.ApiResponse[volume.Volume]
//	@Router			/api/environments/{id}/volumes [post]
func (h *VolumeHandler) Create(c *gin.Context) {
	var req volumetypes.Create
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	options := volume.CreateOptions{
		Name:       req.Name,
		Driver:     req.Driver,
		Labels:     req.Labels,
		DriverOpts: req.Options,
	}

	currentUser, ok := middleware.RequireAuthentication(c)
	if !ok {
		return
	}

	response, err := h.volumeService.CreateVolume(c.Request.Context(), options, *currentUser)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.VolumeCreationError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    response,
	})
}

// Remove godoc
//
//	@Summary		Remove a volume
//	@Description	Remove a Docker volume by name
//	@Tags			Volumes
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id			path		string	true	"Environment ID"
//	@Param			volumeName	path		string	true	"Volume name"
//	@Param			force		query		bool	false	"Force removal"
//	@Success		200			{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		500			{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/environments/{id}/volumes/{volumeName} [delete]
func (h *VolumeHandler) Remove(c *gin.Context) {
	name := c.Param("volumeName")
	force := c.Query("force") == "true"

	currentUser, ok := middleware.RequireAuthentication(c)
	if !ok {
		return
	}

	if err := h.volumeService.DeleteVolume(c.Request.Context(), name, force, *currentUser); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.VolumeDeletionError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "Volume removed successfully"},
	})
}

// Prune godoc
//
//	@Summary		Prune unused volumes
//	@Description	Remove all unused Docker volumes
//	@Tags			Volumes
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id	path		string	true	"Environment ID"
//	@Success		200	{object}	base.ApiResponse[volume.PruneReport]
//	@Failure		500	{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/environments/{id}/volumes/prune [post]
func (h *VolumeHandler) Prune(c *gin.Context) {
	report, err := h.volumeService.PruneVolumes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.VolumePruneError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    report,
	})
}

// GetUsage godoc
//
//	@Summary		Get volume usage
//	@Description	Get containers using a specific volume
//	@Tags			Volumes
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id			path		string	true	"Environment ID"
//	@Param			volumeName	path		string	true	"Volume name"
//	@Success		200			{object}	base.ApiResponse[volume.Usage]
//	@Failure		500			{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/environments/{id}/volumes/{volumeName}/usage [get]
func (h *VolumeHandler) GetUsage(c *gin.Context) {
	name := c.Param("volumeName")

	inUse, containers, err := h.volumeService.GetVolumeUsage(c.Request.Context(), name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.VolumeUsageError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"inUse":      inUse,
			"containers": containers,
		},
	})
}

// GetVolumeUsageCounts godoc
//
//	@Summary		Get volume usage counts
//	@Description	Get counts of volumes in use, unused, and total
//	@Tags			Volumes
//	@Param			id	path		string	true	"Environment ID"
//	@Success		200	{object}	base.ApiResponse[volume.UsageCounts]
//	@Router			/api/environments/{id}/volumes/counts [get]
func (h *VolumeHandler) GetVolumeUsageCounts(c *gin.Context) {
	_, running, stopped, total, err := h.dockerService.GetAllVolumes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.VolumeCountsError{Err: err}).Error()},
		})
		return
	}

	out := volumetypes.UsageCounts{
		Inuse:  running,
		Unused: stopped,
		Total:  total,
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}
