package types

import (
	composetypes "github.com/compose-spec/compose-go/v2/types"
)

// VolumeSourcePathMapper translates Compose volume sources for the Docker host.
type VolumeSourcePathMapper interface {
	TranslateVolumeSources(project *composetypes.Project, translateFileResources bool) error
}

// ComposeContentOptions configures loading a Compose project from in-memory content.
type ComposeContentOptions struct {
	ProjectName     string
	ComposeContent  string
	OverrideContent string
	EnvContent      string
	WorkingDir      string
	PathMapper      VolumeSourcePathMapper
}
