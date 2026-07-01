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

	// OverrideContent is the optional Docker Compose override YAML content merged
	// on top of ComposeContent, mirroring `docker compose` override files.
	//
	// Required: false
	OverrideContent string `json:"overrideContent,omitempty"`

	// EnvContent is the optional environment file content.
	//
	// Required: false
	EnvContent string `json:"envContent,omitempty"`

	// Files is an optional list of additional files to sync (e.g. env_file, configs, secrets).
	//
	// Required: false
	Files []SyncFile `json:"files,omitempty"`

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

	// WorkingDir defines the working directory context for evaluating compose files.
	//
	// Required: false
	WorkingDir string `json:"workingDir,omitempty"`
}

// SyncFile represents a file to be synced to the target environment.
type SyncFile struct {
	// RelativePath is the path of the file relative to the stack's working directory.
	//
	// Required: true
	RelativePath string `json:"relativePath"`

	// Content is the file content.
	//
	// Required: true
	Content []byte `json:"content"`
}

// StackDeployResponse represents the result of a stack deployment.
type StackDeployResponse struct {
	// Name is the deployed stack name.
	//
	// Required: true
	Name string `json:"name"`
}
