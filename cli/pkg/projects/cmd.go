package projects

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/project"
	workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"
	"github.com/spf13/cobra"
)

var (
	limitFlag              int
	startFlag              int
	allFlag                bool
	projectsUpdatesFilter  string
	projectsStatusFilter   string
	projectsArchivedFilter string
	forceFlag              bool
	jsonOutput             bool
	destroyRemoveFiles     bool
	destroyRemoveVolumes   bool

	createName    string
	createFile    string
	createEnvFile string
	updateName    string
	updateFile    string
	updateEnvFile string
	includesFile  string
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
		return runProjectsList(cmd, false)
	},
}

var updatesCmd = &cobra.Command{
	Use:          "updates",
	Short:        "List projects with available updates",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runProjectsList(cmd, true)
	},
}

func runProjectsList(cmd *cobra.Command, forceHasUpdateFilter bool) error {
	c, err := client.NewFromConfig()
	if err != nil {
		return err
	}

	path, err := buildProjectsListPath(cmd, c, forceHasUpdateFilter)
	if err != nil {
		return err
	}
	resp, err := c.Get(cmd.Context(), path)
	if err != nil {
		return errors.WrapIf(err, "failed to list projects")
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := cmdutil.ReadJSONBody(resp)
	if err != nil {
		return errors.WrapIf(err, "failed to list projects")
	}

	if jsonOutput {
		return cmdutil.PrintRawJSON(body)
	}

	var result base.Paginated[project.Details]
	if err := json.Unmarshal(body, &result); err != nil {
		return errors.WrapIf(err, "failed to parse response")
	}

	effectiveUpdatesFilter := strings.TrimSpace(projectsUpdatesFilter)
	if forceHasUpdateFilter {
		effectiveUpdatesFilter = "has_update"
	}
	if effectiveUpdatesFilter != "" {
		output.Header("Project Updates")
		headers := []string{"ID", "NAME", "STATUS", "UPDATES", "IMAGES", "UPDATED"}
		rows := make([][]string, len(result.Data))
		for i, proj := range result.Data {
			imageCount := 0
			updatedCount := 0
			if proj.UpdateInfo != nil {
				imageCount = proj.UpdateInfo.ImageCount
				updatedCount = proj.UpdateInfo.ImagesWithUpdates
			}
			rows[i] = []string{
				proj.ID,
				proj.Name,
				proj.Status,
				projectUpdateStatus(proj),
				strconv.Itoa(imageCount),
				strconv.Itoa(updatedCount),
			}
		}
		output.Table(headers, rows)
		output.Showing(len(result.Data), result.Pagination.TotalItems, "projects")
		return nil
	}

	headers := []string{"ID", "NAME", "STATUS", "SERVICES", "RUNNING", "CREATED"}
	rows := make([][]string, len(result.Data))
	for i, proj := range result.Data {
		rows[i] = []string{
			proj.ID,
			proj.Name,
			proj.Status,
			strconv.Itoa(proj.ServiceCount),
			strconv.Itoa(proj.RunningCount),
			proj.CreatedAt,
		}
	}

	output.Table(headers, rows)
	output.Showing(len(result.Data), result.Pagination.TotalItems, "projects")
	return nil
}

func buildProjectsListPath(cmd *cobra.Command, c *client.Client, forceHasUpdateFilter bool) (string, error) {
	path, err := cmdutil.ApplyPaginationParams(cmd, types.Endpoints.Projects(c.EnvID()), cmdutil.ListParams{
		Resource:        "projects",
		Limit:           limitFlag,
		FallbackDefault: 20,
		Start:           startFlag,
		All:             allFlag,
	})
	if err != nil {
		return "", errors.WrapIf(err, "failed to build pagination query")
	}

	parsed, err := url.Parse(path)
	if err != nil {
		return "", errors.WrapIf(err, "failed to parse path")
	}

	query := parsed.Query()
	updatesFilter := strings.TrimSpace(projectsUpdatesFilter)
	if forceHasUpdateFilter {
		updatesFilter = "has_update"
	}
	if updatesFilter != "" {
		query.Set("updates", updatesFilter)
	}
	if projectsStatusFilter != "" {
		query.Set("status", projectsStatusFilter)
	}
	// The server excludes archived projects unless asked, so --all has to opt
	// into them explicitly to actually mean "everything".
	switch {
	case projectsArchivedFilter != "":
		query.Set("archived", projectsArchivedFilter)
	case allFlag:
		query.Set("archived", "all")
	}

	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func projectUpdateStatus(item project.Details) string {
	if item.UpdateInfo == nil || strings.TrimSpace(item.UpdateInfo.Status) == "" {
		return "unknown"
	}
	return item.UpdateInfo.Status
}

var destroyCmd = &cobra.Command{
	Use:          "destroy <project-id|name>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Destroy project and remove all containers",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := projectRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		if !forceFlag {
			display := resolved.Name
			if display == "" {
				display = resolved.ID
			}
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to destroy project %s? This will remove all containers!", display))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		resp, err := c.DeleteWithBody(cmd.Context(), types.Endpoints.ProjectDestroy(c.EnvID(), resolved.ID), project.Destroy{
			RemoveFiles:   &destroyRemoveFiles,
			RemoveVolumes: destroyRemoveVolumes,
		})
		if err != nil {
			return errors.WrapIf(err, "failed to destroy project")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to destroy project")
		}

		output.Success("Project %s destroyed successfully", resolved.Name)
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:          "get <project-id|name>",
	Short:        "Get project details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolved, complete, err := projectRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		if !complete {
			result, err := c.GetJSON[project.Details](cmd.Context(), types.Endpoints.Project(c.EnvID(), resolved.ID))
			if err != nil {
				return errors.WrapIf(err, "failed to get project")
			}
			resolved = &result.Data
		}

		if jsonOutput {
			return cmdutil.PrintJSON(resolved)
		}

		output.Header("Project Details")
		output.KeyValue("ID", resolved.ID)
		output.KeyValue("Name", resolved.Name)
		output.KeyValue("Status", resolved.Status)
		output.KeyValue("Services", resolved.ServiceCount)
		output.KeyValue("Running", resolved.RunningCount)
		return nil
	},
}

