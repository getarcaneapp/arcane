package projects

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/getarcaneapp/arcane/types/v2/workspace"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "projects",
		Aliases: []string{"project", "proj", "p"},
		Short:   "Manage projects",
	}
	cmd.AddCommand(
		newTagsCmd(),
		newTagCmd(),
		newArchiveCmd(),
		newUnarchiveCmd(),
		newBuildCmd(),
		newUpdateServicesCmd(),
		newWorkspaceCmd(),
		newListCmd(),
		newUpdatesCmd(),
		newGetCmd(),
		newUpCmd(),
		newDownCmd(),
		newRestartCmd(),
		newRedeployCmd(),
		newPullCmd(),
		newCountsCmd(),
		newDestroyCmd(),
		newCreateCmd(),
		newUpdateCmd(),
	)
	cmdutil.SetScope(cmdutil.ScopeResource, cmd)
	return cmd
}

func newListCmd() *cobra.Command {
	var opts projectListOptions
	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List projects",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE:         func(cmd *cobra.Command, args []string) error { return runProjectList(cmd, opts) },
	}
	cmd.Flags().IntVarP(&opts.limitFlag, "limit", "n", 20, "Number of projects to show")
	cmd.Flags().IntVar(&opts.startFlag, "start", 0, cmdutil.StartFlagUsage)
	cmd.Flags().BoolVarP(&opts.allFlag, "all", "a", false, cmdutil.AllFlagUsage+", including archived projects")
	cmdutil.EnumVarP(cmd, cmd.Flags(), &opts.projectsUpdatesFilter, "updates", "", "", "Filter by update status", "has_update", "up_to_date", "error", "unknown")
	cmd.Flags().StringVar(&opts.projectsStatusFilter, "status", "", "Filter by status (comma-separated: running, stopped, partially running)")
	cmdutil.EnumVarP(cmd, cmd.Flags(), &opts.projectsArchivedFilter, "archived", "", "", "Archived filter", "true", "false", "all")
	cmd.Flags().StringVar(&opts.projectsLabelFilter, "label", "", "Filter by container label (key or key=value)")
	cmdutil.SetScope(cmdutil.ScopeMerged, cmd)
	return cmd
}

func newUpdatesCmd() *cobra.Command {
	var opts projectListOptions
	cmd := &cobra.Command{
		Use:          "updates",
		Short:        "List projects with available updates",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE:         func(cmd *cobra.Command, args []string) error { return runProjectList(cmd, opts) },
	}
	cmd.Flags().IntVarP(&opts.limitFlag, "limit", "n", 20, "Number of projects to show")
	cmd.Flags().IntVar(&opts.startFlag, "start", 0, cmdutil.StartFlagUsage)
	cmd.Flags().BoolVarP(&opts.allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	cmdutil.SetScope(cmdutil.ScopeMerged, cmd)
	return cmd
}

func newDestroyCmd() *cobra.Command {
	var opts struct {
		forceFlag            bool
		destroyRemoveFiles   bool
		destroyRemoveVolumes bool
	}
	cmd := &cobra.Command{
		Use:          "destroy <project-id|name>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Destroy project and remove all containers",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := projectRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.forceFlag,
				Prompt:         fmt.Sprintf("Are you sure you want to destroy project %s? This will remove all containers!", cmp.Or(resolved.Name, resolved.ID)),
				Path:           types.ProjectDestroy(c.EnvID(), resolved.ID),
				Body:           project.Destroy{RemoveFiles: &opts.destroyRemoveFiles, RemoveVolumes: opts.destroyRemoveVolumes},
				FailureMessage: "failed to destroy project",
				SuccessMessage: fmt.Sprintf("Project %s destroyed successfully", resolved.Name),
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.forceFlag, "force", "f", false, "Force destroy without confirmation")
	cmd.Flags().BoolVar(&opts.destroyRemoveFiles, "remove-files", true, "Remove the project's files from disk")
	cmd.Flags().BoolVar(&opts.destroyRemoveVolumes, "remove-volumes", false, "Remove the project's volumes")
	return cmd
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "get <project-id|name>",
		Short:        "Get project details",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			allowPrompt := cmdutil.AllowPrompt(cmd)
			resolved, err := projectRef.Fetch(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			if cmdutil.JSONOutputEnabled(cmd) {
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
}

func newUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "up <project-id|name>",
		Short:        "Start project services",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectStreamAction(cmd, args[0], projectStreamActionConfig{
				endpoint:       types.ProjectUp,
				failureMessage: "failed to start project",
				successMessage: "Project %s started successfully",
			})
		},
	}
}

func newDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "down <project-id|name>",
		Short:        "Stop project services",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := projectRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[base.MessageResponse]{
				Method:         http.MethodPost,
				Path:           types.ProjectDown(c.EnvID(), resolved.ID),
				FailureMessage: "failed to stop project",
				Render: func(base.MessageResponse) error {
					output.Success("%s", fmt.Sprintf("Project %s stopped successfully", resolved.Name))
					return nil
				},
			})
		},
	}
}

func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "restart <project-id|name>",
		Short:        "Restart project services",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := projectRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[base.MessageResponse]{
				Method:         http.MethodPost,
				Path:           types.ProjectRestart(c.EnvID(), resolved.ID),
				FailureMessage: "failed to restart project",
				Render: func(base.MessageResponse) error {
					output.Success("%s", fmt.Sprintf("Project %s restarted successfully", resolved.Name))
					return nil
				},
			})
		},
	}
}

func newRedeployCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "redeploy <project-id|name>",
		Short:        "Redeploy project (pull images and restart)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectStreamAction(cmd, args[0], projectStreamActionConfig{
				endpoint:       types.ProjectRedeploy,
				failureMessage: "failed to redeploy project",
				successMessage: "Project %s redeployed successfully",
			})
		},
	}
}

func newPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "pull <project-id|name>",
		Short:        "Pull latest images for project",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectStreamAction(cmd, args[0], projectStreamActionConfig{
				endpoint:       types.ProjectPull,
				failureMessage: "failed to pull images",
				successMessage: "Images pulled successfully for project %s",
			})
		},
	}
}

type projectStreamFrame struct {
	Log   string `json:"log"`
	Error string `json:"error"`
	Done  bool   `json:"done"`
}

type projectStreamActionConfig struct {
	body           any
	endpoint       func(string, string) string
	failureMessage string
	successMessage string
}

