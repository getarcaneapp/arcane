package registries

import (
	"encoding/json"
	"fmt"
	"strconv"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/spf13/cobra"
)

var (
	limitFlag  int
	startFlag  int
	allFlag    bool
	forceFlag  bool
	jsonOutput bool

	registryCreateURL         string
	registryCreateUsername    string
	registryCreateToken       string
	registryCreateDescription string
	registryCreateInsecure    bool
	registryCreateDisabled    bool
	registryCreateType        string
	registryCreateAWSKeyID    string
	registryCreateAWSSecret   string
	registryCreateAWSRegion   string

	registryUpdateURL      string
	registryUpdateUsername string
	registryUpdatePassword string
	registryUpdateEnabled  bool
	registryUpdateDisabled bool
)

var RegistriesCmd = &cobra.Command{
	Use:     "registries",
	Aliases: []string{"registry", "reg"},
	Short:   "Manage container registries",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List container registries",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.ContainerRegistries()
		path, err = cmdutil.ApplyPaginationParams(cmd, path, cmdutil.ListParams{Resource: "registries", Limit: limitFlag, FallbackDefault: 20, Start: startFlag, All: allFlag})
		if err != nil {
			return errors.WrapIf(err, "failed to build pagination query")
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to list registries")
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[containerregistry.ContainerRegistry]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		headers := []string{"ID", "URL", "USERNAME", "ENABLED", "INSECURE"}
		rows := make([][]string, len(result.Data))
		for i, reg := range result.Data {
			enabled := "false"
			if reg.Enabled {
				enabled = "true"
			}
			insecure := "false"
			if reg.Insecure {
				insecure = "true"
			}
			rows[i] = []string{
				reg.ID,
				reg.URL,
				reg.Username,
				enabled,
				insecure,
			}
		}

		output.Table(headers, rows)
		output.Showing(len(result.Data), result.Pagination.TotalItems, "registries")
		return nil
	},
}

var testCmd = &cobra.Command{
	Use:          "test <registry-id>",
	Short:        "Test container registry connection",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.ContainerRegistryTest(args[0]), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to test registry")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to test registry")
		}

		if jsonOutput {
			var result base.ApiResponse[any]
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				if resultBytes, err := json.MarshalIndent(result.Data, "", "  "); err == nil {
					fmt.Println(string(resultBytes))
				}
			}
			return nil
		}

		output.Success("Registry connection test successful")
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:          "get <registry-id>",
	Short:        "Get container registry details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.ContainerRegistry(args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to get registry")
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[containerregistry.ContainerRegistry]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Registry Details")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("URL", result.Data.URL)
		output.KeyValue("Username", result.Data.Username)
		output.KeyValue("Enabled", result.Data.Enabled)
		output.KeyValue("Insecure", result.Data.Insecure)
		output.KeyValue("Created", result.Data.CreatedAt.Format("2006-01-02 15:04"))
		output.KeyValue("Updated", result.Data.UpdatedAt.Format("2006-01-02 15:04"))
		return nil
	},
}

var createCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create a container registry",
	SilenceUsage: true,
	Args:         cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		// No field on CreateContainerRegistryRequest carries omitempty, so Huma
		// marks all of them required — every key has to be present, including
		// description.
		req := map[string]any{
			"url":                registryCreateURL,
			"registryType":       registryCreateType,
			"username":           registryCreateUsername,
			"token":              registryCreateToken,
			"description":        registryCreateDescription,
			"insecure":           registryCreateInsecure,
			"enabled":            !registryCreateDisabled,
			"awsAccessKeyId":     registryCreateAWSKeyID,
			"awsSecretAccessKey": registryCreateAWSSecret,
			"awsRegion":          registryCreateAWSRegion,
		}

		resp, err := c.Post(cmd.Context(), types.ContainerRegistries(), req)
		if err != nil {
			return errors.WrapIf(err, "failed to create registry")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to create registry")
		}

		var result base.ApiResponse[containerregistry.ContainerRegistry]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Success("Registry created successfully")
		output.Header("Registry Details")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("URL", result.Data.URL)
		output.KeyValue("Username", result.Data.Username)
		output.KeyValue("Type", result.Data.RegistryType)
		output.KeyValue("Enabled", result.Data.Enabled)
		output.KeyValue("Insecure", result.Data.Insecure)
		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:          "update <registry-id>",
	Short:        "Update container registry",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		if cmd.Flags().Changed("enabled") && cmd.Flags().Changed("disabled") {
			return errors.New("--enabled and --disabled are mutually exclusive")
		}

		// Every field on UpdateContainerRegistryRequest lacks omitempty, so Huma
		// requires all of them. They are all pointers though, so an explicit null
		// is valid and the service leaves those fields untouched.
		req := map[string]any{
			"url":                nil,
			"username":           nil,
			"token":              nil,
			"description":        nil,
			"insecure":           nil,
			"enabled":            nil,
			"registryType":       nil,
			"awsAccessKeyId":     nil,
			"awsSecretAccessKey": nil,
			"awsRegion":          nil,
		}

		changed := false
		setField := func(key string, value any) {
			req[key] = value
			changed = true
		}
		if cmd.Flags().Changed("url") {
			setField("url", registryUpdateURL)
		}
		if cmd.Flags().Changed("username") {
			setField("username", registryUpdateUsername)
		}
		if cmd.Flags().Changed("password") {
			setField("token", registryUpdatePassword)
		}
		if cmd.Flags().Changed("enabled") {
			setField("enabled", registryUpdateEnabled)
		}
		if cmd.Flags().Changed("disabled") {
			setField("enabled", !registryUpdateDisabled)
		}

		if !changed {
			return errors.New("no updates provided; use --url, --username, --password, --enabled, or --disabled")
		}

		resp, err := c.Put(cmd.Context(), types.ContainerRegistry(args[0]), req)
		if err != nil {
			return errors.WrapIf(err, "failed to update registry")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to update registry")
		}

		if jsonOutput {
			var result base.ApiResponse[any]
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				if resultBytes, err := json.MarshalIndent(result.Data, "", "  "); err == nil {
					fmt.Println(string(resultBytes))
				}
			}
			return nil
		}

		output.Success("Registry updated successfully")
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <registry-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete container registry",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete registry %s?", args[0]))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Delete(cmd.Context(), types.ContainerRegistry(args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to delete registry")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to delete registry")
		}

		output.Success("Registry deleted successfully")
		return nil
	},
}

