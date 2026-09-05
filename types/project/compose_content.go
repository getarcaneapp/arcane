package project

import (
	composetypes "github.com/compose-spec/compose-go/v2/types"
)

// VolumeSourcePathMapper translates Compose volume sources for the Docker host.
type VolumeSourcePathMapper interface {
	TranslateVolumeSources(project *composetypes.Project, translateFileResources bool) error
	// ContainerToHost translates a single container-side path to its host-side
	// equivalent, returning the path unchanged when it is outside every mounted
	// directory. Needed to re-resolve relative Compose paths that escape the
	// projects mount, where prefix translation has nothing to match.
	ContainerToHost(containerPath string) (string, error)
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
