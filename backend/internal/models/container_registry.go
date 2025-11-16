package models

import (
	"time"
)

type ContainerRegistry struct {
	URL         string    `json:"url" sortable:"true"`
	Username    string    `json:"username" sortable:"true"`
	Token       string    `json:"token"`
	Description *string   `json:"description,omitempty" sortable:"true"`
	Insecure    bool      `json:"insecure" sortable:"true"`
	Enabled     bool      `json:"enabled" sortable:"true"`
	CreatedAt   time.Time `json:"createdAt" sortable:"true"`
	UpdatedAt   time.Time `json:"updatedAt" sortable:"true"`
	BaseModel
}

func (ContainerRegistry) TableName() string {
	return "container_registries"
}

type CreateContainerRegistryRequest struct {
	URL         string  `json:"url" binding:"required"`
	Username    string  `json:"username" binding:"required"`
	Token       string  `json:"token" binding:"required"`
	Description *string `json:"description"`
	Insecure    *bool   `json:"insecure"`
	Enabled     *bool   `json:"enabled"`
}

type UpdateContainerRegistryRequest struct {
	URL         *string `json:"url"`
	Username    *string `json:"username"`
	Token       *string `json:"token"`
	Description *string `json:"description"`
	Insecure    *bool   `json:"insecure"`
	Enabled     *bool   `json:"enabled"`
}

type SyncRegistriesRequest struct {
	Registries []SyncRegistryItem `json:"registries" binding:"required"`
}

type SyncRegistryItem struct {
	ID          string  `json:"id" binding:"required"`
	URL         string  `json:"url" binding:"required"`
	Username    string  `json:"username" binding:"required"`
	Token       string  `json:"token" binding:"required"`
	Description *string `json:"description"`
	Insecure    bool    `json:"insecure"`
	Enabled     bool    `json:"enabled"`
}
