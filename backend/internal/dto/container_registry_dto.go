package dto

import "time"

type ContainerRegistryDto struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	Username    string    `json:"username"`
	Description *string   `json:"description,omitempty"`
	Insecure    bool      `json:"insecure"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type ContainerRegistrySyncDto struct {
	ID          string    `json:"id" binding:"required"`
	URL         string    `json:"url" binding:"required"`
	Username    string    `json:"username" binding:"required"`
	Token       string    `json:"token" binding:"required"`
	Description *string   `json:"description,omitempty"`
	Insecure    bool      `json:"insecure"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type ContainerRegistryCredential struct {
	URL      string `json:"url" binding:"required"`
	Username string `json:"username" binding:"required"`
	Token    string `json:"token" binding:"required"`
	Enabled  bool   `json:"enabled"`
}

type SyncRegistriesRequest struct {
	Registries []ContainerRegistrySyncDto `json:"registries" binding:"required"`
}
