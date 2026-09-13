package project

import (
	composetypes "github.com/compose-spec/compose-go/v2/types"
)

// VolumeSourcePathMapper translates Compose volume sources for the Docker host.
type VolumeSourcePathMapper interface {
	TranslateVolumeSources(project *composetypes.Project, translateFileResources bool) error
	// ContainerToHost translates a single container-side path to its host-side
	// equivalent. mapped is false when the path is outside every mounted
	// directory, in which case the path is returned unchanged; a path inside an
	// identity mount is also unchanged but reports mapped as true. Needed to
	// re-resolve relative Compose paths that escape the projects mount, where
	// prefix translation has nothing to match.
	ContainerToHost(containerPath string) (hostPath string, mapped bool, err error)
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
