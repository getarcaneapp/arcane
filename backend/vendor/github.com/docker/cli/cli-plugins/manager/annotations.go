package manager

import "github.com/docker/cli/cli-plugins/metadata"

const (
	// CommandAnnotationPlugin is added to every stub command added by
	// AddPluginCommandStubs with the value "true" and so can be
	// used to distinguish plugin stubs from regular commands.
	//
	// Deprecated: use [metadata.CommandAnnotationPlugin]. This alias will be removed in the next release.
	CommandAnnotationPlugin = metadata.CommandAnnotationPlugin

	// CommandAnnotationPluginVendor is added to every stub command
	// added by AddPluginCommandStubs and contains the vendor of
	// that plugin.
	//
	// Deprecated: use [metadata.CommandAnnotationPluginVendor]. This alias will be removed in the next release.
	CommandAnnotationPluginVendor = metadata.CommandAnnotationPluginVendor

	// CommandAnnotationPluginVersion is added to every stub command
	// added by AddPluginCommandStubs and contains the version of
	// that plugin.
	//
	// Deprecated: use [metadata.CommandAnnotationPluginVersion]. This alias will be removed in the next release.
	CommandAnnotationPluginVersion = metadata.CommandAnnotationPluginVersion

	// CommandAnnotationPluginInvalid is added to any stub command
	// added by AddPluginCommandStubs for an invalid command (that
	// is, one which failed it's candidate test) and contains the
	// reason for the failure.
	//
	// Deprecated: use [metadata.CommandAnnotationPluginInvalid]. This alias will be removed in the next release.
	CommandAnnotationPluginInvalid = metadata.CommandAnnotationPluginInvalid

	// CommandAnnotationPluginCommandPath is added to overwrite the
	// command path for a plugin invocation.
	//
	// Deprecated: use [metadata.CommandAnnotationPluginCommandPath]. This alias will be removed in the next release.
	CommandAnnotationPluginCommandPath = metadata.CommandAnnotationPluginCommandPath
)