var upCmd = &cobra.Command{
	Use:          "up <project-id|name>",
	Short:        "Start project services",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runProjectStreamAction(cmd, args[0], projectStreamActionConfig{
			endpoint:       types.Endpoints.ProjectUp,
			failureMessage: "failed to start project",
			successMessage: "Project %s started successfully",
		})
	},
}

var downCmd = &cobra.Command{
	Use:          "down <project-id|name>",
	Short:        "Stop project services",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := projectRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		return cmdutil.RunPostAction[base.MessageResponse](cmd, c, cmdutil.PostActionSpec{
			Path:           types.Endpoints.ProjectDown(c.EnvID(), resolved.ID),
			FailureMessage: "failed to stop project",
			SuccessMessage: fmt.Sprintf("Project %s stopped successfully", resolved.Name),
		})
	},
}

var restartCmd = &cobra.Command{
	Use:          "restart <project-id|name>",
	Short:        "Restart project services",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := projectRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		return cmdutil.RunPostAction[base.MessageResponse](cmd, c, cmdutil.PostActionSpec{
			Path:           types.Endpoints.ProjectRestart(c.EnvID(), resolved.ID),
			FailureMessage: "failed to restart project",
			SuccessMessage: fmt.Sprintf("Project %s restarted successfully", resolved.Name),
		})
	},
}

var redeployCmd = &cobra.Command{
	Use:          "redeploy <project-id|name>",
	Short:        "Redeploy project (pull images and restart)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runProjectStreamAction(cmd, args[0], projectStreamActionConfig{
			endpoint:       types.Endpoints.ProjectRedeploy,
			failureMessage: "failed to redeploy project",
			successMessage: "Project %s redeployed successfully",
		})
	},
}

