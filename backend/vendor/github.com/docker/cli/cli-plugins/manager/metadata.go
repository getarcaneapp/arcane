package manager

import (
	"github.com/docker/cli/cli-plugins/metadata"
)

const (
	// NamePrefix is the prefix required on all plugin binary names
	//
	// Deprecated: use [metadata.NamePrefix]. This alias will be removed in a future release.
	NamePrefix = metadata.NamePrefix

	// MetadataSubcommandName is the name of the plugin subcommand
	// which must be supported by every plugin and returns the
	// plugin metadata.
	//
	// Deprecated: use [metadata.MetadataSubcommandName]. This alias will be removed in a future release.
	MetadataSubcommandName = metadata.MetadataSubcommandName

	// HookSubcommandName is the name of the plugin subcommand
	// which must be implemented by plugins declaring support
	// for hooks in their metadata.
	//
	// Deprecated: use [metadata.HookSubcommandName]. This alias will be removed in a future release.
	HookSubcommandName = metadata.HookSubcommandName
)

// Metadata provided by the plugin.
//
// Deprecated: use [metadata.Metadata]. This alias will be removed in a future release.
type Metadata = metadata.Metadata
