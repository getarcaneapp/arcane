package manager

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/containerd/errdefs"
	"github.com/docker/cli/cli-plugins/metadata"
	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/debug"
	"github.com/fvbommel/sortorder"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

const (
	// ReexecEnvvar is the name of an ennvar which is set to the command
	// used to originally invoke the docker CLI when executing a
	// plugin. Assuming $PATH and $CWD remain unchanged this should allow
	// the plugin to re-execute the original CLI.
	//
	// Deprecated: use [metadata.ReexecEnvvar]. This alias will be removed in the next release.
	ReexecEnvvar = metadata.ReexecEnvvar
)

// errPluginNotFound is the error returned when a plugin could not be found.
type errPluginNotFound string

func (errPluginNotFound) NotFound() {}

func (e errPluginNotFound) Error() string {
	return "Error: No such CLI plugin: " + string(e)
}

// IsNotFound is true if the given error is due to a plugin not being found.
//
// Deprecated: use [errdefs.IsNotFound].
func IsNotFound(err error) bool {
	return errdefs.IsNotFound(err)
}

// getPluginDirs returns the platform-specific locations to search for plugins
// in order of preference.
//
// Plugin-discovery is performed in the following order of preference:
//
// 1. The "cli-plugins" directory inside the CLIs [config.Path] (usually "~/.docker/cli-plugins").
// 2. Additional plugin directories as configured through [ConfigFile.CLIPluginsExtraDirs].
// 3. Platform-specific defaultSystemPluginDirs.
//
// [ConfigFile.CLIPluginsExtraDirs]: https://pkg.go.dev/github.com/docker/cli@v26.1.4+incompatible/cli/config/configfile#ConfigFile.CLIPluginsExtraDirs
func getPluginDirs(cfg *configfile.ConfigFile) []string {
	var pluginDirs []string

	if cfg != nil {
		pluginDirs = append(pluginDirs, cfg.CLIPluginsExtraDirs...)
	}
	pluginDir := filepath.Join(config.Dir(), "cli-plugins")
	pluginDirs = append(pluginDirs, pluginDir)
	pluginDirs = append(pluginDirs, defaultSystemPluginDirs...)
	return pluginDirs
}

func addPluginCandidatesFromDir(res map[string][]string, d string) {
	dentries, err := os.ReadDir(d)
	// Silently ignore any directories which we cannot list (e.g. due to
	// permissions or anything else) or which is not a directory
	if err != nil {
		return
	}
	for _, dentry := range dentries {
		switch mode := dentry.Type() & os.ModeType; mode { //nolint:exhaustive,nolintlint // no need to include all possible file-modes in this list
		case os.ModeSymlink:
			if !debug.IsEnabled() {
				// Skip broken symlinks unless debug is enabled. With debug
				// enabled, this will print a warning in "docker info".
				if _, err := os.Stat(filepath.Join(d, dentry.Name())); errors.Is(err, os.ErrNotExist) {
					continue
				}
			}
		case 0:
			// Regular file, keep going
		default:
			// Something else, ignore.
			continue
		}
		name := dentry.Name()
		if !strings.HasPrefix(name, metadata.NamePrefix) {
			continue
		}
		name = strings.TrimPrefix(name, metadata.NamePrefix)
		var err error
		if name, err = trimExeSuffix(name); err != nil {
			continue
		}
		res[name] = append(res[name], filepath.Join(d, dentry.Name()))
	}
}

// listPluginCandidates returns a map from plugin name to the list of (unvalidated) Candidates. The list is in descending order of priority.
func listPluginCandidates(dirs []string) map[string][]string {
	result := make(map[string][]string)
	for _, d := range dirs {
		addPluginCandidatesFromDir(result, d)
	}
	return result
}

// GetPlugin returns a plugin on the system by its name
func GetPlugin(name string, dockerCLI config.Provider, rootcmd *cobra.Command) (*Plugin, error) {
	pluginDirs := getPluginDirs(dockerCLI.ConfigFile())
	return getPlugin(name, pluginDirs, rootcmd)
}

