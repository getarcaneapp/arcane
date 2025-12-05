package system

// HealthResponse contains the health status of the API.
type HealthResponse struct {
	// Status indicates the health status (e.g., "UP", "DOWN").
	//
	// Required: true
	Status string `json:"status"`
}