var pullCmd = &cobra.Command{
	Use:          "pull <project-id|name>",
	Short:        "Pull latest images for project",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runProjectStreamAction(cmd, args[0], projectStreamActionConfig{
			endpoint:       types.Endpoints.ProjectPull,
			failureMessage: "failed to pull images",
			successMessage: "Images pulled successfully for project %s",
		})
	},
}

// projectStreamActionConfig describes a project action whose endpoint responds
// with an NDJSON operation stream: {"log":"<raw docker CLI line>"} frames are
// printed as-is and a terminal {"error":"..."} frame fails the command.
type projectStreamActionConfig struct {
	endpoint       func(string, string) string
	failureMessage string
	successMessage string
}

func runProjectStreamAction(cmd *cobra.Command, identifier string, cfg projectStreamActionConfig) error {
	c, err := client.NewFromConfig()
	if err != nil {
		return err
	}

	resolved, _, err := projectRef.Resolve(cmd.Context(), c, identifier, false)
	if err != nil {
		return err
	}

	// Streamed actions can take a long time as they may pull images.
	c.SetTimeout(30 * time.Minute)

	resp, err := c.Post(cmd.Context(), cfg.endpoint(c.EnvID(), resolved.ID), nil)
	if err != nil {
		return errors.WrapIff(err, "%s", cfg.failureMessage)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
		return errors.WrapIff(err, "%s", cfg.failureMessage)
	}

	if err := printOperationStreamInternal(resp.Body); err != nil {
		return errors.WrapIff(err, "%s", cfg.failureMessage)
	}

	output.Success(cfg.successMessage, resolved.Name)
	return nil
}

// printOperationStreamInternal consumes an NDJSON operation stream, printing
// raw docker CLI log lines to stdout. A terminal error frame is returned as an
// error; unknown frames (e.g. the activity envelope) are skipped.
func printOperationStreamInternal(body io.Reader) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var frame struct {
			Log   string `json:"log"`
			Error string `json:"error"`
			Done  bool   `json:"done"`
		}
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			continue
		}
		if frame.Error != "" {
			return errors.New(frame.Error)
		}
		if frame.Done {
			return nil
		}
		if frame.Log != "" {
			fmt.Println(frame.Log)
		}
	}

	return scanner.Err()
}

var createCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create a new project from a Docker Compose file",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		composeBytes, err := os.ReadFile(createFile)
		if err != nil {
			return errors.WrapIf(err, "failed to read compose file")
		}

		body := project.CreateProject{
			Name:           createName,
			ComposeContent: string(composeBytes),
		}

		if createEnvFile != "" {
			envBytes, err := os.ReadFile(createEnvFile)
			if err != nil {
				return errors.WrapIf(err, "failed to read env file")
			}
			body.EnvContent = new(string(envBytes))
		}

		// Creating can take a long time as it may pull images
		c.SetTimeout(30 * time.Minute)

		projectJSON, err := json.Marshal(body)
		if err != nil {
			return errors.WrapIf(err, "failed to encode project configuration")
		}
		manifestJSON, err := json.Marshal(project.CreateProjectWorkspaceManifest{FileChanges: []project.WorkspaceFileChange{}})
		if err != nil {
			return errors.WrapIf(err, "failed to encode project workspace manifest")
		}
		var requestBody bytes.Buffer
		writer := multipart.NewWriter(&requestBody)
		if err := writer.WriteField("project", string(projectJSON)); err != nil {
			return errors.WrapIf(err, "failed to write project configuration")
		}
		if err := writer.WriteField("manifest", string(manifestJSON)); err != nil {
			return errors.WrapIf(err, "failed to write project workspace manifest")
		}
		if err := writer.Close(); err != nil {
			return errors.WrapIf(err, "failed to finalize project request")
		}

		resp, err := c.RequestRaw(cmd.Context(), http.MethodPost, types.Endpoints.Projects(c.EnvID()), &requestBody, map[string]string{"Content-Type": writer.FormDataContentType()})
		if err != nil {
			return errors.WrapIf(err, "failed to create project")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to create project")
		}

		var result base.ApiResponse[project.CreateReponse]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Project %s created successfully", result.Data.Name)
		output.Header("Project Details")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		output.KeyValue("Status", result.Data.Status)
		output.KeyValue("Path", result.Data.Path)
		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:          "update <project-id|name>",
	Short:        "Update an existing project",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := projectRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		body := project.UpdateProject{}

		if cmd.Flags().Changed("name") {
			body.Name = &updateName
		}

		if cmd.Flags().Changed("file") {
			composeBytes, err := os.ReadFile(updateFile)
			if err != nil {
				return errors.WrapIf(err, "failed to read compose file")
			}
			body.ComposeContent = new(string(composeBytes))
		}

		if cmd.Flags().Changed("env-file") {
			envBytes, err := os.ReadFile(updateEnvFile)
			if err != nil {
				return errors.WrapIf(err, "failed to read env file")
			}
			body.EnvContent = new(string(envBytes))
		}

		result, err := c.PutJSON[project.Details](cmd.Context(), types.Endpoints.Project(c.EnvID(), resolved.ID), body)
		if err != nil {
			return errors.WrapIf(err, "failed to update project")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Project %s updated successfully", resolved.Name)
		return nil
	},
}

