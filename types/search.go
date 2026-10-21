package types

// SearchRequest represents a search request.
type SearchRequest struct {
	// Query is the search query string.
	//
	// Required: true
	Query string `json:"query" binding:"required,min=1"`
}

// SearchResponse represents the results of a search.
type SearchResponse struct {
	// Results is a list of categories matching the search query.
	//
	// Required: true
	Results []Category `json:"results"`

	// Query is the search query that was executed.
	//
	// Required: true
	Query string `json:"query"`

	// Count is the number of results returned.
	//
	// Required: true
	Count int `json:"count"`
}

// SearchProfile selects the metadata defaults and ranking rules for a search surface.
type SearchProfile uint8

const (
	SearchProfileSettings SearchProfile = iota + 1
	SearchProfileCustomize
)