func getPlugin(name string, pluginDirs []string, rootcmd *cobra.Command) (*Plugin, error) {
	candidates := listPluginCandidates(pluginDirs)
	if paths, ok := candidates[name]; ok {
		if len(paths) == 0 {
			return nil, errPluginNotFound(name)
		}
		c := &candidate{paths[0]}
		p, err := newPlugin(c, rootcmd.Commands())
		if err != nil {
			return nil, err
		}
		if !errdefs.IsNotFound(p.Err) {
			p.ShadowedPaths = paths[1:]
		}
		return &p, nil
	}

	return nil, errPluginNotFound(name)
}

// ListPlugins produces a list of the plugins available on the system
func ListPlugins(dockerCli config.Provider, rootcmd *cobra.Command) ([]Plugin, error) {
	pluginDirs := getPluginDirs(dockerCli.ConfigFile())
	candidates := listPluginCandidates(pluginDirs)
	if len(candidates) == 0 {
		return nil, nil
	}

	var plugins []Plugin
	var mu sync.Mutex
	ctx := rootcmd.Context()
	if ctx == nil {
		// Fallback, mostly for tests that pass a bare cobra.command
		ctx = context.Background()
	}
	eg, _ := errgroup.WithContext(ctx)
	cmds := rootcmd.Commands()
	for _, paths := range candidates {
		func(paths []string) {
			eg.Go(func() error {
				if len(paths) == 0 {
					return nil
				}
				c := &candidate{paths[0]}
				p, err := newPlugin(c, cmds)
				if err != nil {
					return err
				}
				if !errdefs.IsNotFound(p.Err) {
					p.ShadowedPaths = paths[1:]
					mu.Lock()
					defer mu.Unlock()
					plugins = append(plugins, p)
				}
				return nil
			})
		}(paths)
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	sort.Slice(plugins, func(i, j int) bool {
		return sortorder.NaturalLess(plugins[i].Name, plugins[j].Name)
	})

	return plugins, nil
}

// PluginRunCommand returns an [os/exec.Cmd] which when [os/exec.Cmd.Run] will execute the named plugin.
// The rootcmd argument is referenced to determine the set of builtin commands in order to detect conficts.
// The error returned satisfies the [errdefs.IsNotFound] predicate if no plugin was found or if the first candidate plugin was invalid somehow.
func PluginRunCommand(dockerCli config.Provider, name string, rootcmd *cobra.Command) (*exec.Cmd, error) {
	// This uses the full original args, not the args which may
	// have been provided by cobra to our caller. This is because
	// they lack e.g. global options which we must propagate here.
	args := os.Args[1:]
	if !pluginNameRe.MatchString(name) {
		// We treat this as "not found" so that callers will
		// fallback to their "invalid" command path.
		return nil, errPluginNotFound(name)
	}
	exename := addExeSuffix(metadata.NamePrefix + name)
	pluginDirs := getPluginDirs(dockerCli.ConfigFile())

	for _, d := range pluginDirs {
		path := filepath.Join(d, exename)

		// We stat here rather than letting the exec tell us
		// ENOENT because the latter does not distinguish a
		// file not existing from its dynamic loader or one of
		// its libraries not existing.
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}

		c := &candidate{path: path}
		plugin, err := newPlugin(c, rootcmd.Commands())
		if err != nil {
			return nil, err
		}
		if plugin.Err != nil {
			// TODO: why are we not returning plugin.Err?
			return nil, errPluginNotFound(name)
		}
		cmd := exec.Command(plugin.Path, args...) // #nosec G204 -- ignore "Subprocess launched with a potential tainted input or cmd arguments"

		// Using dockerCli.{In,Out,Err}() here results in a hang until something is input.
		// See: - https://github.com/golang/go/issues/10338
		//      - https://github.com/golang/go/commit/d000e8742a173aa0659584aa01b7ba2834ba28ab
		// os.Stdin is a *os.File which avoids this behaviour. We don't need the functionality
		// of the wrappers here anyway.
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		cmd.Env = append(cmd.Environ(), metadata.ReexecEnvvar+"="+os.Args[0])
		cmd.Env = appendPluginResourceAttributesEnvvar(cmd.Env, rootcmd, plugin)

		return cmd, nil
	}
	return nil, errPluginNotFound(name)
}

// IsPluginCommand checks if the given cmd is a plugin-stub.
func IsPluginCommand(cmd *cobra.Command) bool {
	return cmd.Annotations[metadata.CommandAnnotationPlugin] == "true"
}
