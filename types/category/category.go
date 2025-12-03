package category

import "go.getarcane.app/types/meta"

// Category represents a category with associated metadata and settings.
type Category struct {
	// ID is the unique identifier of the category.
	//
	// Required: true
	ID string `json:"id"`

	// Title of the category.
	//
	// Required: true
	Title string `json:"title"`

	// Description of the category.
	//
	// Required: true
	Description string `json:"description"`

	// Icon is the icon identifier or URL for the category.
	//
	// Required: true
	Icon string `json:"icon"`

	// URL is the associated URL for the category.
	//
	// Required: true
	URL string `json:"url"`

	// Keywords is a list of keywords associated with the category.
	//
	// Required: true
	Keywords []string `json:"keywords"`

	// Settings is a list of metadata settings for the category.
	//
	// Required: true
	Settings []meta.Metadata `json:"settings"`

	// MatchingSettings is a list of settings that match a search query.
	//
	// Required: false
	MatchingSettings []meta.Metadata `json:"matchingSettings,omitempty"`

	// RelevanceScore indicates the relevance score for search results.
	//
	// Required: false
	RelevanceScore int `json:"relevanceScore,omitempty"`
}
