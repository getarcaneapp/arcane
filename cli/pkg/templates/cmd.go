package templates

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/output"
	"go.getarcane.app/cli/internal/types"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/env"
	"go.getarcane.app/types/template"
)

var (
	limitFlag  int
	forceFlag  bool
	jsonOutput bool
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
	Short:        "List templates",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.Templates()
		if limitFlag > 0 {
			path = fmt.Sprintf("%s?limit=%d", path, limitFlag)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list templates: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[[]template.Template]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		headers := []string{"ID", "NAME", "DESCRIPTION", "CUSTOM", "REMOTE"}
		rows := make([][]string, len(result.Data))
		for i, tpl := range result.Data {
			custom := "no"
			if tpl.IsCustom {
				custom = "yes"
			}
			remote := "no"
			if tpl.IsRemote {
				remote = "yes"
			}
			rows[i] = []string{
				tpl.ID,
				tpl.Name,
				tpl.Description,
				custom,
				remote,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d templates\n", len(result.Data))
		return nil
	},
}

var allCmd = &cobra.Command{
	Use:          "all",
	Short:        "List all templates (including remote)",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.TemplatesAll())
		if err != nil {
			return fmt.Errorf("failed to list all templates: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[[]template.Template]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		headers := []string{"ID", "NAME", "DESCRIPTION", "CUSTOM", "REMOTE"}
		rows := make([][]string, len(result.Data))
		for i, tpl := range result.Data {
			custom := "no"
			if tpl.IsCustom {
				custom = "yes"
			}
			remote := "no"
			if tpl.IsRemote {
				remote = "yes"
			}
			rows[i] = []string{
				tpl.ID,
				tpl.Name,
				tpl.Description,
				custom,
				remote,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d templates\n", len(result.Data))
		return nil
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

		resp, err := c.Get(cmd.Context(), types.Endpoints.TemplatesDefault())
		if err != nil {
			return fmt.Errorf("failed to get default templates: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[template.DefaultTemplatesResponse]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
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

		resp, err := c.Get(cmd.Context(), types.Endpoints.TemplateContent(args[0]))
		if err != nil {
			return fmt.Errorf("failed to get template content: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[template.TemplateContent]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Template Content")
		output.KeyValue("Name", result.Data.Template.Name)
		output.KeyValue("Description", result.Data.Template.Description)
		output.KeyValue("Services", fmt.Sprintf("%d", len(result.Data.Services)))
		output.KeyValue("Env Variables", fmt.Sprintf("%d", len(result.Data.EnvVariables)))
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

		resp, err := c.Get(cmd.Context(), types.Endpoints.TemplatesRegistries())
		if err != nil {
			return fmt.Errorf("failed to list registries: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[[]template.TemplateRegistry]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
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
	Short:        "List template variables",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.TemplatesVariables())
		if err != nil {
			return fmt.Errorf("failed to list variables: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[[]env.Variable]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		headers := []string{"KEY", "VALUE"}
		rows := make([][]string, len(result.Data))
		for i, v := range result.Data {
			rows[i] = []string{
				v.Key,
				v.Value,
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
			fmt.Printf("Are you sure you want to delete template %s? (y/N): ", args[0])
			var response string
			if _, err := fmt.Scanln(&response); err != nil {
				fmt.Println("Cancelled")
				return nil
			}
			if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
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
			return fmt.Errorf("failed to delete template: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

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
			fmt.Printf("Are you sure you want to delete template registry %s? (y/N): ", args[0])
			var response string
			if _, err := fmt.Scanln(&response); err != nil {
				fmt.Println("Cancelled")
				return nil
			}
			if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
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
			return fmt.Errorf("failed to delete registry: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		output.Success("Template registry deleted successfully")
		return nil
	},
}

func init() {
	TemplatesCmd.AddCommand(listCmd)
	TemplatesCmd.AddCommand(allCmd)
	TemplatesCmd.AddCommand(defaultCmd)
	TemplatesCmd.AddCommand(contentCmd)
	TemplatesCmd.AddCommand(registriesCmd)
	TemplatesCmd.AddCommand(variablesCmd)
	TemplatesCmd.AddCommand(deleteCmd)
	TemplatesCmd.AddCommand(deleteRegistryCmd)

	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of templates to show")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	allCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	defaultCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	contentCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	registriesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	variablesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteRegistryCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteRegistryCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
