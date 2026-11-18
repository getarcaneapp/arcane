package types

type (
	// Compose references make a secret reach a container: compose only passes a
	// variable to a service that names it, such as DB_PASSWORD: ${DB_PASSWORD}.

	// SecretSourceComposeService is one service of a compose file.
	SecretSourceComposeService struct {
		Name   string `json:"name"`
		Reason string `json:"reason,omitempty"`
		// Available are keys the service already gets, through its environment
		// block or by interpolating them anywhere.
		Available []string `json:"available"`
		// Editable is false when Arcane cannot add references to the service as
		// text; Reason says why.
		Editable bool `json:"editable"`
	}

	SecretSourceComposeServicesRequest struct {
		Compose string `json:"compose"`
	}

	SecretSourceComposeRefsRequest struct {
		Compose string `json:"compose"`
		// Assignments maps a service name to the keys to add to it.
		Assignments map[string][]string `json:"assignments"`
		// Env is the project's .env content; EnvKeys are added to it as
		// KEY=${KEY} references.
		Env     string   `json:"env,omitempty"`
		EnvKeys []string `json:"envKeys,omitempty"`
	}

	// SecretSourceComposeSkippedRef is a key that was not added to a service.
	SecretSourceComposeSkippedRef struct {
		Service string `json:"service"`
		Key     string `json:"key"`
		Reason  string `json:"reason"`
	}

	SecretSourceComposeRefsResult struct {
		Compose string                          `json:"compose"`
		Added   map[string][]string             `json:"added"`
		Skipped []SecretSourceComposeSkippedRef `json:"skipped"`
		// Env is the updated .env content; it is set only when EnvKeys were sent.
		Env      string   `json:"env,omitempty"`
		EnvAdded []string `json:"envAdded"`
	}

	SecretSourceTargetKeysRequest struct {
		Target SecretSourceBindingTarget `json:"target"`
	}

	// SecretSourceTargetKeys are the variable names a target provides. Values are never
	// returned.
	SecretSourceTargetKeys struct {
		Keys []string `json:"keys"`
		// Skipped are entries that cannot be used as variables.
		Skipped []string `json:"skipped"`
	}
)
