package environments

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/internal/config"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/environment"
	"github.com/spf13/cobra"
)

var (
	limitFlag  int
	forceFlag  bool
	jsonOutput bool
)

var (
	envUpdateName     string
	envUpdateApiUrl   string
	envUpdateEnabled  bool
	envUpdateDisabled bool
)

var (
	statusOnlineStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	statusOfflineStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))
	statusMutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8"))
	enabledStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#6d28d9"))
)

// EnvironmentsCmd is the parent command for environment operations
var EnvironmentsCmd = &cobra.Command{
	Use:     "environments",
	Aliases: []string{"environment", "env", "e"},
	Short:   "Manage environments",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List environments",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.Environments()
		effectiveLimit := cmdutil.EffectiveLimit(cmd, "environments", "limit", limitFlag, 20)
		if effectiveLimit > 0 {
			path = fmt.Sprintf("%s?limit=%d", path, effectiveLimit)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list environments: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[environment.Environment]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		headers := []string{"ID", "NAME", "API URL", "STATUS", "ENABLED"}
		rows := make([][]string, len(result.Data))
		for i, env := range result.Data {
			enabled := "false"
			if env.Enabled {
				enabled = "true"
			}
			rows[i] = []string{
				env.ID,
				env.Name,
				env.ApiUrl,
				env.Status,
				enabled,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d environments\n", result.Pagination.TotalItems)
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete environment",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete environment %s?", args[0]))
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

		resp, err := c.Delete(cmd.Context(), types.Endpoints.Environment(args[0]))
		if err != nil {
			return fmt.Errorf("failed to delete environment: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to delete environment: %w", err)
		}

		output.Success("Environment deleted successfully")
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:          "get <id>",
	Short:        "Get environment details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.Environment(args[0]))
		if err != nil {
			return fmt.Errorf("failed to get environment: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[environment.Environment]
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

		output.Header("Environment Details")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		output.KeyValue("API URL", result.Data.ApiUrl)
		output.KeyValue("Status", result.Data.Status)
		output.KeyValue("Enabled", result.Data.Enabled)
		return nil
	},
}

var testCmd = &cobra.Command{
	Use:          "test <id>",
	Short:        "Test environment connection",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.EnvironmentTest(args[0]), nil)
		if err != nil {
			return fmt.Errorf("failed to test environment: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to test environment: %w", err)
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

		output.Success("Environment connection test successful")
		return nil
	},
}

var switchCmd = &cobra.Command{
	Use:          "switch",
	Short:        "Switch the default environment (interactive)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !prompt.IsInteractive() {
			return fmt.Errorf("interactive terminal required; run `arcane config set environment <id>` instead")
		}

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := fmt.Sprintf("%s?limit=%d", types.Endpoints.Environments(), 200)
		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list environments: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[environment.Environment]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if len(result.Data) == 0 {
			return fmt.Errorf("no environments available")
		}

		currentEnv := cfg.DefaultEnvironment
		if strings.TrimSpace(currentEnv) == "" {
			currentEnv = "0"
		}

		envs := result.Data
		sort.SliceStable(envs, func(i, j int) bool {
			left := strings.ToLower(strings.TrimSpace(envs[i].Name))
			right := strings.ToLower(strings.TrimSpace(envs[j].Name))
			if left == "" {
				left = strings.ToLower(envs[i].ID)
			}
			if right == "" {
				right = strings.ToLower(envs[j].ID)
			}
			if left == right {
				return envs[i].ID < envs[j].ID
			}
			return left < right
		})

		options := make([]string, len(envs))
		for i, env := range envs {
			displayName := strings.TrimSpace(env.Name)
			if displayName == "" {
				displayName = env.ID
			}
			status := strings.TrimSpace(env.Status)
			if status == "" {
				status = "unknown"
			}
			var statusLabel string
			switch strings.ToLower(status) {
			case "online":
				statusLabel = statusOnlineStyle.Render(status)
			case "offline":
				statusLabel = statusOfflineStyle.Render(status)
			default:
				statusLabel = statusMutedStyle.Render(status)
			}

			enabledLabel := statusMutedStyle.Render("disabled")
			if env.Enabled {
				enabledLabel = enabledStyle.Render("enabled")
			}
			marker := "  "
			if env.ID == currentEnv {
				marker = "* "
			}
			options[i] = fmt.Sprintf("%s%s (id: %s, %s, %s)", marker, displayName, env.ID, statusLabel, enabledLabel)
		}

		choice, err := prompt.Select("environment", options)
		if err != nil {
			return err
		}

		selected := envs[choice]
		if selected.ID == currentEnv {
			output.Info("Default environment already set to %s", selected.ID)
			return nil
		}

		cfg.DefaultEnvironment = selected.ID
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		output.Success("Default environment set to %s", selected.ID)
		if strings.TrimSpace(selected.Name) != "" {
			output.KeyValue("Name", selected.Name)
		}
		output.KeyValue("API URL", selected.ApiUrl)
		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:          "update <id>",
	Short:        "Update environment",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		var req environment.Update
		if cmd.Flags().Changed("enabled") && cmd.Flags().Changed("disabled") {
			return fmt.Errorf("--enabled and --disabled are mutually exclusive")
		}
		if cmd.Flags().Changed("name") {
			req.Name = &envUpdateName
		}
		if cmd.Flags().Changed("api-url") {
			req.ApiUrl = &envUpdateApiUrl
		}
		if cmd.Flags().Changed("enabled") {
			enabled := true
			req.Enabled = &enabled
		}
		if cmd.Flags().Changed("disabled") {
			enabled := false
			req.Enabled = &enabled
		}

		resp, err := c.Put(cmd.Context(), types.Endpoints.Environment(args[0]), req)
		if err != nil {
			return fmt.Errorf("failed to update environment: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to update environment: %w", err)
		}

		var result base.ApiResponse[environment.Environment]
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

		output.Success("Environment updated successfully")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		output.KeyValue("API URL", result.Data.ApiUrl)
		output.KeyValue("Enabled", result.Data.Enabled)
		return nil
	},
}

