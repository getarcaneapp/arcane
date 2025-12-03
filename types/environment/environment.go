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
}
