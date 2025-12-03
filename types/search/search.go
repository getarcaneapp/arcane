package search

import "go.getarcane.app/types/category"

// Request represents a search request.
type Request struct {
	// Query is the search query string.
	//
	// Required: true
	Query string `json:"query" binding:"required,min=1"`
}

// Response represents the results of a search.
type Response struct {
	// Results is a list of categories matching the search query.
	//
	// Required: true
	Results []category.Category `json:"results"`

	// Query is the search query that was executed.
	//
	// Required: true
	Query string `json:"query"`

	// Count is the number of results returned.
	//
	// Required: true
	Count int `json:"count"`
}