var usageCmd = &cobra.Command{
	Use:          "usage",
	Aliases:      []string{"pull-usage"},
	Short:        "Show pull usage and rate limits for configured registries",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.ContainerRegistriesPullUsage())
		if err != nil {
			return errors.WrapIf(err, "failed to get registry pull usage")
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[containerregistry.PullUsageResponse]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		if len(result.Data.Registries) == 0 {
			output.Info("No registries configured")
			return nil
		}

		formatCount := func(value *int) string {
			if value == nil {
				return "-"
			}
			return strconv.Itoa(*value)
		}

		headers := []string{"NAME", "REGISTRY", "PROVIDER", "LIMIT", "REMAINING", "USED", "OBSERVED", "CHECKED", "ERROR"}
		rows := make([][]string, len(result.Data.Registries))
		for i, usage := range result.Data.Registries {
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
		output.Table(headers, rows)
		return nil
	},
}

func init() {
	RegistriesCmd.AddCommand(listCmd)
	RegistriesCmd.AddCommand(getCmd)
	RegistriesCmd.AddCommand(createCmd)
	RegistriesCmd.AddCommand(testCmd)
	RegistriesCmd.AddCommand(updateCmd)
	RegistriesCmd.AddCommand(deleteCmd)
	RegistriesCmd.AddCommand(usageCmd)

	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of registries to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
	listCmd.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	testCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	createCmd.Flags().StringVar(&registryCreateURL, "url", "", "Registry URL (e.g. ghcr.io, docker.io, registry.example.com)")
	createCmd.Flags().StringVar(&registryCreateUsername, "username", "", "Username")
	createCmd.Flags().StringVar(&registryCreateToken, "token", "", "Access token or password")
	createCmd.Flags().StringVar(&registryCreateDescription, "description", "", "Description")
	createCmd.Flags().BoolVar(&registryCreateInsecure, "insecure", false, "Allow insecure (HTTP) connections")
	createCmd.Flags().BoolVar(&registryCreateDisabled, "disabled", false, "Create registry in disabled state")
	createCmd.Flags().StringVar(&registryCreateType, "registry-type", "generic", "Registry type: generic or ecr")
	createCmd.Flags().StringVar(&registryCreateAWSKeyID, "aws-access-key-id", "", "AWS Access Key ID (ECR only)")
	createCmd.Flags().StringVar(&registryCreateAWSSecret, "aws-secret-access-key", "", "AWS Secret Access Key (ECR only)")
	createCmd.Flags().StringVar(&registryCreateAWSRegion, "aws-region", "", "AWS region (ECR only)")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = createCmd.MarkFlagRequired("url")

	updateCmd.Flags().StringVar(&registryUpdateURL, "url", "", "Registry URL")
	updateCmd.Flags().StringVar(&registryUpdateUsername, "username", "", "Username")
	updateCmd.Flags().StringVar(&registryUpdatePassword, "password", "", "Password")
	updateCmd.Flags().BoolVar(&registryUpdateEnabled, "enabled", false, "Enable registry")
	updateCmd.Flags().BoolVar(&registryUpdateDisabled, "disabled", false, "Disable registry")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	usageCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