func runProjectStreamAction(cmd *cobra.Command, identifier string, cfg projectStreamActionConfig) error {
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return err
	}

	resolved, _, err := projectRef.Resolve(cmd.Context(), c, identifier, false)
	if err != nil {
		return err
	}

	// Streamed actions can take a long time as they may pull images.
	c.SetTimeout(30 * time.Minute)

	resp, err := c.Post(cmd.Context(), cfg.endpoint(c.EnvID(), resolved.ID), cfg.body)
	if err != nil {
		return fmt.Errorf("%s: %w", cfg.failureMessage, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ensureSuccessStatusErr := cmdutil.EnsureSuccessStatus(resp); ensureSuccessStatusErr != nil {
		return fmt.Errorf("%s: %w", cfg.failureMessage, ensureSuccessStatusErr)
	}

	jsonMode := cmdutil.JSONOutputEnabled(cmd)
	frames := make([]jsontext.Value, 0)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var frame projectStreamFrame
		if json.Unmarshal([]byte(line), &frame) != nil {
			continue
		}
		if jsonMode {
			frames = append(frames, jsontext.Value(line))
		}
		if frame.Error != "" {
			return fmt.Errorf("%s: %s", cfg.failureMessage, frame.Error)
		}
		if frame.Done {
			break
		}
		if !jsonMode && frame.Log != "" {
			_, _ = fmt.Fprintln(output.Stdout(), frame.Log)
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return fmt.Errorf("%s: %w", cfg.failureMessage, scanErr)
	}

	if jsonMode {
		return cmdutil.PrintJSON(frames)
	}
	output.Success(cfg.successMessage, resolved.Name)
	return nil
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		createName    string
		createFile    string
		createEnvFile string
	}
	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a new project from a Docker Compose file",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			composeBytes, err := os.ReadFile(opts.createFile)
			if err != nil {
				return fmt.Errorf("failed to read compose file: %w", err)
			}

			body := project.CreateProject{
				Name:           opts.createName,
				ComposeContent: string(composeBytes),
			}

			if opts.createEnvFile != "" {
				envBytes, readFileErr := os.ReadFile(opts.createEnvFile)
				if readFileErr != nil {
					return fmt.Errorf("failed to read env file: %w", readFileErr)
				}
				body.EnvContent = new(string(envBytes))
			}

			// Creating can take a long time as it may pull images
			c.SetTimeout(30 * time.Minute)

			projectJSON, err := json.Marshal(body)
			if err != nil {
				return fmt.Errorf("failed to encode project configuration: %w", err)
			}
			manifestJSON, err := json.Marshal(project.CreateProjectWorkspaceManifest{FileChanges: []project.WorkspaceFileChange{}})
			if err != nil {
				return fmt.Errorf("failed to encode project workspace manifest: %w", err)
			}
			var requestBody bytes.Buffer
			writer := multipart.NewWriter(&requestBody)
			if writeFieldErr := writer.WriteField("project", string(projectJSON)); writeFieldErr != nil {
				return fmt.Errorf("failed to write project configuration: %w", writeFieldErr)
			}
			if writeManifestErr := writer.WriteField("manifest", string(manifestJSON)); writeManifestErr != nil {
				return fmt.Errorf("failed to write project workspace manifest: %w", writeManifestErr)
			}
			if closeErr := writer.Close(); closeErr != nil {
				return fmt.Errorf("failed to finalize project request: %w", closeErr)
			}

			resp, err := c.RequestRaw(cmd.Context(), http.MethodPost, types.Projects(c.EnvID()), &requestBody, map[string]string{"Content-Type": writer.FormDataContentType()})
			if err != nil {
				return fmt.Errorf("failed to create project: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			responseBody, readErr := cmdutil.ReadJSONBody(resp)
			if readErr != nil {
				return fmt.Errorf("failed to create project: %w", readErr)
			}
			var result base.ApiResponse[jsontext.Value]
			if decodeErr := json.Unmarshal(responseBody, &result); decodeErr != nil {
				return fmt.Errorf("failed to parse response: %w", decodeErr)
			}
			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(result.Data)
			}
			var data project.CreateReponse
			if decodeErr := json.Unmarshal(result.Data, &data); decodeErr != nil {
				return fmt.Errorf("failed to parse response: %w", decodeErr)
			}

			output.Success("Project %s created successfully", data.Name)
			output.Header("Project Details")
			output.KeyValue("ID", data.ID)
			output.KeyValue("Name", data.Name)
			output.KeyValue("Status", data.Status)
			output.KeyValue("Path", data.Path)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.createName, "name", "", "Project name")
	cmd.Flags().StringVarP(&opts.createFile, "file", "f", "", "Docker Compose file")
	cmd.Flags().StringVar(&opts.createEnvFile, "env-file", "", "Environment file")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("file")
	cmdutil.SetScope(cmdutil.ScopeDestination, cmd)
	return cmd
}

