package podman

import (
	"cmp"
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	internaltypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func newSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "secrets",
		Aliases: []string{"secret"},
		Short:   "Manage Podman secrets",
	}
	cmd.AddCommand(
		newSecretListCmd(),
		newSecretInspectCmd(),
		newSecretCreateCmd(),
		newSecretUpdateCmd(),
		newSecretDeleteCmd(),
	)
	return cmd
}

func newSecretListCmd() *cobra.Command {
	var opts struct {
		limit int
		start int
		all   bool
	}
	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List Podman secrets",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.PodmanSecret]{
				Resource: "secrets",
				Endpoint: internaltypes.PodmanSecrets(c.EnvID()),
				Params: cmdutil.ListParams{
					Resource:        "secrets",
					Limit:           opts.limit,
					FallbackDefault: 20,
					Start:           opts.start,
					All:             opts.all,
				},
				Headers: []string{"ID", "NAME", "DRIVER", "CREATED"},
				Row: func(secret types.PodmanSecret) []string {
					return []string{
						secret.ID[:min(len(secret.ID), 12)],
						secret.Spec.Name,
						secret.Spec.Driver.Name,
						secret.CreatedAt.Format("2006-01-02 15:04"),
					}
				},
			})
		},
	}
	cmd.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of secrets to show")
	cmd.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	cmd.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	return cmd
}

func newSecretInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "inspect <secret>",
		Aliases:      []string{"get"},
		Short:        "Show Podman secret metadata",
		Long:         "Show a Podman secret's metadata. The secret value is never returned.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.PodmanSecret]{
				Method:         http.MethodGet,
				Path:           internaltypes.PodmanSecret(c.EnvID(), args[0]),
				FailureMessage: "failed to inspect Podman secret",
				Render: func(data types.PodmanSecret) error {
					output.Header("Secret Details")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Name", data.Spec.Name)
					output.KeyValue("Driver", cmp.Or(data.Spec.Driver.Name, "-"))
					output.KeyValue("Created", data.CreatedAt.Format("2006-01-02 15:04"))
					output.KeyValue("Updated", data.UpdatedAt.Format("2006-01-02 15:04"))
					for _, key := range slices.Sorted(maps.Keys(data.Spec.Labels)) {
						output.KeyValue("Label "+key, data.Spec.Labels[key])
					}
					return nil
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	return cmd
}

func newSecretCreateCmd() *cobra.Command {
	var opts struct {
		file   string
		driver string
		labels map[string]string
	}
	cmd := &cobra.Command{
		Use:          "create <name>",
		Short:        "Create a Podman secret",
		Long:         "Create a Podman secret from --file, or from a hidden prompt (or one line of stdin) when --file is omitted.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := func() (string, error) {
				if opts.file == "" {
					return output.Secret(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), "Secret value", "file")
				}
				content, readErr := os.ReadFile(opts.file)
				return string(content), readErr
			}()
			if err != nil {
				return fmt.Errorf("failed to read secret value: %w", err)
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.PodmanSecretCreateResponse]{
				Method: http.MethodPost,
				Path:   internaltypes.PodmanSecrets(c.EnvID()),
				Body: types.PodmanSecretCreateRequest{
					Name:   args[0],
					Data:   data,
					Driver: opts.driver,
					Labels: opts.labels,
				},
				FailureMessage: "failed to create Podman secret",
				Render: func(created types.PodmanSecretCreateResponse) error {
					output.Success("Secret %s created successfully", args[0])
					output.KeyValue("ID", cmp.Or(created.ID, "-"))
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.file, "file", "", "Read the secret value from this file")
	cmd.Flags().StringVar(&opts.driver, "driver", "", "Secret driver (default file)")
	cmd.Flags().StringToStringVar(&opts.labels, "label", nil, "Label (KEY=VALUE)")
	cmdutil.SetScope(cmdutil.ScopeDestination, cmd)
	return cmd
}

func newSecretUpdateCmd() *cobra.Command {
	var opts struct {
		file   string
		driver string
		labels map[string]string
	}
	cmd := &cobra.Command{
		Use:     "update <name>",
		Aliases: []string{"replace"},
		Short:   "Replace a Podman secret's value",
		Long: "Replace a Podman secret's value from --file, or from a hidden prompt (or one line of stdin) when --file is omitted. " +
			"Podman replaces the whole secret, so pass --driver and --label again to keep them.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := func() (string, error) {
				if opts.file == "" {
					return output.Secret(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), "New secret value", "file")
				}
				content, readErr := os.ReadFile(opts.file)
				return string(content), readErr
			}()
			if err != nil {
				return fmt.Errorf("failed to read secret value: %w", err)
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.PodmanSecret]{
				Method: http.MethodPut,
				Path:   internaltypes.PodmanSecret(c.EnvID(), args[0]),
				Body: types.PodmanSecretUpdateRequest{
					Data:   data,
					Driver: opts.driver,
					Labels: opts.labels,
				},
				FailureMessage: "failed to update Podman secret",
				Render: func(updated types.PodmanSecret) error {
					output.Success("Secret %s updated successfully", args[0])
					output.KeyValue("ID", cmp.Or(updated.ID, "-"))
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.file, "file", "", "Read the new secret value from this file")
	cmd.Flags().StringVar(&opts.driver, "driver", "", "Secret driver (default file)")
	cmd.Flags().StringToStringVar(&opts.labels, "label", nil, "Label (KEY=VALUE)")
	cmdutil.SetScope(cmdutil.ScopeDestination, cmd)
	return cmd
}

func newSecretDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	cmd := &cobra.Command{
		Use:          "delete <secret>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete a Podman secret",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete secret %s?", args[0]),
				Path:           internaltypes.PodmanSecret(c.EnvID(), args[0]),
				FailureMessage: "failed to delete Podman secret",
				SuccessMessage: fmt.Sprintf("Secret %s deleted successfully", args[0]),
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return cmd
}
