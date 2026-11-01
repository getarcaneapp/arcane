package registry

import (
	"time"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/pkg/registry"
)

type (
	ContainerRegistry struct {
		database.BaseModel

		CreatedAt           time.Time  `json:"createdAt" sortable:"true"`
		UpdatedAt           time.Time  `json:"updatedAt" sortable:"true"`
		Description         *string    `json:"description,omitempty" sortable:"true"`
		ECRTokenGeneratedAt *time.Time `json:"ecrTokenGeneratedAt"`

		URL                string               `json:"url" sortable:"true"`
		Username           string               `json:"username" sortable:"true"`
		Token              string               `json:"token"`
		RegistryType       string               `json:"registryType" sortable:"true"`
		AWSAccessKeyID     string               `json:"awsAccessKeyId"`
		AWSSecretAccessKey string               `json:"awsSecretAccessKey"`
		AWSRegion          string               `json:"awsRegion"`
		ECRToken           string               `json:"ecrToken"`
		RepositoryNames    database.StringSlice `json:"repositoryNames" gorm:"column:repository_names;type:text;not null;default:'[]'"`
		Insecure           bool                 `json:"insecure" sortable:"true"`
		Enabled            bool                 `json:"enabled" sortable:"true"`
	}
)

func (ContainerRegistry) TableName() string {
	return "container_registries"
}

func (r ContainerRegistry) Host() string {
	host, _ := registry.SplitRegistryURL(r.URL)
	return host
}

func (r ContainerRegistry) Namespace() string {
	_, namespace := registry.SplitRegistryURL(r.URL)
	return namespace
}