var versionCmd = &cobra.Command{
	Use:          "version <id>",
	Short:        "Get environment version",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.EnvironmentVersion(args[0]))
		if err != nil {
			return fmt.Errorf("failed to get environment version: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to get environment version: %w", err)
		}

		var result struct {
			CurrentVersion  string `json:"currentVersion"`
			CurrentTag      string `json:"currentTag"`
			Revision        string `json:"revision"`
			ShortRevision   string `json:"shortRevision"`
			GoVersion       string `json:"goVersion"`
			BuildTime       string `json:"buildTime"`
			DisplayVersion  string `json:"displayVersion"`
			UpdateAvailable bool   `json:"updateAvailable"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Environment Version")
		output.KeyValue("Version", result.DisplayVersion)
		output.KeyValue("Full Version", result.CurrentVersion)
		output.KeyValue("Tag", result.CurrentTag)
		output.KeyValue("Revision", result.ShortRevision)
		output.KeyValue("Go Version", result.GoVersion)
		output.KeyValue("Build Time", result.BuildTime)
		output.KeyValue("Update Available", fmt.Sprintf("%t", result.UpdateAvailable))
		return nil
	},
}

func init() {
	EnvironmentsCmd.AddCommand(listCmd)
	EnvironmentsCmd.AddCommand(getCmd)
	EnvironmentsCmd.AddCommand(testCmd)
	EnvironmentsCmd.AddCommand(deleteCmd)
	EnvironmentsCmd.AddCommand(switchCmd)
	EnvironmentsCmd.AddCommand(updateCmd)
	EnvironmentsCmd.AddCommand(versionCmd)

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of environments to show")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Get command flags
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Test command flags
	testCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Delete command flags
	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Update command flags
	updateCmd.Flags().StringVar(&envUpdateName, "name", "", "Environment name")
	updateCmd.Flags().StringVar(&envUpdateApiUrl, "api-url", "", "API URL")
	updateCmd.Flags().BoolVar(&envUpdateEnabled, "enabled", false, "Enable environment")
	updateCmd.Flags().BoolVar(&envUpdateDisabled, "disabled", false, "Disable environment")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Version command flags
	versionCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
