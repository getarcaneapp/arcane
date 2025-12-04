package api

import (
	"net/http"

	"github.com/docker/docker/api/types/network"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/gin-gonic/gin"
	networktypes "go.getarcane.app/types/network"
)

type NetworkHandler struct {
	networkService *services.NetworkService
	dockerService  *services.DockerClientService
}

func NewNetworkHandler(group *gin.RouterGroup, dockerService *services.DockerClientService, networkService *services.NetworkService, authMiddleware *middleware.AuthMiddleware) {
	handler := &NetworkHandler{dockerService: dockerService, networkService: networkService}

	apiGroup := group.Group("/environments/:id/networks")
	apiGroup.Use(authMiddleware.WithAdminNotRequired().Add())
	{
		apiGroup.GET("/counts", handler.GetNetworkUsageCounts)
		apiGroup.GET("", handler.List)
		apiGroup.GET("/:networkId", handler.GetByID)
		apiGroup.POST("", handler.Create)
		apiGroup.DELETE("/:networkId", handler.Remove)
		apiGroup.POST("/prune", handler.Prune)
	}
}

// List godoc
//
//	@Summary		List networks
//	@Description	Get a paginated list of Docker networks
//	@Tags			Networks
//	@Param			id					path		string	true	"Environment ID"
//	@Param			pagination[page]	query		int		false	"Page number for pagination"	default(1)
//	@Param			pagination[limit]	query		int		false	"Number of items per page"		default(20)
//	@Param			sort[column]		query		string	false	"Column to sort by"
//	@Param			sort[direction]		query		string	false	"Sort direction (asc or desc)"	default("asc")
//	@Success		200					{object}	base.Paginated[network.Summary]
//	@Router			/api/environments/{id}/networks [get]
func (h *NetworkHandler) List(c *gin.Context) {
	params := pagination.ExtractListModifiersQueryParams(c)

	if params.Limit == 0 {
		params.Limit = 20
	}

	networks, paginationResp, err := h.networkService.ListNetworksPaginated(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.NetworkListError{Err: err}).Error()},
		})
		return
	}

	pagination.ApplyFilterResultsHeaders(&c.Writer, pagination.FilterResult[networktypes.Summary]{
		Items:          networks,
		TotalCount:     paginationResp.TotalItems,
		TotalAvailable: paginationResp.GrandTotalItems,
	})

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"data":       networks,
		"pagination": paginationResp,
	})
}

// GetByID godoc
//
//	@Summary		Get network by ID
//	@Description	Get a Docker network by its ID
//	@Tags			Networks
//	@Param			id			path		string	true	"Environment ID"
//	@Param			networkId	path		string	true	"Network ID"
//	@Success		200			{object}	base.ApiResponse[network.Inspect]
//	@Router			/api/environments/{id}/networks/{networkId} [get]
func (h *NetworkHandler) GetByID(c *gin.Context) {
	id := c.Param("networkId")

	networkInspect, err := h.networkService.GetNetworkByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.NetworkNotFoundError{Err: err}).Error()},
		})
		return
	}

	out, mapErr := mapper.MapOne[network.Inspect, networktypes.Inspect](*networkInspect)
	if mapErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.NetworkMappingError{Err: mapErr}).Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}

// Create godoc
//
//	@Summary		Create a network
//	@Description	Create a new Docker network
//	@Tags			Networks
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string	true	"Environment ID"
//	@Param			network	body		object	true	"Network creation data"
//	@Success		201		{object}	base.ApiResponse[network.CreateResponse]
//	@Router			/api/environments/{id}/networks [post]
func (h *NetworkHandler) Create(c *gin.Context) {
	var req struct {
		Name    string                `json:"name" binding:"required"`
		Options network.CreateOptions `json:"options"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	currentUser, ok := middleware.RequireAuthentication(c)
	if !ok {
		return
	}

	response, err := h.networkService.CreateNetwork(c.Request.Context(), req.Name, req.Options, *currentUser)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.NetworkCreationError{Err: err}).Error()},
		})
		return
	}

	out, mapErr := mapper.MapOne[network.CreateResponse, networktypes.CreateResponse](*response)
	if mapErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.NetworkMappingError{Err: mapErr}).Error()}})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    out,
	})
}

// Remove godoc
//
//	@Summary		Remove a network
//	@Description	Remove a Docker network by ID
//	@Tags			Networks
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id			path		string	true	"Environment ID"
//	@Param			networkId	path		string	true	"Network ID"
//	@Success		200			{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		500			{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/environments/{id}/networks/{networkId} [delete]
func (h *NetworkHandler) Remove(c *gin.Context) {
	id := c.Param("networkId")

	currentUser, ok := middleware.RequireAuthentication(c)
	if !ok {
		return
	}

	if err := h.networkService.RemoveNetwork(c.Request.Context(), id, *currentUser); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.NetworkRemovalError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "Network removed successfully"},
	})
}

// GetNetworkUsageCounts godoc
//
//	@Summary		Get network usage counts
//	@Description	Get counts of networks in use, unused, and total
//	@Tags			Networks
//	@Param			id	path		string	true	"Environment ID"
//	@Success		200	{object}	base.ApiResponse[network.UsageCounts]
//	@Router			/api/environments/{id}/networks/counts [get]
func (h *NetworkHandler) GetNetworkUsageCounts(c *gin.Context) {
	_, inuse, unused, total, err := h.dockerService.GetAllNetworks(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.NetworkUsageCountsError{Err: err}).Error()},
		})
		return
	}

	out := networktypes.UsageCounts{
		Inuse:  inuse,
		Unused: unused,
		Total:  total,
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}

// Prune godoc
//
//	@Summary		Prune unused networks
//	@Description	Remove all unused Docker networks
//	@Tags			Networks
//	@Param			id	path		string	true	"Environment ID"
//	@Success		200	{object}	base.ApiResponse[network.PruneReport]
//	@Router			/api/environments/{id}/networks/prune [post]
func (h *NetworkHandler) Prune(c *gin.Context) {
	report, err := h.networkService.PruneNetworks(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.NetworkPruneError{Err: err}).Error()},
		})
		return
	}

	out, mapErr := mapper.MapOne[network.PruneReport, networktypes.PruneReport](*report)
	if mapErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.NetworkMappingError{Err: mapErr}).Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}
