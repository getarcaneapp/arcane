// FIXME(thaJeztah): remove once we are a module; the go:build directive prevents go from downgrading language version to go1.16:
//go:build go1.23

package container

import (
	"strings"
	"sync"

	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/docker/api/types/container"
	"github.com/moby/sys/capability"
	"github.com/moby/sys/signal"
	"github.com/spf13/cobra"
)

// allCaps is the magic value for "all capabilities".
const allCaps = "ALL"

// allLinuxCapabilities is a list of all known Linux capabilities.
//
// TODO(thaJeztah): add descriptions, and enable descriptions for our completion scripts (cobra.CompletionOptions.DisableDescriptions is currently set to "true")
// TODO(thaJeztah): consider what casing we want to use for completion (see below);
//
// We need to consider what format is most convenient; currently we use the
// canonical name (uppercase and "CAP_" prefix), however, tab-completion is
// case-sensitive by default, so requires the user to type uppercase letters
// to filter the list of options.
//
// Bash completion provides a `completion-ignore-case on` option to make completion
// case-insensitive (https://askubuntu.com/a/87066), but it looks to be a global
// option; the current cobra.CompletionOptions also don't provide this as an option
// to be used in the generated completion-script.
//
// Fish completion has `smartcase` (by default?) which matches any case if
// all of the input is lowercase.
//
// Zsh does not appear have a dedicated option, but allows setting matching-rules
// (see https://superuser.com/a/1092328).
var allLinuxCapabilities = sync.OnceValue(func() []string {
	caps := capability.ListKnown()
	out := make([]string, 0, len(caps)+1)
	out = append(out, allCaps)
	for _, c := range caps {
		out = append(out, "CAP_"+strings.ToUpper(c.String()))
	}
	return out
})

// logDriverOptions provides the options for each built-in logging driver.
var logDriverOptions = map[string][]string{
	"awslogs": {
		"max-buffer-size", "mode", "awslogs-create-group", "awslogs-credentials-endpoint", "awslogs-datetime-format",
		"awslogs-group", "awslogs-multiline-pattern", "awslogs-region", "awslogs-stream", "tag",
	},
	"fluentd": {
		"max-buffer-size", "mode", "env", "env-regex", "labels", "fluentd-address", "fluentd-async",
		"fluentd-buffer-limit", "fluentd-request-ack", "fluentd-retry-wait", "fluentd-max-retries",
		"fluentd-sub-second-precision", "fluentd-write-timeout", "tag",
	},
	"gcplogs": {
		"max-buffer-size", "mode", "env", "env-regex", "labels", "gcp-log-cmd", "gcp-meta-id", "gcp-meta-name",
		"gcp-meta-zone", "gcp-project",
	},
	"gelf": {
		"max-buffer-size", "mode", "env", "env-regex", "labels", "gelf-address", "gelf-compression-level",
		"gelf-compression-type", "gelf-tcp-max-reconnect", "gelf-tcp-reconnect-delay", "tag",
	},
	"journald":  {"max-buffer-size", "mode", "env", "env-regex", "labels", "tag"},
	"json-file": {"max-buffer-size", "mode", "env", "env-regex", "labels", "compress", "max-file", "max-size"},
	"local":     {"max-buffer-size", "mode", "compress", "max-file", "max-size"},
	"none":      {},
	"splunk": {
		"max-buffer-size", "mode", "env", "env-regex", "labels", "splunk-caname", "splunk-capath", "splunk-format",
		"splunk-gzip", "splunk-gzip-level", "splunk-index", "splunk-insecureskipverify", "splunk-source",
		"splunk-sourcetype", "splunk-token", "splunk-url", "splunk-verify-connection", "tag",
	},
	"syslog": {
		"max-buffer-size", "mode", "env", "env-regex", "labels", "syslog-address", "syslog-facility", "syslog-format",
		"syslog-tls-ca-cert", "syslog-tls-cert", "syslog-tls-key", "syslog-tls-skip-verify", "tag",
	},
}

// builtInLogDrivers provides a list of the built-in logging drivers.
var builtInLogDrivers = sync.OnceValue(func() []string {
	drivers := make([]string, 0, len(logDriverOptions))
	for driver := range logDriverOptions {
		drivers = append(drivers, driver)
	}
	return drivers
})

// allLogDriverOptions provides all options of the built-in logging drivers.
// The list does not contain duplicates.
var allLogDriverOptions = sync.OnceValue(func() []string {
	var result []string
	seen := make(map[string]bool)
	for driver := range logDriverOptions {
		for _, opt := range logDriverOptions[driver] {
			if !seen[opt] {
				seen[opt] = true
				result = append(result, opt)
			}
		}
	}
	return result
})

// restartPolicies is a list of all valid restart-policies..
//
// TODO(thaJeztah): add descriptions, and enable descriptions for our completion scripts (cobra.CompletionOptions.DisableDescriptions is currently set to "true")
var restartPolicies = []string{
	string(container.RestartPolicyDisabled),
	string(container.RestartPolicyAlways),
	string(container.RestartPolicyOnFailure),
	string(container.RestartPolicyUnlessStopped),
}

