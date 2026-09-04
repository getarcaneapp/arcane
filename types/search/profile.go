package search

// Profile selects the metadata defaults and ranking rules for a search surface.
type Profile uint8

const (
	SettingsProfile Profile = iota + 1
	CustomizeProfile
)
