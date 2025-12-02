package metadata

const (
	// CommandAnnotationPlugin is added to every stub command added by
	// AddPluginCommandStubs with the value "true" and so can be
	// used to distinguish plugin stubs from regular commands.
	CommandAnnotationPlugin = "com.docker.cli.plugin"

	// CommandAnnotationPluginVendor is added to every stub command
	// added by AddPluginCommandStubs and contains the vendor of
	// that plugin.
	CommandAnnotationPluginVendor = "com.docker.cli.plugin.vendor"

	// CommandAnnotationPluginVersion is added to every stub command
	// added by AddPluginCommandStubs and contains the version of
	// that plugin.
	CommandAnnotationPluginVersion = "com.docker.cli.plugin.version"

	// CommandAnnotationPluginInvalid is added to any stub command
	// added by AddPluginCommandStubs for an invalid command (that
	// is, one which failed it's candidate test) and contains the
	// reason for the failure.
	CommandAnnotationPluginInvalid = "com.docker.cli.plugin-invalid"

	// CommandAnnotationPluginCommandPath is added to overwrite the
	// command path for a plugin invocation.
	CommandAnnotationPluginCommandPath = "com.docker.cli.plugin.command_path"
)
