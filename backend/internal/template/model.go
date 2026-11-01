package template

import "getarcane.dev/arcane/v3/internal/database"

type (
	TemplateRegistry struct {
		database.BaseModel

		Name        string `json:"name" unorm:"nfc" trim:"true"`
		URL         string `json:"url"`
		Description string `json:"description" unorm:"nfc" trim:"true"`
		Enabled     bool   `json:"enabled"`
	}

	ComposeTemplate struct {
		database.BaseModel

		EnvContent *string                  `json:"envContent,omitempty" gorm:"type:text"`
		RegistryID *string                  `json:"registryId,omitempty"`
		Registry   *TemplateRegistry        `json:"registry,omitempty" gorm:"foreignKey:RegistryID;references:ID"`
		Metadata   *ComposeTemplateMetadata `json:"metadata,omitempty" gorm:"embedded;embeddedPrefix:meta_"`

		Name        string `json:"name" unorm:"nfc" trim:"true" minLength:"1"`
		Description string `json:"description" unorm:"nfc" trim:"true"`
		Content     string `json:"content" gorm:"type:text"`
		IsCustom    bool   `json:"isCustom"`
		IsRemote    bool   `json:"isRemote"`
	}

	ComposeTemplateMetadata struct {
		Version          *string  `json:"version,omitempty"`
		Author           *string  `json:"author,omitempty"`
		RemoteURL        *string  `json:"remoteUrl,omitempty"`
		EnvURL           *string  `json:"envUrl,omitempty"`
		DocumentationURL *string  `json:"documentationUrl,omitempty"`
		IconURL          *string  `json:"iconUrl,omitempty"`
		RegistryIconURL  *string  `json:"registryIconUrl,omitempty"`
		Tags             []string `json:"tags,omitempty" gorm:"serializer:json"`
	}
)

func (TemplateRegistry) TableName() string { return "template_registries" }

func (ComposeTemplate) TableName() string { return "compose_templates" }
