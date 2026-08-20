package templates

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/env"
	"github.com/getarcaneapp/arcane/types/v2/template"
	"github.com/spf13/cobra"
)

var (
	limitFlag       int
	startFlag       int
	templateListAll bool
	forceFlag       bool
	jsonOutput      bool

	templateUpdateName        string
	templateUpdateFile        string
	templateUpdateDescription string
	templateUpdateEnvFile     string
	templateCreateName        string
	templateCreateFile        string
	templateCreateDescription string
	templateCreateEnvFile     string
	templateDownloadOutput    string
	templateDefaultsSaveFile  string
	templateDefaultsEnvFile   string
	templateFetchURL          string
	templateVarsUpdateKey     string
	templateVarsUpdateValue   string
	templateVarsUpdateSecret  bool
	templateVarsUpdateAllEnvs bool
	templateVarsUpdateEnvIDs  []string
	templateRegUpdateName     string
	templateRegUpdateURL      string
	templateRegUpdateDesc     string
	templateRegUpdateEnabled  bool
	templateRegUpdateDisabled bool
)

// TemplatesCmd is the parent command for template operations
var TemplatesCmd = &cobra.Command{
	Use:     "templates",
	Aliases: []string{"template", "tpl"},
	Short:   "Manage Docker Compose templates",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List local templates",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		headers := []string{"NAME", "CUSTOM", "REMOTE", "DESCRIPTION"}
		row := func(tpl template.Template) []string {
			custom := "no"
			if tpl.IsCustom {
				custom = "yes"
			}
			remote := "no"
			if tpl.IsRemote {
				remote = "yes"
			}
			return []string{
				tpl.Name,
				custom,
				remote,
				tpl.Description,
			}
		}

		if templateListAll {
			if cmd.Flags().Changed("limit") || cmd.Flags().Changed("start") {
				return errors.New("--all cannot be combined with explicit pagination flags")
			}

			result, err := c.GetJSON[[]template.Template](cmd.Context(), types.Endpoints.TemplatesAll())
			if err != nil {
				return errors.WrapIf(err, "failed to list templates")
			}

			if jsonOutput {
				return cmdutil.PrintJSON(result.Data)
			}

			rows := make([][]string, len(result.Data))
			for i, tpl := range result.Data {
				rows[i] = row(tpl)
			}
			output.Table(headers, rows)
			output.Showing(len(result.Data), int64(len(result.Data)), "templates")
			return nil
		}

		return cmdutil.RunList(cmd, c, cmdutil.ListSpec[template.Template]{
			Resource: "templates",
			Endpoint: types.Endpoints.Templates(),
			Params:   cmdutil.ListParams{Resource: "templates", Limit: limitFlag, FallbackDefault: 20, Start: startFlag},
			JSON:     jsonOutput,
			Headers:  headers,
			Row:      row,
		})
	},
}

var defaultCmd = &cobra.Command{
	Use:          "default",
	Short:        "Get default templates",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		result, err := c.GetJSON[template.DefaultTemplatesResponse](cmd.Context(), types.Endpoints.TemplatesDefault())
		if err != nil {
			return errors.WrapIf(err, "failed to get default templates")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("Default Templates")
		output.KeyValue("Compose Template", fmt.Sprintf("%d bytes", len(result.Data.ComposeTemplate)))
		output.KeyValue("Env Template", fmt.Sprintf("%d bytes", len(result.Data.EnvTemplate)))
		return nil
	},
}

