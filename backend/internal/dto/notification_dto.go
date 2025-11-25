package dto

import "github.com/ofkm/arcane-backend/internal/models"

type NotificationSettingsRequest struct {
	Provider models.NotificationProvider `json:"provider" binding:"required"`
	Enabled  bool                        `json:"enabled"`
	Config   models.JSON                 `json:"config" binding:"required"`
}

type NotificationSettingsResponse struct {
	ID       uint                        `json:"id"`
	Provider models.NotificationProvider `json:"provider"`
	Enabled  bool                        `json:"enabled"`
	Config   models.JSON                 `json:"config"`
}

type AppriseSettingsRequest struct {
	APIURL             string `json:"apiUrl"`
	Enabled            bool   `json:"enabled"`
	ImageUpdateTag     string `json:"imageUpdateTag"`
	ContainerUpdateTag string `json:"containerUpdateTag"`
}

type AppriseSettingsResponse struct {
	ID                 uint   `json:"id"`
	APIURL             string `json:"apiUrl"`
	Enabled            bool   `json:"enabled"`
	ImageUpdateTag     string `json:"imageUpdateTag"`
	ContainerUpdateTag string `json:"containerUpdateTag"`
}