var updateIncludesCmd = &cobra.Command{
	Use:          "update-includes <project-id|name>",
	Short:        "Update an include file in a project",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := projectRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		content, err := os.ReadFile(includesFile)
		if err != nil {
			return errors.WrapIf(err, "failed to read include file")
		}

		current, err := c.GetJSON[workspacetypes.Workspace](cmd.Context(), types.Endpoints.ProjectWorkspace(c.EnvID(), resolved.ID))
		if err != nil {
			return errors.WrapIf(err, "failed to get project workspace")
		}

		relativePath := filepath.ToSlash(filepath.Base(includesFile))
		operation := project.FileOpCreateFile
		for _, entry := range current.Data.Files {
			if entry.RelativePath == relativePath && !entry.IsDirectory {
				operation = project.FileOpUpdateFile
				break
			}
		}
		uploadIndex := 0
		manifest := project.WorkspaceUpdateManifest{
			FileTreeRevision: current.Data.FileTreeRevision,
			FileChanges:      []project.WorkspaceFileChange{{Operation: operation, RelativePath: relativePath, UploadIndex: &uploadIndex}},
		}
		manifestJSON, err := json.Marshal(manifest)
		if err != nil {
			return errors.WrapIf(err, "failed to encode workspace manifest")
		}
		var requestBody bytes.Buffer
		writer := multipart.NewWriter(&requestBody)
		if err := writer.WriteField("manifest", string(manifestJSON)); err != nil {
			return errors.WrapIf(err, "failed to write workspace manifest")
		}
		filePart, err := writer.CreateFormFile("files", filepath.Base(includesFile))
		if err != nil {
			return errors.WrapIf(err, "failed to create workspace upload")
		}
		if _, err := filePart.Write(content); err != nil {
			return errors.WrapIf(err, "failed to write workspace upload")
		}
		if err := writer.Close(); err != nil {
			return errors.WrapIf(err, "failed to finalize workspace upload")
		}

		resp, err := c.RequestRaw(cmd.Context(), http.MethodPut, types.Endpoints.ProjectWorkspace(c.EnvID(), resolved.ID), &requestBody, map[string]string{"Content-Type": writer.FormDataContentType()})
		if err != nil {
			return errors.WrapIf(err, "failed to update include file")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to update include file")
		}

		if jsonOutput {
			var result base.ApiResponse[workspacetypes.Workspace]
			if err := cmdutil.DecodeJSON(resp, &result); err != nil {
				return err
			}
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Include file %s updated successfully for project %s", filepath.Base(includesFile), resolved.Name)
		return nil
	},
}