// addCompletions adds the completions that `run` and `create` have in common.
func addCompletions(cmd *cobra.Command, dockerCLI completion.APIClientProvider) {
	_ = cmd.RegisterFlagCompletionFunc("attach", completion.FromList("stderr", "stdin", "stdout"))
	_ = cmd.RegisterFlagCompletionFunc("cap-add", completeLinuxCapabilityNames)
	_ = cmd.RegisterFlagCompletionFunc("cap-drop", completeLinuxCapabilityNames)
	_ = cmd.RegisterFlagCompletionFunc("cgroupns", completeCgroupns())
	_ = cmd.RegisterFlagCompletionFunc("env", completion.EnvVarNames)
	_ = cmd.RegisterFlagCompletionFunc("env-file", completion.FileNames)
	_ = cmd.RegisterFlagCompletionFunc("ipc", completeIpc(dockerCLI))
	_ = cmd.RegisterFlagCompletionFunc("link", completeLink(dockerCLI))
	_ = cmd.RegisterFlagCompletionFunc("log-driver", completeLogDriver(dockerCLI))
	_ = cmd.RegisterFlagCompletionFunc("log-opt", completeLogOpt)
	_ = cmd.RegisterFlagCompletionFunc("network", completion.NetworkNames(dockerCLI))
	_ = cmd.RegisterFlagCompletionFunc("pid", completePid(dockerCLI))
	_ = cmd.RegisterFlagCompletionFunc("platform", completion.Platforms)
	_ = cmd.RegisterFlagCompletionFunc("pull", completion.FromList(PullImageAlways, PullImageMissing, PullImageNever))
	_ = cmd.RegisterFlagCompletionFunc("restart", completeRestartPolicies)
	_ = cmd.RegisterFlagCompletionFunc("security-opt", completeSecurityOpt)
	_ = cmd.RegisterFlagCompletionFunc("stop-signal", completeSignals)
	_ = cmd.RegisterFlagCompletionFunc("storage-opt", completeStorageOpt)
	_ = cmd.RegisterFlagCompletionFunc("ulimit", completeUlimit)
	_ = cmd.RegisterFlagCompletionFunc("userns", completion.FromList("host"))
	_ = cmd.RegisterFlagCompletionFunc("uts", completion.FromList("host"))
	_ = cmd.RegisterFlagCompletionFunc("volume-driver", completeVolumeDriver(dockerCLI))
	_ = cmd.RegisterFlagCompletionFunc("volumes-from", completion.ContainerNames(dockerCLI, true))
}

// completeCgroupns implements shell completion for the `--cgroupns` option of `run` and `create`.
func completeCgroupns() cobra.CompletionFunc {
	return completion.FromList(string(container.CgroupnsModeHost), string(container.CgroupnsModePrivate))
}

// completeDetachKeys implements shell completion for the `--detach-keys` option of `run` and `create`.
func completeDetachKeys(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{"ctrl-"}, cobra.ShellCompDirectiveNoSpace
}

// completeIpc implements shell completion for the `--ipc` option of `run` and `create`.
// The completion is partly composite.
func completeIpc(dockerCLI completion.APIClientProvider) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(toComplete) > 0 && strings.HasPrefix("container", toComplete) { //nolint:gocritic // not swapped, matches partly typed "container"
			return []string{"container:"}, cobra.ShellCompDirectiveNoSpace
		}
		if strings.HasPrefix(toComplete, "container:") {
			names, _ := completion.ContainerNames(dockerCLI, true)(cmd, args, toComplete)
			return prefixWith("container:", names), cobra.ShellCompDirectiveNoFileComp
		}
		return []string{
			string(container.IPCModeContainer + ":"),
			string(container.IPCModeHost),
			string(container.IPCModeNone),
			string(container.IPCModePrivate),
			string(container.IPCModeShareable),
		}, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeLink implements shell completion for the `--link` option  of `run` and `create`.
func completeLink(dockerCLI completion.APIClientProvider) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return postfixWith(":", containerNames(dockerCLI, cmd, args, toComplete)), cobra.ShellCompDirectiveNoSpace
	}
}

// completeLogDriver implements shell completion for the `--log-driver` option  of `run` and `create`.
// The log drivers are collected from a call to the Info endpoint with a fallback to a hard-coded list
// of the build-in log drivers.
func completeLogDriver(dockerCLI completion.APIClientProvider) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		info, err := dockerCLI.Client().Info(cmd.Context())
		if err != nil {
			return builtInLogDrivers(), cobra.ShellCompDirectiveNoFileComp
		}
		drivers := info.Plugins.Log
		return drivers, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeLogOpt implements shell completion for the `--log-opt` option  of `run` and `create`.
// If the user supplied a log-driver, only options for that driver are returned.
func completeLogOpt(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	driver, _ := cmd.Flags().GetString("log-driver")
	if options, exists := logDriverOptions[driver]; exists {
		return postfixWith("=", options), cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
	}
	return postfixWith("=", allLogDriverOptions()), cobra.ShellCompDirectiveNoSpace
}

