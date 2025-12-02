package metadata

const (
	// NamePrefix is the prefix required on all plugin binary names
	NamePrefix = "docker-"

	// MetadataSubcommandName is the name of the plugin subcommand
	// which must be supported by every plugin and returns the
	// plugin metadata.
	MetadataSubcommandName = "docker-cli-plugin-metadata"

	// HookSubcommandName is the name of the plugin subcommand
	// which must be implemented by plugins declaring support
	// for hooks in their metadata.
	HookSubcommandName = "docker-cli-plugin-hooks"

	// ReexecEnvvar is the name of an ennvar which is set to the command
	// used to originally invoke the docker CLI when executing a
	// plugin. Assuming $PATH and $CWD remain unchanged this should allow
	// the plugin to re-execute the original CLI.
	ReexecEnvvar = "DOCKER_CLI_PLUGIN_ORIGINAL_CLI_COMMAND"
)

// Metadata provided by the plugin.
type Metadata struct {
	// SchemaVersion describes the version of this struct. Mandatory, must be "0.1.0"
	SchemaVersion string `json:",omitempty"`
	// Vendor is the name of the plugin vendor. Mandatory
	Vendor string `json:",omitempty"`
	// Version is the optional version of this plugin.
	Version string `json:",omitempty"`
	// ShortDescription should be suitable for a single line help message.
	ShortDescription string `json:",omitempty"`
	// URL is a pointer to the plugin's homepage.
	URL string `json:",omitempty"`
	// Hidden hides the plugin in completion and help message output.
	Hidden bool `json:",omitempty"`
}