var contentCmd = &cobra.Command{
	Use:          "content <template-id>",
	Short:        "Get template content",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		result, err := c.GetJSON[template.TemplateContent](cmd.Context(), types.Endpoints.TemplateContent(args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to get template content")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("Template Content")
		output.KeyValue("Name", result.Data.Template.Name)
		output.KeyValue("Description", result.Data.Template.Description)
		output.KeyValue("Services", strconv.Itoa(len(result.Data.Services)))
		output.KeyValue("Env Variables", strconv.Itoa(len(result.Data.EnvVariables)))
		fmt.Println("\n--- Compose Content ---")
		fmt.Println(result.Data.Content)
		if result.Data.EnvContent != "" {
			fmt.Println("\n--- Environment Content ---")
			fmt.Println(result.Data.EnvContent)
		}
		return nil
	},
}

var registriesCmd = &cobra.Command{
	Use:          "registries",
	Aliases:      []string{"reg"},
	Short:        "List template registries",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		result, err := c.GetJSON[[]template.TemplateRegistry](cmd.Context(), types.Endpoints.TemplatesRegistries())
		if err != nil {
			return errors.WrapIf(err, "failed to list registries")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		headers := []string{"ID", "NAME", "URL", "ENABLED"}
		rows := make([][]string, len(result.Data))
		for i, reg := range result.Data {
			enabled := "no"
			if reg.Enabled {
				enabled = "yes"
			}
			rows[i] = []string{
				reg.ID,
				reg.Name,
				reg.URL,
				enabled,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d registries\n", len(result.Data))
		return nil
	},
}

var variablesCmd = &cobra.Command{
	Use:          "variables",
	Aliases:      []string{"vars"},
	Short:        "List global variables",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		result, err := c.GetJSON[[]env.GlobalVariable](cmd.Context(), types.Endpoints.TemplatesVariables())
		if err != nil {
			return errors.WrapIf(err, "failed to list variables")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		headers := []string{"ID", "KEY", "VALUE", "SECRET", "SCOPE"}
		rows := make([][]string, len(result.Data))
		for i, v := range result.Data {
			scope := "all environments"
			if !v.AllEnvironments {
				scope = strings.Join(v.EnvironmentIDs, ", ")
			}
			rows[i] = []string{
				v.ID,
				v.Key,
				v.Value,
				strconv.FormatBool(v.IsSecret),
				scope,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d variables\n", len(result.Data))
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <template-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete template",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete template %s?", args[0]))
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

		resp, err := c.Delete(cmd.Context(), types.Endpoints.Template(args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to delete template")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to delete template")
		}

		output.Success("Template deleted successfully")
		return nil
	},
}

var deleteRegistryCmd = &cobra.Command{
	Use:          "delete-registry <registry-id>",
	Short:        "Delete template registry",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete template registry %s?", args[0]))
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

		resp, err := c.Delete(cmd.Context(), types.Endpoints.TemplateRegistry(args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to delete registry")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to delete registry")
		}

		output.Success("Template registry deleted successfully")
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:          "get <template>",
	Short:        "Get a template by ID or name",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolved, _, err := templateRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(resolved)
		}

		tpl := *resolved
		output.Header("Template")
		output.KeyValue("ID", tpl.ID)
		output.KeyValue("Name", tpl.Name)
		output.KeyValue("Description", tpl.Description)
		custom := "no"
		if tpl.IsCustom {
			custom = "yes"
		}
		remote := "no"
		if tpl.IsRemote {
			remote = "yes"
		}
		output.KeyValue("Custom", custom)
		output.KeyValue("Remote", remote)
		return nil
	},
}

var templateRef = cmdutil.ResourceRef[template.Template, template.Template]{
	Singular: "template",
	Plural:   "templates",
	IDHint:   "the template ID",
	ListCmd:  "arcane templates list",
	GetPath:  func(_, identifier string) string { return types.Endpoints.Template(identifier) },
	ListPath: func(string) string { return types.Endpoints.Templates() },
	Matches:  templateMatches,
	Label: func(match template.Template) string {
		source := "local"
		if match.IsRemote {
			source = "remote"
		}
		custom := "builtin"
		if match.IsCustom {
			custom = "custom"
		}
		return fmt.Sprintf("%s (id: %s, %s, %s)", match.Name, match.ID, source, custom)
	},
	Promote: func(match template.Template) *template.Template { return &match },
}

func templateMatches(item template.Template, identifierLower, original string) bool {
	if strings.EqualFold(item.ID, original) {
		return true
	}
	if strings.EqualFold(item.Name, original) {
		return true
	}
	if strings.Contains(strings.ToLower(item.Name), identifierLower) {
		return true
	}
	return strings.HasPrefix(strings.ToLower(item.ID), identifierLower)
}

var createCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create a new template",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		content, err := os.ReadFile(templateCreateFile)
		if err != nil {
			return errors.WrapIff(err, "failed to read file %s", templateCreateFile)
		}

		req := template.CreateRequest{
			Name:        templateCreateName,
			Description: templateCreateDescription,
			Content:     string(content),
		}

		if templateCreateEnvFile != "" {
			envContent, err := os.ReadFile(templateCreateEnvFile)
			if err != nil {
				return errors.WrapIff(err, "failed to read env file %s", templateCreateEnvFile)
			}
			req.EnvContent = string(envContent)
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		result, err := c.PostJSON[template.Template](cmd.Context(), types.Endpoints.Templates(), req)
		if err != nil {
			return errors.WrapIf(err, "failed to create template")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Template created successfully")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:          "update <template-id>",
	Short:        "Update a template",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		// UpdateRequest has no omitempty fields and the handler replaces the whole
		// record, so an unset --file would otherwise wipe the compose content.
		// Start from the stored template and overlay only what was passed.
		current, err := fetchTemplateInternal(cmd, c, args[0])
		if err != nil {
			return err
		}

		req := template.UpdateRequest{
			Name:        current.Name,
			Description: current.Description,
			Content:     current.Content,
		}
		if current.EnvContent != nil {
			req.EnvContent = *current.EnvContent
		}
		if cmd.Flags().Changed("name") {
			req.Name = templateUpdateName
		}
		if cmd.Flags().Changed("description") {
			req.Description = templateUpdateDescription
		}

		if templateUpdateFile != "" {
			content, err := os.ReadFile(templateUpdateFile)
			if err != nil {
				return errors.WrapIff(err, "failed to read file %s", templateUpdateFile)
			}
			req.Content = string(content)
		}

		if templateUpdateEnvFile != "" {
			envContent, err := os.ReadFile(templateUpdateEnvFile)
			if err != nil {
				return errors.WrapIff(err, "failed to read env file %s", templateUpdateEnvFile)
			}
			req.EnvContent = string(envContent)
		}

		result, err := c.PutJSON[template.Template](cmd.Context(), types.Endpoints.Template(args[0]), req)
		if err != nil {
			return errors.WrapIf(err, "failed to update template")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Template updated successfully")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		return nil
	},
}

var downloadCmd = &cobra.Command{
	Use:          "download <template-id>",
	Short:        "Download template compose file",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		result, err := c.PostJSON[template.Template](cmd.Context(), types.Endpoints.TemplateDownload(args[0]), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to download template")
		}

		if templateDownloadOutput != "" {
			dir := filepath.Dir(templateDownloadOutput)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return errors.WrapIff(err, "failed to create directory %s", dir)
			}
			if err := os.WriteFile(templateDownloadOutput, []byte(result.Data.Content), 0o600); err != nil {
				return errors.WrapIff(err, "failed to write file %s", templateDownloadOutput)
			}
			output.Success("Template downloaded to %s", templateDownloadOutput)
			return nil
		}

		fmt.Print(result.Data.Content)
		return nil
	},
}

