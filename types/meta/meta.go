package meta

// Metadata represents metadata about a configuration or setting.
type Metadata struct {
	// Key is the identifier for the metadata.
	//
	// Required: true
	Key string `json:"key"`

	// Label is the human-readable label for the metadata.
	//
	// Required: true
	Label string `json:"label"`

	// Type is the data type of the metadata value.
	//
	// Required: true
	Type string `json:"type"`

	// Keywords is a list of keywords associated with the metadata.
	//
	// Required: false
	Keywords []string `json:"keywords,omitempty"`

	// Description provides additional information about the metadata.
	//
	// Required: false
	Description string `json:"description,omitempty"`
}
