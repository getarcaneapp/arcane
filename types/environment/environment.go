package environment

type Create struct {
	// ApiUrl is the URL of the environment API.
	//
	// Required: true
	ApiUrl string `json:"apiUrl" binding:"required,url"`

	// Name of the environment.
	//
	// Required: false
	Name *string `json:"name,omitempty"`

	// Enabled indicates if the environment is enabled.
	//
	// Required: false
	Enabled *bool `json:"enabled,omitempty"`

	// AccessToken for authentication with the environment.
	//
	// Required: false
	AccessToken *string `json:"accessToken,omitempty"`

	// BootstrapToken for initial setup of the environment.
	//
	// Required: false
	BootstrapToken *string `json:"bootstrapToken,omitempty"`

	// Tags for categorizing the environment.
	//
	// Required: false
	Tags []string `json:"tags,omitempty"`
}

type Update struct {
	// ApiUrl is the URL of the environment API.
	//
	// Required: false
	ApiUrl *string `json:"apiUrl,omitempty" binding:"omitempty,url"`

	// Name of the environment.
	//
	// Required: false
	Name *string `json:"name,omitempty"`

	// Enabled indicates if the environment is enabled.
	//
	// Required: false
	Enabled *bool `json:"enabled,omitempty"`

	// AccessToken for authentication with the environment.
	//
	// Required: false
	AccessToken *string `json:"accessToken,omitempty"`

	// BootstrapToken for initial setup of the environment.
	//
	// Required: false
	BootstrapToken *string `json:"bootstrapToken,omitempty"`

	// Tags for categorizing the environment.
	//
	// Required: false
	Tags []string `json:"tags,omitempty"`
}

type Test struct {
	// Status of the environment test.
	//
	// Required: true
	Status string `json:"status"`

	// Message providing additional details about the test result.
	//
	// Required: false
	Message *string `json:"message,omitempty"`
}

type Response struct {
	// ID of the environment.
	//
	// Required: true
	ID string `json:"id"`

	// Name of the environment.
	//
	// Required: false
	Name string `json:"name,omitempty"`

	// ApiUrl is the URL of the environment API.
	//
	// Required: true
	ApiUrl string `json:"apiUrl"`

	// Status of the environment.
	//
	// Required: true
	Status string `json:"status"`

	// Enabled indicates if the environment is enabled.
	//
	// Required: true
	Enabled bool `json:"enabled"`

	// Tags associated with the environment.
	//
	// Required: false
	Tags []string `json:"tags,omitempty"`

	// LastSeen is the last time the environment was seen.
	//
	// Required: false
	LastSeen *string `json:"lastSeen,omitempty"`
}

// FilterCreate is the request body for creating a new environment filter.
type FilterCreate struct {
	// Name of the filter.
	//
	// Required: true
	Name string `json:"name" binding:"required"`

	// IsDefault indicates if this is the default filter.
	//
	// Required: false
	IsDefault bool `json:"isDefault"`

	// SelectedTags are tags that environments must have.
	//
	// Required: false
	SelectedTags []string `json:"selectedTags"`

	// ExcludedTags are tags that environments must not have.
	//
	// Required: false
	ExcludedTags []string `json:"excludedTags"`

	// TagMode determines how selected tags are matched ("any" or "all").
	//
	// Required: false
	TagMode string `json:"tagMode"`

	// StatusFilter filters by environment status ("all", "online", "offline").
	//
	// Required: false
	StatusFilter string `json:"statusFilter"`

	// GroupBy determines how to group environments ("none", "status", "tags").
	//
	// Required: false
	GroupBy string `json:"groupBy"`
}

// FilterUpdate is the request body for updating an environment filter.
type FilterUpdate struct {
	// Name of the filter.
	//
	// Required: false
	Name *string `json:"name,omitempty"`

	// IsDefault indicates if this is the default filter.
	//
	// Required: false
	IsDefault *bool `json:"isDefault,omitempty"`

	// SelectedTags are tags that environments must have.
	//
	// Required: false
	SelectedTags []string `json:"selectedTags,omitempty"`

	// ExcludedTags are tags that environments must not have.
	//
	// Required: false
	ExcludedTags []string `json:"excludedTags,omitempty"`

	// TagMode determines how selected tags are matched ("any" or "all").
	//
	// Required: false
	TagMode *string `json:"tagMode,omitempty"`

	// StatusFilter filters by environment status ("all", "online", "offline").
	//
	// Required: false
	StatusFilter *string `json:"statusFilter,omitempty"`

	// GroupBy determines how to group environments ("none", "status", "tags").
	//
	// Required: false
	GroupBy *string `json:"groupBy,omitempty"`
}

// FilterResponse is the response body for an environment filter.
type FilterResponse struct {
	// ID of the filter.
	//
	// Required: true
	ID string `json:"id"`

	// UserID of the filter owner.
	//
	// Required: true
	UserID string `json:"userId"`

	// Name of the filter.
	//
	// Required: true
	Name string `json:"name"`

	// IsDefault indicates if this is the default filter.
	//
	// Required: true
	IsDefault bool `json:"isDefault"`

	// SelectedTags are tags that environments must have.
	//
	// Required: true
	SelectedTags []string `json:"selectedTags"`

	// ExcludedTags are tags that environments must not have.
	//
	// Required: true
	ExcludedTags []string `json:"excludedTags"`

	// TagMode determines how selected tags are matched ("any" or "all").
	//
	// Required: true
	TagMode string `json:"tagMode"`

	// StatusFilter filters by environment status ("all", "online", "offline").
	//
	// Required: true
	StatusFilter string `json:"statusFilter"`

	// GroupBy determines how to group environments ("none", "status", "tags").
	//
	// Required: true
	GroupBy string `json:"groupBy"`

	// CreatedAt is the creation timestamp.
	//
	// Required: false
	CreatedAt string `json:"createdAt,omitempty"`

	// UpdatedAt is the last update timestamp.
	//
	// Required: false
	UpdatedAt string `json:"updatedAt,omitempty"`
}
