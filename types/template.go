package types

type (
	BaseTemplate struct {
		ID          string `json:"id" readOnly:"true"`
		Name        string `json:"name" required:"false" unorm:"nfc" trim:"true" minLength:"1"`
		Description string `json:"description" required:"false" unorm:"nfc" trim:"true"`
	}

	RemoteTemplate struct {
		BaseTemplate
		Version          string   `json:"version"`
		Author           string   `json:"author"`
		ComposeURL       string   `json:"compose_url"`
		EnvURL           string   `json:"env_url"`
		DocumentationURL string   `json:"documentation_url"`
		IconURL          string   `json:"icon_url,omitempty"`
		Tags             []string `json:"tags"`
	}

	TemplateBaseRegistry struct {
		Name        string `json:"name" required:"false" unorm:"nfc" trim:"true"`
		Description string `json:"description" required:"false" unorm:"nfc" trim:"true"`
		URL         string `json:"url" required:"false"`
	}

	TemplateRemoteRegistry struct {
		TemplateBaseRegistry
		Schema    string           `json:"$schema,omitempty"`
		Version   string           `json:"version"`
		Author    string           `json:"author"`
		Templates []RemoteTemplate `json:"templates"`
	}

	TemplateRegistry struct {
		LastFetchError *string `json:"lastFetchError,omitempty" readOnly:"true"`
		TemplateBaseRegistry
		ID      string `json:"id" readOnly:"true"`
		Enabled bool   `json:"enabled" required:"false"`
	}

	TemplateContent struct {
		Template         Template      `json:"template"`
		Content          string        `json:"content"`
		EnvContent       string        `json:"envContent"`
		Services         []string      `json:"services"`
		EnvVariables     []EnvVariable `json:"envVariables"`
		IsFolderTemplate bool          `json:"isFolderTemplate"`
	}

	Template struct {
		EnvContent *string           `json:"envContent,omitempty"`
		RegistryID *string           `json:"registryId,omitempty" readOnly:"true"`
		Registry   *TemplateRegistry `json:"registry,omitempty" readOnly:"true"`
		Metadata   *TemplateMeta     `json:"metadata,omitempty" readOnly:"true"`
		BaseTemplate
		Content  string `json:"content" required:"false"`
		IsCustom bool   `json:"isCustom" readOnly:"true"`
		IsRemote bool   `json:"isRemote" readOnly:"true"`
	}

	DefaultTemplatesResponse struct {
		ComposeTemplate       string `json:"composeTemplate"`
		SwarmStackTemplate    string `json:"swarmStackTemplate"`
		SwarmStackEnvTemplate string `json:"swarmStackEnvTemplate"`
		EnvTemplate           string `json:"envTemplate"`
	}

	SaveDefaultTemplatesRequest struct {
		ComposeContent string `json:"composeContent"`
		EnvContent     string `json:"envContent"`
	}
)
