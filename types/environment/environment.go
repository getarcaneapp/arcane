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
	AccessToken *string `json:"accessToken,omitempty"` //nolint:gosec // API schema requires accessToken field name

	// BootstrapToken for initial setup of the environment.
	//
	// Required: false
	BootstrapToken *string `json:"bootstrapToken,omitempty"`

	// UseApiKey indicates if an API key should be generated for pairing.
	//
	// Required: false
	UseApiKey *bool `json:"useApiKey,omitempty"`

	// IsEdge indicates if this is an edge agent that connects outbound to manager.
	//
	// Required: false
	IsEdge *bool `json:"isEdge,omitempty"`
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
	AccessToken *string `json:"accessToken,omitempty"` //nolint:gosec // API schema requires accessToken field name

	// BootstrapToken for initial setup of the environment.
	//
	// Required: false
	BootstrapToken *string `json:"bootstrapToken,omitempty"`

	// RegenerateApiKey indicates whether to regenerate the API key.
	//
	// Required: false
	RegenerateApiKey *bool `json:"regenerateApiKey,omitempty"`
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

// TestConnectionRequest is the request body for testing a connection.
type TestConnectionRequest struct {
	// ApiUrl is an optional URL to test instead of the saved one.
	//
	// Required: false
	ApiUrl *string `json:"apiUrl,omitempty"`
}

// Environment represents an environment in API responses.
type Environment struct {
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

	// IsEdge indicates if this is an edge agent that connects outbound.
	//
	// Required: false
	IsEdge bool `json:"isEdge"`

	// EdgeTransport indicates the active tunnel transport for an edge environment.
	// Values are "grpc" or "websocket" when connected.
	//
	// Required: false
	EdgeTransport *string `json:"edgeTransport,omitempty"`

	// ApiKey is returned only when creating or regenerating
	//
	// Required: false
	ApiKey *string `json:"apiKey,omitempty"` //nolint:gosec // API schema requires apiKey field name
}

// AgentPairRequest is the request body for pairing with an agent.
type AgentPairRequest struct {
	// Rotate indicates if the token should be rotated.
	//
	// Required: false
	Rotate *bool `json:"rotate,omitempty"`
}

// AgentPairResponse is the response from pairing with an agent.
type AgentPairResponse struct {
	// Token is the pairing token.
	//
	// Required: true
	Token string `json:"token"`
}
