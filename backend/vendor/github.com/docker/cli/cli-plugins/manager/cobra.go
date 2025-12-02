package manager

import (
	"fmt"
	"os"
	"sync"

	"github.com/docker/cli/cli-plugins/metadata"
	"github.com/docker/cli/cli/config"
	"github.com/spf13/cobra"
)

var pluginCommandStubsOnce sync.Once

// AddPluginCommandStubs adds a stub cobra.Commands for each valid and invalid
// plugin. The command stubs will have several annotations added, see
// `CommandAnnotationPlugin*`.
func AddPluginCommandStubs(dockerCLI config.Provider, rootCmd *cobra.Command) (err error) {
	pluginCommandStubsOnce.Do(func() {
		var plugins []Plugin
		plugins, err = ListPlugins(dockerCLI, rootCmd)
		if err != nil {
			return
		}
		for _, p := range plugins {
			vendor := p.Vendor
			if vendor == "" {
				vendor = "unknown"
			}
			annotations := map[string]string{
				metadata.CommandAnnotationPlugin:        "true",
				metadata.CommandAnnotationPluginVendor:  vendor,
				metadata.CommandAnnotationPluginVersion: p.Version,
			}
			if p.Err != nil {
				annotations[metadata.CommandAnnotationPluginInvalid] = p.Err.Error()
			}
			rootCmd.AddCommand(&cobra.Command{
				Use:                p.Name,
				Short:              p.ShortDescription,
				Hidden:             p.Hidden,
				Run:                func(_ *cobra.Command, _ []string) {},
				Annotations:        annotations,
				DisableFlagParsing: true,
				RunE: func(cmd *cobra.Command, args []string) error {
					flags := rootCmd.PersistentFlags()
					flags.SetOutput(nil)
					perr := flags.Parse(args)
					if perr != nil {
						return err
					}
					if flags.Changed("help") {
						cmd.HelpFunc()(rootCmd, args)
						return nil
					}
					return fmt.Errorf("docker: unknown command: docker %s\n\nRun 'docker --help' for more information", cmd.Name())
				},
				ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
					// Delegate completion to plugin
					cargs := []string{p.Path, cobra.ShellCompRequestCmd, p.Name}
					cargs = append(cargs, args...)
					cargs = append(cargs, toComplete)
					os.Args = cargs
					runCommand, runErr := PluginRunCommand(dockerCLI, p.Name, cmd)
					if runErr != nil {
						return nil, cobra.ShellCompDirectiveError
					}
					runErr = runCommand.Run()
					if runErr == nil {
						os.Exit(0) // plugin already rendered complete data
					}
					return nil, cobra.ShellCompDirectiveError
				},
			})
		}
	})
	return err
}
