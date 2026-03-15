package models

import (
	"time"
)

type ContainerRegistry struct {
	URL                 string     `json:"url" sortable:"true"`
	Username            string     `json:"username" sortable:"true"`
	Token               string     `json:"token"`
	Description         *string    `json:"description,omitempty" sortable:"true"`
	Insecure            bool       `json:"insecure" sortable:"true"`
	Enabled             bool       `json:"enabled" sortable:"true"`
	RegistryType        string     `json:"registryType" sortable:"true"`
	AWSAccessKeyID      string     `json:"awsAccessKeyId"`
	AWSSecretAccessKey  string     `json:"awsSecretAccessKey"`
	AWSRegion           string     `json:"awsRegion"`
	ECRToken            string     `json:"ecrToken"`
	ECRTokenGeneratedAt *time.Time `json:"ecrTokenGeneratedAt"`
	CreatedAt           time.Time  `json:"createdAt" sortable:"true"`
	UpdatedAt           time.Time  `json:"updatedAt" sortable:"true"`
	BaseModel
}

func (ContainerRegistry) TableName() string {
	return "container_registries"
}

type CreateContainerRegistryRequest struct {
	URL                string  `json:"url" binding:"required"`
	Username           string  `json:"username"`
	Token              string  `json:"token"`
	Description        *string `json:"description"`
	Insecure           *bool   `json:"insecure"`
	Enabled            *bool   `json:"enabled"`
	RegistryType       string  `json:"registryType"`
	AWSAccessKeyID     string  `json:"awsAccessKeyId"`
	AWSSecretAccessKey string  `json:"awsSecretAccessKey"`
	AWSRegion          string  `json:"awsRegion"`
}

type UpdateContainerRegistryRequest struct {
	URL                *string `json:"url"`
	Username           *string `json:"username"`
	Token              *string `json:"token"`
	Description        *string `json:"description"`
	Insecure           *bool   `json:"insecure"`
	Enabled            *bool   `json:"enabled"`
	RegistryType       *string `json:"registryType"`
	AWSAccessKeyID     *string `json:"awsAccessKeyId"`
	AWSSecretAccessKey *string `json:"awsSecretAccessKey"`
	AWSRegion          *string `json:"awsRegion"`
}
