package registries

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/client"
	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "registries",
		Aliases: []string{"registry", "reg"},
		Short:   "Manage container registries",
	}
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newGetCmd())
	cmd.AddCommand(newCreateCmd())
	cmd.AddCommand(newTestCmd())
	cmd.AddCommand(newUpdateCmd())
	cmd.AddCommand(newDeleteCmd())
	cmd.AddCommand(newUsageCmd())
	return cmd
}

func newListCmd() *cobra.Command {
	var opts struct {
		limit int
		start int
		all   bool
	}
	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List container registries",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.ContainerRegistry]{
				Resource: "registries",
				Endpoint: clitypes.ContainerRegistries(),
				Params: cmdutil.ListParams{
					Resource:        "registries",
					Limit:           opts.limit,
					FallbackDefault: 20,
					Start:           opts.start,
					All:             opts.all,
				},
				Headers: []string{"ID", "URL", "USERNAME", "ENABLED", "INSECURE"},
				Row: func(reg types.ContainerRegistry) []string {
					return []string{
						reg.ID,
						reg.URL,
						reg.Username,
						strconv.FormatBool(reg.Enabled),
						strconv.FormatBool(reg.Insecure),
					}
				},
			})
		},
	}
	cmd.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of registries to show")
	cmd.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	cmd.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	return cmd
}

func newTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "test <registry-id>",
		Short:        "Test container registry connection",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.MessageResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.ContainerRegistryAction(args[0], "test"),
				FailureMessage: "failed to test registry",
				Render: func(types.MessageResponse) error {
					output.Success("%s", "Registry connection test successful")
					return nil
				},
			})
		},
	}
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "get <registry-id>",
		Short:        "Get container registry details",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.ContainerRegistry]{
				Method:         http.MethodGet,
				Path:           clitypes.ContainerRegistry(args[0]),
				FailureMessage: "failed to get registry",
				Render: func(data types.ContainerRegistry) error {
					output.Header("Registry Details")
					output.KeyValue("ID", data.ID)
					output.KeyValue("URL", data.URL)
					output.KeyValue("Username", data.Username)
					output.KeyValue("Enabled", data.Enabled)
					output.KeyValue("Insecure", data.Insecure)
					output.KeyValue("Created", data.CreatedAt.Format("2006-01-02 15:04"))
					output.KeyValue("Updated", data.UpdatedAt.Format("2006-01-02 15:04"))
					return nil
				},
			})
		},
	}
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		url          string
		username     string
		token        string
		description  string
		insecure     bool
		enabled      bool
		registryType string
		awsKeyID     string
		awsSecret    string
		awsRegion    string
	}
	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a container registry",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			req := types.ContainerRegistry{
				URL:                opts.url,
				RegistryType:       opts.registryType,
				Username:           opts.username,
				Token:              opts.token,
				Description:        &opts.description,
				Insecure:           opts.insecure,
				Enabled:            opts.enabled,
				AWSAccessKeyID:     opts.awsKeyID,
				AWSSecretAccessKey: opts.awsSecret,
				AWSRegion:          opts.awsRegion,
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.ContainerRegistry]{
				Method:         http.MethodPost,
				Path:           clitypes.ContainerRegistries(),
				Body:           req,
				FailureMessage: "failed to create registry",
				Render: func(data types.ContainerRegistry) error {
					output.Success("Registry created successfully")
					output.Header("Registry Details")
					output.KeyValue("ID", data.ID)
					output.KeyValue("URL", data.URL)
					output.KeyValue("Username", data.Username)
					output.KeyValue("Type", data.RegistryType)
					output.KeyValue("Enabled", data.Enabled)
					output.KeyValue("Insecure", data.Insecure)
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.url, "url", "", "Registry URL (e.g. ghcr.io, docker.io, registry.example.com)")
	cmd.Flags().StringVar(&opts.username, "username", "", "Username")
	cmd.Flags().StringVar(&opts.token, "token", "", "Access token or password")
	cmd.Flags().StringVar(&opts.description, "description", "", "Description")
	cmd.Flags().BoolVar(&opts.insecure, "insecure", false, "Allow insecure (HTTP) connections")
	cmd.Flags().BoolVar(&opts.enabled, "enabled", true, "Enable registry")
	cmdutil.EnumVarP(cmd, cmd.Flags(), &opts.registryType, "registry-type", "", "generic", "Registry type", "generic", "ecr")
	cmd.Flags().StringVar(&opts.awsKeyID, "aws-access-key-id", "", "AWS Access Key ID (ECR only; omit both keys to use the host's IAM role)")
	cmd.Flags().StringVar(&opts.awsSecret, "aws-secret-access-key", "", "AWS Secret Access Key (ECR only; omit both keys to use the host's IAM role)")
	cmd.Flags().StringVar(&opts.awsRegion, "aws-region", "", "AWS region (ECR only)")
	_ = cmd.MarkFlagRequired("url")
	return cmd
}

