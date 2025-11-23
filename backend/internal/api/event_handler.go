package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ofkm/arcane-backend/internal/common"
	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/middleware"
	"github.com/ofkm/arcane-backend/internal/services"
	"github.com/ofkm/arcane-backend/internal/utils/pagination"
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

func (h *EventHandler) CreateEvent(c *gin.Context) {
	var req dto.CreateEventDto
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
