package system

// HealthResponse contains the health status of the API.
type HealthResponse struct {
	// Status indicates the health status (e.g., "UP", "DOWN").
	//
	// Required: true
	Status string `json:"status"`
}

// ReadinessResponse is the body of the readiness probe; Checks maps each dependency to "ok" or its error.
type ReadinessResponse struct {
	// Status is "ready" or "not_ready".
	//
	// Required: true
	Status string `json:"status"`
	// Checks maps each dependency name to "ok" or its error.
	//
	// Required: true
	Checks map[string]string `json:"checks"`
}