func newUpdateCmd() *cobra.Command {
	var opts struct {
		url      string
		username string
		password string
		enabled  bool
	}
	cmd := &cobra.Command{
		Use:          "update <registry-id>",
		Short:        "Update container registry",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			req := map[string]any{}
			for _, field := range []struct {
				changed bool
				key     string
				value   any
			}{
				{changed: cmd.Flags().Changed("url"), key: "url", value: opts.url},
				{changed: cmd.Flags().Changed("username"), key: "username", value: opts.username},
				{changed: cmd.Flags().Changed("password"), key: "token", value: opts.password},
				{changed: cmd.Flags().Changed("enabled"), key: "enabled", value: opts.enabled},
			} {
				if field.changed {
					req[field.key] = field.value
				}
			}

			if len(req) == 0 {
				return errors.New("no updates provided; use --url, --username, --password, or --enabled")
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.ContainerRegistry]{
				Method:         http.MethodPatch,
				Path:           clitypes.ContainerRegistry(args[0]),
				Body:           req,
				FailureMessage: "failed to update registry",
				Render: func(data types.ContainerRegistry) error {
					output.Success("Registry updated successfully")
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.url, "url", "", "Registry URL")
	cmd.Flags().StringVar(&opts.username, "username", "", "Username")
	cmd.Flags().StringVar(&opts.password, "password", "", "Password")
	cmd.Flags().BoolVar(&opts.enabled, "enabled", false, "Enable registry")
	return cmd
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	cmd := &cobra.Command{
		Use:          "delete <registry-id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete container registry",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete registry %s?", args[0]),
				Path:           clitypes.ContainerRegistry(args[0]),
				Body:           nil,
				FailureMessage: "failed to delete registry",
				SuccessMessage: "Registry deleted successfully",
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return cmd
}

func newUsageCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "usage",
		Aliases:      []string{"pull-usage"},
		Short:        "Show pull usage and rate limits for configured registries",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromConfig()
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.ContainerRegistryPullUsageResponse]{
				Method:         http.MethodGet,
				Path:           clitypes.ContainerRegistriesPullUsage(),
				FailureMessage: "failed to get registry pull usage",
				Render: func(data types.ContainerRegistryPullUsageResponse) error {
					if len(data.Registries) == 0 {
						output.Info("No registries configured")
						return nil
					}

					formatCount := func(value *int) string {
						return kit.Ternary(value == nil, "-", strconv.Itoa(kit.FromPtr(value)))
					}

					headers := []string{"NAME", "REGISTRY", "PROVIDER", "LIMIT", "REMAINING", "USED", "OBSERVED", "CHECKED", "ERROR"}
					rows := make([][]string, len(data.Registries))
					for i, usage := range data.Registries {
						rows[i] = []string{
							usage.DisplayName,
							usage.Registry,
							usage.Provider,
							formatCount(usage.Limit),
							formatCount(usage.Remaining),
							formatCount(usage.Used),
							strconv.FormatInt(usage.ObservedPulls, 10),
							usage.CheckedAt.Format("2006-01-02 15:04"),
							usage.Error,
						}
					}
					return output.Table(headers, rows)
				},
			})
		},
	}
}
