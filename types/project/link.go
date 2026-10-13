package project

// Link is a project URL with an optional display label.
type Link struct {
	URL   string `json:"url"`
	Label string `json:"label,omitempty"`
}