var defaultsSaveCmd = &cobra.Command{
	Use:          "defaults-save",
	Short:        "Save default templates",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		content, err := os.ReadFile(templateDefaultsSaveFile)
		if err != nil {
			return errors.WrapIff(err, "failed to read file %s", templateDefaultsSaveFile)
		}

		req := template.SaveDefaultTemplatesRequest{
			ComposeContent: string(content),
		}

		if templateDefaultsEnvFile != "" {
			envContent, err := os.ReadFile(templateDefaultsEnvFile)
			if err != nil {
				return errors.WrapIff(err, "failed to read env file %s", templateDefaultsEnvFile)
			}
			req.EnvContent = string(envContent)
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		return cmdutil.RunPostAction[base.MessageResponse](cmd, c, cmdutil.PostActionSpec{
			Path:           types.Endpoints.TemplatesDefault(),
			Body:           req,
			FailureMessage: "failed to save default templates",
			SuccessMessage: "Default templates saved successfully",
			JSON:           jsonOutput,
		})
	},
}

var variablesUpdateCmd = &cobra.Command{
	Use:          "variables-update <variable-id>",
	Aliases:      []string{"vars-update"},
	Short:        "Update a global variable",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Only the flags that were set are sent; every field on the request is a
		// pointer, and the server keeps the current value for the ones omitted.
		var req env.UpdateGlobalVariableRequest
		if cmd.Flags().Changed("key") {
			req.Key = &templateVarsUpdateKey
		}
		if cmd.Flags().Changed("value") {
			req.Value = &templateVarsUpdateValue
		}
		if cmd.Flags().Changed("secret") {
			req.IsSecret = &templateVarsUpdateSecret
		}
		if cmd.Flags().Changed("all-environments") {
			req.AllEnvironments = &templateVarsUpdateAllEnvs
		}
		if cmd.Flags().Changed("environment") {
			req.EnvironmentIDs = &templateVarsUpdateEnvIDs
		}
		if req.Key == nil && req.Value == nil && req.IsSecret == nil && req.AllEnvironments == nil && req.EnvironmentIDs == nil {
			return errors.New("no changes requested; pass at least one of --key, --value, --secret, --all-environments or --environment")
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		result, err := c.PutJSON[env.GlobalVariableMutationResponse](cmd.Context(), types.Endpoints.Variable(args[0]), req)
		if err != nil {
			return errors.WrapIf(err, "failed to update variable")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Variable updated successfully")
		return nil
	},
}

var registriesUpdateCmd = &cobra.Command{
	Use:          "registries-update <registry-id>",
	Short:        "Update a template registry",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if templateRegUpdateEnabled && templateRegUpdateDisabled {
			return errors.New("--enabled and --disabled cannot be used together")
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		// Every field on UpdateRegistryRequest is required and the handler does a
		// full replace, so start from the current registry and overlay only the
		// flags that were actually passed.
		current, err := fetchTemplateRegistryInternal(cmd, c, args[0])
		if err != nil {
			return err
		}

		req := template.UpdateRegistryRequest{
			Name:        current.Name,
			URL:         current.URL,
			Description: current.Description,
			Enabled:     current.Enabled,
		}
		if cmd.Flags().Changed("name") {
			req.Name = templateRegUpdateName
		}
		if cmd.Flags().Changed("url") {
			req.URL = templateRegUpdateURL
		}
		if cmd.Flags().Changed("description") {
			req.Description = templateRegUpdateDesc
		}
		if templateRegUpdateEnabled {
			req.Enabled = true
		}
		if templateRegUpdateDisabled {
			req.Enabled = false
		}

		// The handler answers with a plain message, not the updated registry.
		result, err := c.PutJSON[base.MessageResponse](cmd.Context(), types.Endpoints.TemplateRegistry(args[0]), req)
		if err != nil {
			return errors.WrapIf(err, "failed to update registry")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Template registry updated successfully")
		output.KeyValue("ID", current.ID)
		output.KeyValue("Name", req.Name)
		return nil
	},
}

// fetchTemplateInternal loads a single template, including its content.
func fetchTemplateInternal(cmd *cobra.Command, c *client.Client, id string) (template.Template, error) {
	result, err := c.GetJSON[template.Template](cmd.Context(), types.Endpoints.Template(id))
	if err != nil {
		return template.Template{}, errors.WrapIf(err, "failed to load template")
	}
	return result.Data, nil
}

// fetchTemplateRegistryInternal looks a registry up by ID. The API exposes no
// get-by-id route for template registries, so this filters the list.
func fetchTemplateRegistryInternal(cmd *cobra.Command, c *client.Client, id string) (template.TemplateRegistry, error) {
	listed, err := c.GetJSON[[]template.TemplateRegistry](cmd.Context(), types.Endpoints.TemplatesRegistries())
	if err != nil {
		return template.TemplateRegistry{}, errors.WrapIf(err, "failed to load registry")
	}

	for _, registry := range listed.Data {
		if registry.ID == id {
			return registry, nil
		}
	}
	return template.TemplateRegistry{}, errors.Errorf("template registry %q not found", id)
}

var fetchCmd = &cobra.Command{
	Use:          "fetch",
	Short:        "Fetch remote template registries",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := cmdutil.AppendQuery(types.Endpoints.TemplateFetch(), url.Values{"url": []string{templateFetchURL}})
		result, err := c.GetJSON[template.RemoteRegistry](cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to fetch templates")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Remote templates fetched successfully")
		return nil
	},
}

func init() {
	TemplatesCmd.AddCommand(listCmd)
	TemplatesCmd.AddCommand(defaultCmd)
	TemplatesCmd.AddCommand(contentCmd)
	TemplatesCmd.AddCommand(registriesCmd)
	TemplatesCmd.AddCommand(variablesCmd)
	TemplatesCmd.AddCommand(deleteCmd)
	TemplatesCmd.AddCommand(deleteRegistryCmd)
	TemplatesCmd.AddCommand(getCmd)
	TemplatesCmd.AddCommand(createCmd)
	TemplatesCmd.AddCommand(updateCmd)
	TemplatesCmd.AddCommand(downloadCmd)
	TemplatesCmd.AddCommand(defaultsSaveCmd)
	TemplatesCmd.AddCommand(variablesUpdateCmd)
	TemplatesCmd.AddCommand(registriesUpdateCmd)
	TemplatesCmd.AddCommand(fetchCmd)

	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of templates to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, "Offset for pagination")
	listCmd.Flags().BoolVarP(&templateListAll, "all", "a", false, "List all templates (including remote)")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	defaultCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	contentCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	registriesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	variablesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteRegistryCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteRegistryCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// get command flags
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// create command flags
	createCmd.Flags().StringVar(&templateCreateName, "name", "", "Template name")
	createCmd.Flags().StringVar(&templateCreateFile, "file", "", "Path to Docker Compose file")
	createCmd.Flags().StringVar(&templateCreateDescription, "description", "", "Template description")
	createCmd.Flags().StringVar(&templateCreateEnvFile, "env-file", "", "Path to environment file")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = createCmd.MarkFlagRequired("name")
	_ = createCmd.MarkFlagRequired("file")

	// update command flags
	updateCmd.Flags().StringVar(&templateUpdateName, "name", "", "Template name")
	updateCmd.Flags().StringVar(&templateUpdateFile, "file", "", "Path to Docker Compose file")
	updateCmd.Flags().StringVar(&templateUpdateDescription, "description", "", "Template description")
	updateCmd.Flags().StringVar(&templateUpdateEnvFile, "env-file", "", "Path to environment file")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// download command flags
	downloadCmd.Flags().StringVarP(&templateDownloadOutput, "output", "o", "", "Output file path")
	downloadCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// defaults-save command flags
	defaultsSaveCmd.Flags().StringVar(&templateDefaultsSaveFile, "file", "", "Path to compose content file")
	defaultsSaveCmd.Flags().StringVar(&templateDefaultsEnvFile, "env-file", "", "Path to environment content file")
	defaultsSaveCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = defaultsSaveCmd.MarkFlagRequired("file")

	// variables-update command flags
	variablesUpdateCmd.Flags().StringVar(&templateVarsUpdateKey, "key", "", "New variable key")
	variablesUpdateCmd.Flags().StringVar(&templateVarsUpdateValue, "value", "", "New variable value")
	variablesUpdateCmd.Flags().BoolVar(&templateVarsUpdateSecret, "secret", false, "Mark the variable as a secret")
	variablesUpdateCmd.Flags().BoolVar(&templateVarsUpdateAllEnvs, "all-environments", false, "Scope the variable to all environments")
	variablesUpdateCmd.Flags().StringSliceVar(&templateVarsUpdateEnvIDs, "environment", nil, "Environment IDs to scope the variable to (repeatable)")
	variablesUpdateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = variablesUpdateCmd.MarkFlagRequired("file")

	// registries-update command flags
	registriesUpdateCmd.Flags().StringVar(&templateRegUpdateName, "name", "", "Registry name")
	registriesUpdateCmd.Flags().StringVar(&templateRegUpdateURL, "url", "", "Registry URL")
	registriesUpdateCmd.Flags().StringVar(&templateRegUpdateDesc, "description", "", "Registry description")
	registriesUpdateCmd.Flags().BoolVar(&templateRegUpdateEnabled, "enabled", false, "Enable registry")
	registriesUpdateCmd.Flags().BoolVar(&templateRegUpdateDisabled, "disabled", false, "Disable registry")
	registriesUpdateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// fetch command flags
	fetchCmd.Flags().StringVar(&templateFetchURL, "url", "", "Registry URL to fetch templates from (required)")
	_ = fetchCmd.MarkFlagRequired("url")
	fetchCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
