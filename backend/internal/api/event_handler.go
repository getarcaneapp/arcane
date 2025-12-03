package api

import (
	"net/http"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/gin-gonic/gin"
	"go.getarcane.app/types/event"
)

type EventHandler struct {
	eventService *services.EventService
}

func NewEventHandler(group *gin.RouterGroup, eventService *services.EventService, authMiddleware *middleware.AuthMiddleware) {
	handler := &EventHandler{eventService: eventService}

	apiGroup := group.Group("/events")
	apiGroup.Use(authMiddleware.WithAdminRequired().Add())
	{
		apiGroup.GET("", handler.ListEvents)
		apiGroup.POST("", handler.CreateEvent)
		apiGroup.DELETE("/:eventId", handler.DeleteEvent)
		apiGroup.GET("/environment/:environmentId", handler.GetEventsByEnvironment)
	}
}

// ListEvents godoc
//
//	@Summary		List events
//	@Description	Get a paginated list of system events
//	@Tags			Events
//	@Param			pagination[page]	query		int		false	"Page number for pagination"	default(1)
//	@Param			pagination[limit]	query		int		false	"Number of items per page"		default(20)
//	@Param			sort[column]		query		string	false	"Column to sort by"
//	@Param			sort[direction]		query		string	false	"Sort direction (asc or desc)"	default("asc")
//	@Success		200					{object}	base.Paginated[event.Event]
//	@Router			/api/events [get]
func (h *EventHandler) ListEvents(c *gin.Context) {
	params := pagination.ExtractListModifiersQueryParams(c)

	events, paginationResp, err := h.eventService.ListEventsPaginated(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.EventListError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"data":       events,
		"pagination": paginationResp,
	})
}

// GetEventsByEnvironment godoc
//
//	@Summary		Get events by environment
//	@Description	Get a paginated list of events for a specific environment
//	@Tags			Events
//	@Param			environmentId		path		string	true	"Environment ID"
//	@Param			pagination[page]	query		int		false	"Page number for pagination"	default(1)
//	@Param			pagination[limit]	query		int		false	"Number of items per page"		default(20)
//	@Param			sort[column]		query		string	false	"Column to sort by"
//	@Param			sort[direction]		query		string	false	"Sort direction (asc or desc)"	default("asc")
//	@Success		200					{object}	base.Paginated[event.Event]
//	@Router			/api/events/environment/{environmentId} [get]
func (h *EventHandler) GetEventsByEnvironment(c *gin.Context) {
	environmentID := c.Param("environmentId")
	if environmentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.EnvironmentIDRequiredError{}).Error()},
		})
		return
	}

	params := pagination.ExtractListModifiersQueryParams(c)

	events, paginationResp, err := h.eventService.GetEventsByEnvironmentPaginated(c.Request.Context(), environmentID, params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.EventListError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"data":       events,
		"pagination": paginationResp,
	})
}

// CreateEvent godoc
//
//	@Summary		Create an event
//	@Description	Create a new system event
//	@Tags			Events
//	@Accept			json
//	@Produce		json
//	@Param			event	body		event.Create	true	"Event creation data"
//	@Success		201		{object}	base.ApiResponse[event.Event]
//	@Router			/api/events [post]
func (h *EventHandler) CreateEvent(c *gin.Context) {
	var req event.Create
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	event, err := h.eventService.CreateEventFromDto(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.EventCreationError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    event,
	})
}

// DeleteEvent godoc
//
//	@Summary		Delete an event
//	@Description	Delete a system event by ID
//	@Tags			Events
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			eventId	path		string	true	"Event ID"
//	@Success		200		{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		400		{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		500		{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/events/{eventId} [delete]
func (h *EventHandler) DeleteEvent(c *gin.Context) {
	eventID := c.Param("eventId")
	if eventID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.EventIDRequiredError{}).Error()},
		})
		return
	}

	if err := h.eventService.DeleteEvent(c.Request.Context(), eventID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.EventDeletionError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "Event deleted successfully"},
	})
}
