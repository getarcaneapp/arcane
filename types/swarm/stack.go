package swarm

import "time"

type StackSummary struct {
	// ID is the unique identifier for the stack (uses namespace).
	//
	// Required: true
	ID string `json:"id"`

	// Name is the stack name.
	//
	// Required: true
	Name string `json:"name"`

	// Namespace is the stack namespace label value.
	//
	// Required: true
	Namespace string `json:"namespace"`

	// Services is the number of services in the stack.
	//
	// Required: true
	Services int `json:"services"`

	// CreatedAt is the earliest service creation time in the stack.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the latest service update time in the stack.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`
}

type StackInspect struct {
	// Name is the stack name.
	//
	// Required: true
	Name string `json:"name"`

	// Namespace is the stack namespace label value.
	//
	// Required: true
	Namespace string `json:"namespace"`

	// Services is the number of services in the stack.
	//
	// Required: true
	Services int `json:"services"`

	// CreatedAt is the earliest service creation time in the stack.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the latest service update time in the stack.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`
}

type StackRenderConfigRequest struct {
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
}

type StackRenderConfigResponse struct {
	// Name is the stack name.
	//
	// Required: true
	Name string `json:"name"`

	// RenderedCompose is the normalized compose config output.
	//
	// Required: true
	RenderedCompose string `json:"renderedCompose"`

	// Services contains service names discovered in the compose file.
	//
	// Required: true
	Services []string `json:"services"`

	// Networks contains network names discovered in the compose file.
	//
	// Required: true
	Networks []string `json:"networks"`

	// Volumes contains volume names discovered in the compose file.
	//
	// Required: true
	Volumes []string `json:"volumes"`

	// Configs contains config names discovered in the compose file.
	//
	// Required: true
	Configs []string `json:"configs"`

	// Secrets contains secret names discovered in the compose file.
	//
	// Required: true
	Secrets []string `json:"secrets"`

	// Warnings contains non-fatal warnings.
	//
	// Required: false
	Warnings []string `json:"warnings,omitempty"`
}

type StackSource struct {
	// Name is the stack name.
	//
	// Required: true
	Name string `json:"name"`

	// ComposeContent is the original Docker Compose YAML content used for deployment.
	//
	// Required: true
	ComposeContent string `json:"composeContent"`

	// EnvContent is the optional original environment file content used for deployment.
	//
	// Required: false
	EnvContent string `json:"envContent,omitempty"`
}
