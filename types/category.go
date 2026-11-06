package types

type (
	Category struct {
		ID               string            `json:"id"`
		Title            string            `json:"title"`
		Description      string            `json:"description"`
		Icon             string            `json:"icon"`
		URL              string            `json:"url"`
		Keywords         []string          `json:"keywords"`
		Settings         []SettingMetadata `json:"settings"`
		MatchingSettings []SettingMetadata `json:"matchingSettings,omitempty"`
		RelevanceScore   int               `json:"relevanceScore,omitempty"`
	}
)
