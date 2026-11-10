package sync

import (
	"path"
	"path/filepath"
	"slices"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/pkg/projects"
)

func overrideFileNameFor(repoPath string) string {
	name := path.Base(filepath.ToSlash(repoPath))
	return kit.Ternary(slices.Contains(projects.ComposeOverrideFileCandidates, name), name, projects.DefaultComposeOverrideFileName)
}
