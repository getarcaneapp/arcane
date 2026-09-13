package features

// DisabledError describes an unavailable runtime feature using problem details.
type DisabledError struct {
	Status  int    `json:"status"`
	Title   string `json:"title"`
	Detail  string `json:"detail"`
	Code    string `json:"code"`
	Feature ID     `json:"feature"`
}

// Error returns the reason the feature operation was rejected.
func (e *DisabledError) Error() string { return e.Detail }

// GetStatus supplies the HTTP response status to Huma.
func (e *DisabledError) GetStatus() int { return e.Status }

// ContentType selects the problem-details media type for JSON and CBOR responses.
func (e *DisabledError) ContentType(contentType string) string {
	switch contentType {
	case "application/json":
		return "application/problem+json"
	case "application/cbor":
		return "application/problem+cbor"
	default:
		return contentType
	}
}
