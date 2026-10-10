package backup

// Requester is who started a manual backup; its workflow re-checks their permission on every delivery.
type Requester struct {
	UserID        string `json:"userId"`
	APIKeyID      string `json:"apiKeyId,omitempty"`
	EnvironmentID string `json:"environmentId"`
	Permission    string `json:"permission"`
}
