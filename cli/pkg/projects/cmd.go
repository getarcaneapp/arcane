package projects

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/output"
	"go.getarcane.app/cli/internal/types"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/project"
)

var (
	limitFlag  int
	forceFlag  bool
	jsonOutput bool
)

// ProjectsCmd is the parent command for project operations
var ProjectsCmd = &cobra.Command{
	Use:     "projects",
	Aliases: []string{"project", "proj", "p"},
	Short:   "Manage projects",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List projects",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.Projects(c.EnvID())
		if limitFlag > 0 {
			path = fmt.Sprintf("%s?limit=%d", path, limitFlag)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list projects: %w", err)
		}
		defer resp.Body.Close()

		var result base.Paginated[project.Details]
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

		headers := []string{"ID", "NAME", "STATUS", "SERVICES", "RUNNING", "CREATED"}
		rows := make([][]string, len(result.Data))
		for i, proj := range result.Data {
			rows[i] = []string{
				proj.ID,
				proj.Name,
				proj.Status,
				fmt.Sprintf("%d", proj.ServiceCount),
				fmt.Sprintf("%d", proj.RunningCount),
				proj.CreatedAt,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d projects\n", result.Pagination.TotalItems)
		return nil
	},
}

var destroyCmd = &cobra.Command{
	Use:          "destroy <project-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Destroy project and remove all containers",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			fmt.Printf("Are you sure you want to destroy project %s? This will remove all containers! (y/N): ", args[0])
			var response string
			fmt.Scanln(&response)
			if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
				fmt.Println("Cancelled")
				return nil
			}
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Delete(cmd.Context(), types.Endpoints.ProjectDestroy(c.EnvID(), args[0]))
		if err != nil {
			return fmt.Errorf("failed to destroy project: %w", err)
		}
		defer resp.Body.Close()

		output.Success("Project %s destroyed successfully", args[0])
		return nil
	},
}

func init() {
	ProjectsCmd.AddCommand(listCmd)
	ProjectsCmd.AddCommand(destroyCmd)

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of projects to show")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Destroy command flags
	destroyCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force destroy without confirmation")
	destroyCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}