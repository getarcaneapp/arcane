package swarm

// StackDeployRequest is used to deploy a Swarm stack from a Compose file.
type StackDeployRequest struct {
	// Name is the stack name (namespace).
	//
	// Required: true
	Name string `json:"name"`

	// ComposeContent is the Docker Compose YAML content.
	//
	// Required: true
	ComposeContent string `json:"composeContent"`

	// EnvContent is the optional environment file content.
	//
	// Required: false
	EnvContent string `json:"envContent,omitempty"`

	// WithRegistryAuth sends registry auth details to Swarm agents.
	//
	// Required: false
	WithRegistryAuth bool `json:"withRegistryAuth,omitempty"`

	// Prune removes services that are no longer referenced in the stack.
	//
	// Required: false
	Prune bool `json:"prune,omitempty"`

	// ResolveImage controls how image digests are resolved (always, changed, never).
	//
	// Required: false
	ResolveImage string `json:"resolveImage,omitempty"`
}

// StackDeployResponse represents the result of a stack deployment.
type StackDeployResponse struct {
	// Name is the deployed stack name.
	//
	// Required: true
	Name string `json:"name"`
}
