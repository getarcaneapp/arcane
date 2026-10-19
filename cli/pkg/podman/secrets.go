package podman

import (
	"cmp"
	"fmt"
	"net/http"
	"os"

	"github.com/getarcaneapp/arcane/types/v2/podman"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

func newSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "secrets",
		Aliases: []string{"secret"},
		Short:   "Manage Podman secrets",
	}
	cmd.AddCommand(
		newSecretListCmd(),
		newSecretCreateCmd(),
		newSecretDeleteCmd(),
	)
	return cmd
}

func newSecretListCmd() *cobra.Command {
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

			return cmdutil.Run(cmd, c, cmdutil.Spec[[]podman.Secret]{
				Method:         http.MethodGet,
				Path:           types.PodmanSecrets(c.EnvID()),
				FailureMessage: "failed to list Podman secrets",
				Render: func(data []podman.Secret) error {
					rows := make([][]string, len(data))
					for i, secret := range data {
						rows[i] = []string{
							secret.ID[:min(len(secret.ID), 12)],
							secret.Spec.Name,
							secret.Spec.Driver.Name,
							secret.CreatedAt.Format("2006-01-02 15:04"),
						}
					}
					return cmdutil.PrintTable(cmd, []string{"ID", "NAME", "DRIVER", "CREATED"}, rows, cmdutil.TableSummary{
						Resource: "secrets",
						Total:    int64(len(data)),
					})
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

			return cmdutil.Run(cmd, c, cmdutil.Spec[podman.SecretCreateResponse]{
				Method: http.MethodPost,
				Path:   types.PodmanSecrets(c.EnvID()),
				Body: podman.SecretCreateRequest{
					Name:   args[0],
					Data:   data,
					Driver: opts.driver,
					Labels: opts.labels,
				},
				FailureMessage: "failed to create Podman secret",
				Render: func(created podman.SecretCreateResponse) error {
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
				Path:           types.PodmanSecret(c.EnvID(), args[0]),
				FailureMessage: "failed to delete Podman secret",
				SuccessMessage: fmt.Sprintf("Secret %s deleted successfully", args[0]),
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return cmd
}