func newUpdateCmd() *cobra.Command {
	var opts struct {
		updateName    string
		updateFile    string
		updateEnvFile string
	}
	cmd := &cobra.Command{
		Use:          "update <project-id|name>",
		Short:        "Update an existing project",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := projectRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			var composeContent, envContent *string

			if cmd.Flags().Changed("file") {
				composeBytes, readFileErr := os.ReadFile(opts.updateFile)
				if readFileErr != nil {
					return fmt.Errorf("failed to read compose file: %w", readFileErr)
				}
				composeContent = new(string(composeBytes))
			}

			if cmd.Flags().Changed("env-file") {
				envBytes, readEnvFileErr := os.ReadFile(opts.updateEnvFile)
				if readEnvFileErr != nil {
					return fmt.Errorf("failed to read env file: %w", readEnvFileErr)
				}
				envContent = new(string(envBytes))
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[project.Details]{
				Method: http.MethodPut,
				Path:   types.Project(c.EnvID(), resolved.ID),
				Body: project.UpdateProject{
					Name:           kit.Ternary(cmd.Flags().Changed("name"), &opts.updateName, nil),
					ComposeContent: composeContent,
					EnvContent:     envContent,
				},
				FailureMessage: "failed to update project",
				Render: func(project.Details) error {
					output.Success("Project %s updated successfully", resolved.Name)
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.updateName, "name", "", "New project name")
	cmd.Flags().StringVarP(&opts.updateFile, "file", "f", "", "Docker Compose file")
	cmd.Flags().StringVar(&opts.updateEnvFile, "env-file", "", "Environment file")
	return cmd
}

func newUpdateIncludesCmd() *cobra.Command {
	var opts struct {
		includesFile string
	}
	cmd := &cobra.Command{
		Use:          "update <project-id|name>",
		Aliases:      []string{"update-includes"},
		Short:        "Update an include file in a project workspace",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := projectRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			content, err := os.ReadFile(opts.includesFile)
			if err != nil {
				return fmt.Errorf("failed to read include file: %w", err)
			}

			current, err := c.GetJSON[workspace.Workspace](cmd.Context(), types.ProjectWorkspace(c.EnvID(), resolved.ID))
			if err != nil {
				return fmt.Errorf("failed to get project workspace: %w", err)
			}

			relativePath := filepath.ToSlash(filepath.Base(opts.includesFile))
			operation := project.FileOpCreateFile
			if slices.ContainsFunc(current.Data.Files, func(entry workspace.FileEntry) bool {
				return entry.RelativePath == relativePath && !entry.IsDirectory
			}) {
				operation = project.FileOpUpdateFile
			}
			uploadIndex := 0
			manifest := project.WorkspaceUpdateManifest{
				FileTreeRevision: current.Data.FileTreeRevision,
				FileChanges:      []project.WorkspaceFileChange{{Operation: operation, RelativePath: relativePath, UploadIndex: &uploadIndex}},
			}
			manifestJSON, err := json.Marshal(manifest)
			if err != nil {
				return fmt.Errorf("failed to encode workspace manifest: %w", err)
			}
			var requestBody bytes.Buffer
			writer := multipart.NewWriter(&requestBody)
			if writeFieldErr := writer.WriteField("manifest", string(manifestJSON)); writeFieldErr != nil {
				return fmt.Errorf("failed to write workspace manifest: %w", writeFieldErr)
			}
			filePart, err := writer.CreateFormFile("files", filepath.Base(opts.includesFile))
			if err != nil {
				return fmt.Errorf("failed to create workspace upload: %w", err)
			}
			if _, writeErr := filePart.Write(content); writeErr != nil {
				return fmt.Errorf("failed to write workspace upload: %w", writeErr)
			}
			if closeErr := writer.Close(); closeErr != nil {
				return fmt.Errorf("failed to finalize workspace upload: %w", closeErr)
			}

			resp, err := c.RequestRaw(cmd.Context(), http.MethodPut, types.ProjectWorkspace(c.EnvID(), resolved.ID), &requestBody, map[string]string{"Content-Type": writer.FormDataContentType()})
			if err != nil {
				return fmt.Errorf("failed to update include file: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			responseBody, readErr := cmdutil.ReadJSONBody(resp)
			if readErr != nil {
				return fmt.Errorf("failed to update include file: %w", readErr)
			}
			var result base.ApiResponse[jsontext.Value]
			if decodeErr := json.Unmarshal(responseBody, &result); decodeErr != nil {
				return fmt.Errorf("failed to parse response: %w", decodeErr)
			}
			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(result.Data)
			}

			output.Success("Include file %s updated successfully for project %s", filepath.Base(opts.includesFile), resolved.Name)
			return nil
		},
	}
	cmd.Flags().StringVarP(&opts.includesFile, "file", "f", "", "Include file")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newCountsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "counts",
		Short:        "Get project counts",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[map[string]any]{
				Method:         http.MethodGet,
				Path:           types.ProjectsCounts(c.EnvID()),
				FailureMessage: "failed to get project counts",
				Render: func(data map[string]any) error {
					output.Header("Project Counts")
					for k, v := range data {
						output.KeyValue(k, v)
					}
					return nil
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeMerged, cmd)
	return cmd
}

var projectRef = cmdutil.ResourceRef[project.Details, project.Details]{
	EnvironmentScoped: true,
	Singular:          "project",
	Plural:            "projects",
	IDHint:            "the project ID",
	ListCmd:           "arcane projects list",
	GetPath:           types.Project,
	IdentifierOf:      func(data project.Details) string { return data.ID },
	ListPath:          types.Projects,
	Matches: func(item project.Details, identifierLower, original string) bool {
		return strings.Contains(strings.ToLower(item.ID), identifierLower) ||
			strings.Contains(strings.ToLower(item.Name), identifierLower) || strings.EqualFold(item.Name, original) ||
			strings.Contains(strings.ToLower(item.Path), identifierLower)
	},
	Label: func(match project.Details) string {
		return fmt.Sprintf("%s (%s, %s)", match.Name, match.ID, match.Status)
	},
	Promote: func(match project.Details) *project.Details { return &match },
}

type projectListOptions struct {
	limitFlag              int
	startFlag              int
	allFlag                bool
	projectsUpdatesFilter  string
	projectsStatusFilter   string
	projectsArchivedFilter string
	projectsLabelFilter    string
}

func runProjectList(cmd *cobra.Command, opts projectListOptions) error {
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return err
	}

	query := url.Values{}
	updatesFilter := kit.Ternary(cmd.Name() == "updates", "has_update", strings.TrimSpace(opts.projectsUpdatesFilter))
	if updatesFilter != "" {
		query.Set("updates", updatesFilter)
	}
	if opts.projectsStatusFilter != "" {
		query.Set("status", opts.projectsStatusFilter)
	}
	if labelFilter := strings.TrimSpace(opts.projectsLabelFilter); labelFilter != "" {
		query.Set("label", labelFilter)
	}
	// The server excludes archived projects unless asked, so --all has to opt
	// into them explicitly to actually mean "everything".
	switch {
	case opts.projectsArchivedFilter != "":
		query.Set("archived", opts.projectsArchivedFilter)
	case opts.allFlag:
		query.Set("archived", "all")
	}

	if updatesFilter != "" && !cmdutil.JSONOutputEnabled(cmd) {
		output.Header("Project Updates")
	}

	return cmdutil.RunList(cmd, c, cmdutil.ListSpec[project.Details]{
		Resource: "projects",
		Endpoint: types.Projects(c.EnvID()),
		Params:   cmdutil.ListParams{Resource: "projects", Limit: opts.limitFlag, FallbackDefault: 20, Start: opts.startFlag, All: opts.allFlag},
		Query:    query,
		Headers:  kit.Ternary(updatesFilter != "", []string{"ID", "NAME", "STATUS", "UPDATES", "IMAGES", "UPDATED"}, []string{"ID", "NAME", "STATUS", "SERVICES", "RUNNING", "CREATED"}),
		Row: func(proj project.Details) []string {
			if updatesFilter == "" {
				return []string{
					proj.ID,
					proj.Name,
					proj.Status,
					strconv.Itoa(proj.ServiceCount),
					strconv.Itoa(proj.RunningCount),
					proj.CreatedAt,
				}
			}
			imageCount := 0
			updatedCount := 0
			status := "unknown"
			if proj.UpdateInfo != nil {
				imageCount = proj.UpdateInfo.ImageCount
				updatedCount = proj.UpdateInfo.ImagesWithUpdates
				if strings.TrimSpace(proj.UpdateInfo.Status) != "" {
					status = proj.UpdateInfo.Status
				}
			}
			return []string{
				proj.ID,
				proj.Name,
				proj.Status,
				status,
				strconv.Itoa(imageCount),
				strconv.Itoa(updatedCount),
			}
		},
	})
}