var countsCmd = &cobra.Command{
	Use:          "counts",
	Short:        "Get project counts",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		result, err := c.GetJSON[map[string]any](cmd.Context(), types.Endpoints.ProjectsCounts(c.EnvID()))
		if err != nil {
			return errors.WrapIf(err, "failed to get project counts")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("Project Counts")
		for k, v := range result.Data {
			output.KeyValue(k, v)
		}
		return nil
	},
}

func init() {
	ProjectsCmd.AddCommand(listCmd)
	ProjectsCmd.AddCommand(updatesCmd)
	ProjectsCmd.AddCommand(getCmd)
	ProjectsCmd.AddCommand(upCmd)
	ProjectsCmd.AddCommand(downCmd)
	ProjectsCmd.AddCommand(restartCmd)
	ProjectsCmd.AddCommand(redeployCmd)
	ProjectsCmd.AddCommand(pullCmd)
	ProjectsCmd.AddCommand(countsCmd)
	ProjectsCmd.AddCommand(destroyCmd)
	ProjectsCmd.AddCommand(createCmd)
	ProjectsCmd.AddCommand(updateCmd)
	ProjectsCmd.AddCommand(updateIncludesCmd)

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of projects to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
	listCmd.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage+", including archived projects")
	listCmd.Flags().StringVar(&projectsUpdatesFilter, "updates", "", "Filter by update status (has_update, up_to_date, error, unknown)")
	listCmd.Flags().StringVar(&projectsStatusFilter, "status", "", "Filter by status (comma-separated: running, stopped, partially running)")
	listCmd.Flags().StringVar(&projectsArchivedFilter, "archived", "", "Archived filter: 'true' for archived only, 'all' to include archived")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	updatesCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of projects to show")
	updatesCmd.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
	updatesCmd.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	updatesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Get command flags
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Counts command flags
	countsCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Destroy command flags
	destroyCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force destroy without confirmation")
	destroyCmd.Flags().BoolVar(&destroyRemoveFiles, "remove-files", true, "Remove the project's files from disk")
	destroyCmd.Flags().BoolVar(&destroyRemoveVolumes, "remove-volumes", false, "Remove the project's volumes")
	destroyCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Create command flags
	createCmd.Flags().StringVar(&createName, "name", "", "Project name")
	createCmd.Flags().StringVarP(&createFile, "file", "f", "", "Docker Compose file")
	createCmd.Flags().StringVar(&createEnvFile, "env-file", "", "Environment file")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = createCmd.MarkFlagRequired("name")
	_ = createCmd.MarkFlagRequired("file")

	// Update command flags
	updateCmd.Flags().StringVar(&updateName, "name", "", "New project name")
	updateCmd.Flags().StringVarP(&updateFile, "file", "f", "", "Docker Compose file")
	updateCmd.Flags().StringVar(&updateEnvFile, "env-file", "", "Environment file")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Update includes command flags
	updateIncludesCmd.Flags().StringVarP(&includesFile, "file", "f", "", "Include file")
	updateIncludesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = updateIncludesCmd.MarkFlagRequired("file")
}

var projectRef = cmdutil.ResourceRef[project.Details, project.Details]{
	Singular: "project",
	Plural:   "projects",
	IDHint:   "the project ID",
	ListCmd:  "arcane projects list",
	GetPath:  types.Endpoints.Project,
	ListPath: types.Endpoints.Projects,
	Matches:  projectMatches,
	Label: func(match project.Details) string {
		return fmt.Sprintf("%s (%s, %s)", match.Name, match.ID, match.Status)
	},
	Promote: func(match project.Details) *project.Details { return &match },
}

func projectMatches(item project.Details, identifierLower, original string) bool {
	idLower := strings.ToLower(item.ID)
	if idLower == identifierLower || (len(identifierLower) >= 4 && strings.HasPrefix(idLower, identifierLower)) {
		return true
	}
	if strings.Contains(idLower, identifierLower) {
		return true
	}
	if strings.Contains(strings.ToLower(item.Name), identifierLower) {
		return true
	}
	if strings.EqualFold(item.Name, original) {
		return true
	}
	if strings.Contains(strings.ToLower(item.Path), identifierLower) {
		return true
	}
	return false
}