// completePid implements shell completion for the `--pid` option  of `run` and `create`.
func completePid(dockerCLI completion.APIClientProvider) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(toComplete) > 0 && strings.HasPrefix("container", toComplete) { //nolint:gocritic // not swapped, matches partly typed "container"
			return []string{"container:"}, cobra.ShellCompDirectiveNoSpace
		}
		if strings.HasPrefix(toComplete, "container:") {
			names, _ := completion.ContainerNames(dockerCLI, true)(cmd, args, toComplete)
			return prefixWith("container:", names), cobra.ShellCompDirectiveNoFileComp
		}
		return []string{"container:", "host"}, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeSecurityOpt implements shell completion for the `--security-opt` option of `run` and `create`.
// The completion is partly composite.
func completeSecurityOpt(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(toComplete) > 0 && strings.HasPrefix("apparmor=", toComplete) { //nolint:gocritic // not swapped, matches partly typed "apparmor="
		return []string{"apparmor="}, cobra.ShellCompDirectiveNoSpace
	}
	if len(toComplete) > 0 && strings.HasPrefix("label", toComplete) { //nolint:gocritic // not swapped, matches partly typed "label"
		return []string{"label="}, cobra.ShellCompDirectiveNoSpace
	}
	if strings.HasPrefix(toComplete, "label=") {
		if strings.HasPrefix(toComplete, "label=d") {
			return []string{"label=disable"}, cobra.ShellCompDirectiveNoFileComp
		}
		labels := []string{"disable", "level:", "role:", "type:", "user:"}
		return prefixWith("label=", labels), cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
	}
	// length must be > 1 here so that completion of "s" falls through.
	if len(toComplete) > 1 && strings.HasPrefix("seccomp", toComplete) { //nolint:gocritic // not swapped, matches partly typed "seccomp"
		return []string{"seccomp="}, cobra.ShellCompDirectiveNoSpace
	}
	if strings.HasPrefix(toComplete, "seccomp=") {
		return []string{"seccomp=unconfined"}, cobra.ShellCompDirectiveNoFileComp
	}
	return []string{"apparmor=", "label=", "no-new-privileges", "seccomp=", "systempaths=unconfined"}, cobra.ShellCompDirectiveNoFileComp
}

// completeStorageOpt implements shell completion for the `--storage-opt` option  of `run` and `create`.
func completeStorageOpt(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{"size="}, cobra.ShellCompDirectiveNoSpace
}

// completeUlimit implements shell completion for the `--ulimit` option of `run` and `create`.
func completeUlimit(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	limits := []string{
		"as",
		"chroot",
		"core",
		"cpu",
		"data",
		"fsize",
		"locks",
		"maxlogins",
		"maxsyslogins",
		"memlock",
		"msgqueue",
		"nice",
		"nofile",
		"nproc",
		"priority",
		"rss",
		"rtprio",
		"sigpending",
		"stack",
	}
	return postfixWith("=", limits), cobra.ShellCompDirectiveNoSpace
}

// completeVolumeDriver contacts the API to get the built-in and installed volume drivers.
func completeVolumeDriver(dockerCLI completion.APIClientProvider) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		info, err := dockerCLI.Client().Info(cmd.Context())
		if err != nil {
			// fallback: the built-in drivers
			return []string{"local"}, cobra.ShellCompDirectiveNoFileComp
		}
		drivers := info.Plugins.Volume
		return drivers, cobra.ShellCompDirectiveNoFileComp
	}
}

// containerNames contacts the API to get names and optionally IDs of containers.
// In case of an error, an empty list is returned.
func containerNames(dockerCLI completion.APIClientProvider, cmd *cobra.Command, args []string, toComplete string) []string {
	names, _ := completion.ContainerNames(dockerCLI, true)(cmd, args, toComplete)
	if names == nil {
		return []string{}
	}
	return names
}

// prefixWith prefixes every element in the slice with the given prefix.
func prefixWith(prefix string, values []string) []string {
	result := make([]string, len(values))
	for i, v := range values {
		result[i] = prefix + v
	}
	return result
}

// postfixWith appends postfix to every element in the slice.
func postfixWith(postfix string, values []string) []string {
	result := make([]string, len(values))
	for i, v := range values {
		result[i] = v + postfix
	}
	return result
}

func completeLinuxCapabilityNames(cmd *cobra.Command, args []string, toComplete string) (names []string, _ cobra.ShellCompDirective) {
	return completion.FromList(allLinuxCapabilities()...)(cmd, args, toComplete)
}

func completeRestartPolicies(cmd *cobra.Command, args []string, toComplete string) (names []string, _ cobra.ShellCompDirective) {
	return completion.FromList(restartPolicies...)(cmd, args, toComplete)
}

func completeSignals(cmd *cobra.Command, args []string, toComplete string) (names []string, _ cobra.ShellCompDirective) {
	// TODO(thaJeztah): do we want to provide the full list here, or a subset?
	signalNames := make([]string, 0, len(signal.SignalMap))
	for k := range signal.SignalMap {
		signalNames = append(signalNames, k)
	}
	return completion.FromList(signalNames...)(cmd, args, toComplete)
}
