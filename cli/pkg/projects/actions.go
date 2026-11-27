package projects

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	kit "kit.getarcane.app"
	"kit.getarcane.app/acfs"

	"getarcane.dev/cli/v3/internal/client"
	"getarcane.dev/cli/v3/internal/cmdutil"
	"getarcane.dev/cli/v3/internal/output"
	clitypes "getarcane.dev/cli/v3/internal/types"
	"getarcane.dev/cli/v3/pkg/updater"
	"getarcane.dev/types/v3"
)

func newTagsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "tags",
		Short:        "List project tags for the agent",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.ProjectTagOption]{
				Method:         http.MethodGet,
				Path:           clitypes.ProjectsTags(c.EnvID()),
				FailureMessage: "failed to list project tags",
				Render: func(data []types.ProjectTagOption) error {
					headers := []string{"NAME", "COLOR"}
					rows := make([][]string, len(data))
					for i, tag := range data {
						rows[i] = []string{tag.Name, string(tag.Color)}
					}
					return cmdutil.PrintTable(cmd, headers, rows)
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeMerged, cmd)
	return cmd
}

func newTagCmd() *cobra.Command {
	var opts struct {
		tagAdd    string
		tagRemove string
		tagColor  string
	}
	cmd := &cobra.Command{
		Use:          "tag <project-id|name>",
		Short:        "Attach or detach a project tag",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if (opts.tagAdd == "") == (opts.tagRemove == "") {
				return errors.New("exactly one of --add or --remove is required")
			}
			if opts.tagRemove != "" && opts.tagColor != "" {
				return errors.New("--color can only be used with --add")
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := ProjectRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			body := types.UpdateProjectTag{
				Name:     cmp.Or(opts.tagRemove, opts.tagAdd),
				Attached: opts.tagAdd != "",
				Color:    types.ProjectTagColor(opts.tagColor),
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.UpdateProjectTagResponse]{
				Method:         http.MethodPatch,
				Path:           clitypes.ProjectTags(c.EnvID(), resolved.ID),
				Body:           body,
				FailureMessage: "failed to update project tag",
				Render: func(data types.UpdateProjectTagResponse) error {
					names := make([]string, len(data.Tags))
					for i, tag := range data.Tags {
						names[i] = tag.Name
					}
					output.Success("Tags updated for project %s", resolved.Name)
					output.KeyValue("Tags", strings.Join(names, ", "))
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.tagAdd, "add", "", "Tag name to attach")
	cmd.Flags().StringVar(&opts.tagRemove, "remove", "", "Tag name to detach")
	cmd.Flags().StringVar(&opts.tagColor, "color", "", "Tag color (used with --add)")
	return cmd
}

func newArchiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "archive <project-id|name>",
		Short:        "Archive a stopped project",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			resolved, _, err := ProjectRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.MessageResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.ProjectAction(c.EnvID(), resolved.ID, "archive", nil),
				FailureMessage: "failed to archive project",
				Render: func(types.MessageResponse) error {
					output.Success("%s", fmt.Sprintf("Project %s archived successfully", resolved.Name))
					return nil
				},
			})
		},
	}
}

func newUnarchiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "unarchive <project-id|name>",
		Short:        "Unarchive a project",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			resolved, _, err := ProjectRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.MessageResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.ProjectAction(c.EnvID(), resolved.ID, "unarchive", nil),
				FailureMessage: "failed to unarchive project",
				Render: func(types.MessageResponse) error {
					output.Success("%s", fmt.Sprintf("Project %s unarchived successfully", resolved.Name))
					return nil
				},
			})
		},
	}
}

func newBuildCmd() *cobra.Command {
	var opts struct {
		buildProvider string
		buildServices []string
		buildPush     bool
		buildLoad     bool
	}
	cmd := &cobra.Command{
		Use:          "build <project-id|name>",
		Short:        "Build project images",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectStreamAction(cmd, args[0], projectStreamActionConfig{
				endpoint:       clitypes.ProjectBuild,
				failureMessage: "failed to build project images",
				successMessage: "Images built successfully for project %s",
				body: types.ProjectBuildRequest{
					Services: opts.buildServices,
					Provider: opts.buildProvider,
					Push:     kit.Ternary(cmd.Flags().Changed("push"), &opts.buildPush, nil),
					Load:     kit.Ternary(cmd.Flags().Changed("load"), &opts.buildLoad, nil),
				},
			})
		},
	}
	cmd.Flags().StringArrayVar(&opts.buildServices, "service", nil, "Service to build (repeatable; all services with build directives when omitted)")
	cmdutil.EnumVarP(cmd, cmd.Flags(), &opts.buildProvider, "provider", "", "", "Build provider override", "local", "depot")
	cmd.Flags().BoolVar(&opts.buildPush, "push", false, "Push built images")
	cmd.Flags().BoolVar(&opts.buildLoad, "load", false, "Load built images into Docker")
	return cmd
}

func newRunJobCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "run-job <project-id|name> <job>",
		Short:        "Run a Compose job and stream its output",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectStreamAction(cmd, args[0], projectStreamActionConfig{
				endpoint: func(envID, projectID string) string {
					return clitypes.ProjectJobRun(envID, projectID, args[1])
				},
				failureMessage: "failed to run job " + args[1],
				successMessage: "Job " + args[1] + " completed successfully in project %s",
				unbounded:      true,
			})
		},
	}
}

func newUpdateServicesCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "upgrade <project-id|name> [services...]",
		Short:        "Update services that have a newer image (all services when none are given)",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := ProjectRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			c.SetTimeout(30 * time.Minute)
			options, err := upgradeOptions(cmd, c, resolved.ID, args[1:])
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.UpdaterResult]{
				Method:         http.MethodPost,
				Path:           clitypes.UpdaterRun(c.EnvID()),
				Body:           options,
				FailureMessage: "failed to update project services",
				Timeout:        30 * time.Minute,
				Render:         updater.RenderResult,
				Check:          updater.CheckResult,
			})
		},
	}
}

// upgradeOptions scopes the updater run like the UI: a project update runs after the project's update check,
// and named services are updated through their containers.
func upgradeOptions(cmd *cobra.Command, c *client.Client, projectID string, services []string) (types.UpdaterOptions, error) {
	if len(services) == 0 {
		check, err := c.PostJSON[types.ProjectUpdateInfo](cmd.Context(), clitypes.UpdaterProjectCheck(c.EnvID(), projectID), struct{}{})
		if err != nil {
			return types.UpdaterOptions{}, fmt.Errorf("failed to check project updates: %w", err)
		}
		if check.Data.ErrorCount > 0 {
			output.Warning("Update check failed for %d image(s): %s", check.Data.ErrorCount, kit.FromPtr(check.Data.ErrorMessage))
		}
		return types.UpdaterOptions{Type: "project", ResourceIds: []string{projectID}}, nil
	}

	type runtimeService struct {
		Name        string `json:"name"`
		ContainerID string `json:"containerId"`
	}
	// Decode only the service containers: compose service configs in this payload do not decode back into their types.
	runtime, err := c.GetJSON[struct {
		Name            string           `json:"name"`
		RuntimeServices []runtimeService `json:"runtimeServices"`
	}](cmd.Context(), clitypes.ProjectRuntime(c.EnvID(), projectID))
	if err != nil {
		return types.UpdaterOptions{}, fmt.Errorf("failed to load project services: %w", err)
	}
	containerIDs := make([]string, 0, len(services))
	for _, name := range kit.Unique(services) {
		// One container per service: updating it recreates every replica of the service.
		index := slices.IndexFunc(runtime.Data.RuntimeServices, func(service runtimeService) bool {
			return service.Name == name && service.ContainerID != ""
		})
		if index < 0 {
			return types.UpdaterOptions{}, fmt.Errorf("service %s has no container in project %s", name, runtime.Data.Name)
		}
		containerIDs = append(containerIDs, runtime.Data.RuntimeServices[index].ContainerID)
	}
	return types.UpdaterOptions{Type: "container", ResourceIds: containerIDs}, nil
}

func newWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Work with project workspace files",
	}
	cmd.AddCommand(
		newWorkspaceCatCmd(),
		newWorkspaceDownloadCmd(),
		newUpdateIncludesCmd(),
	)
	return cmd
}

func newWorkspaceCatCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "cat <project-id|name> <path>",
		Short:        "Print a project workspace file",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := ProjectRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			reqPath := clitypes.ProjectWorkspaceFile(c.EnvID(), resolved.ID) + "?relativePath=" + url.QueryEscape(args[1])
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.WorkspaceFileContent]{
				Method:         http.MethodGet,
				Path:           reqPath,
				FailureMessage: "failed to get workspace file",
				Render: func(data types.WorkspaceFileContent) error {
					if data.Content == "" && data.ReadOnlyReason != "" {
						return fmt.Errorf("file %s has no inline content (%s); use `arcane projects workspace download` instead", args[1], data.ReadOnlyReason)
					}
					_, _ = fmt.Fprint(output.Stdout(), data.Content)
					return nil
				},
			})
		},
	}
}

func newWorkspaceDownloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "download <project-id|name> <path> [output-file]",
		Short:        "Download a project workspace file",
		Args:         cobra.RangeArgs(2, 3),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := ProjectRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			reqPath := clitypes.ProjectWorkspaceFileDownload(c.EnvID(), resolved.ID) + "?relativePath=" + url.QueryEscape(args[1])
			resp, err := c.Request(cmd.Context(), http.MethodGet, reqPath, nil)
			if err != nil {
				return fmt.Errorf("failed to download workspace file: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if ensureSuccessStatusErr := cmdutil.EnsureSuccessStatus(resp); ensureSuccessStatusErr != nil {
				return fmt.Errorf("failed to download workspace file: %w", ensureSuccessStatusErr)
			}

			outputFile := path.Base(args[1])
			if len(args) == 3 {
				outputFile = args[2]
			}

			outputPath, err := filepath.Abs(outputFile)
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", outputFile, err)
			}
			file, err := acfs.Host{Special: true}.OpenFile(cmd.Context(), outputPath, acfs.OpenOptions{
				Mode:     0o666,
				Write:    true,
				Create:   true,
				Truncate: true,
			})
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", outputFile, err)
			}
			written, copyErr := output.Copy(cmd.Context(), file, resp.Body, output.TransferOptions{
				Total:        resp.ContentLength,
				Label:        "Downloading workspace file",
				ShowProgress: !cmdutil.JSONOutputEnabled(cmd),
			})
			closeErr := file.Close()
			if copyErr != nil {
				return fmt.Errorf("failed to write workspace file: %w", copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("failed to write workspace file: %w", closeErr)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(struct {
					Path  string `json:"path"`
					Bytes int64  `json:"bytes"`
				}{outputFile, written})
			}
			output.Success("Downloaded %s (%d bytes) to %s", args[1], written, outputFile)
			return nil
		},
	}
}

func newExportCmd() *cobra.Command {
	var opts struct {
		volumes    []string
		allVolumes bool
		stop       bool
	}
	cmd := &cobra.Command{
		Use:          "export <project-id|name> [output-file]",
		Short:        "Download a project, optionally with the data of its Docker-managed volumes, as a tar.gz archive",
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.stop && !opts.allVolumes && len(opts.volumes) == 0 {
				return errors.New("--stop requires --volume or --all-volumes")
			}
			if len(args) == 2 {
				if info, statErr := os.Stat(args[1]); statErr == nil && info.IsDir() {
					return fmt.Errorf("output path %s is a directory", args[1])
				}
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			c.SetTimeout(30 * time.Minute)

			resolved, _, err := ProjectRef.Resolve(cmd.Context(), c, args[0], cmdutil.AllowPrompt(cmd))
			if err != nil {
				return err
			}
			volumes := opts.volumes
			if opts.allVolumes {
				options, optionsErr := c.GetJSON[types.ProjectExportOptions](cmd.Context(), clitypes.ProjectExport(c.EnvID(), resolved.ID))
				if optionsErr != nil {
					return fmt.Errorf("failed to list project volumes: %w", optionsErr)
				}
				for _, volume := range options.Data.Volumes {
					if volume.Included && !slices.Contains(volumes, volume.Key) {
						volumes = append(volumes, volume.Key)
					}
				}
			}
			exported, err := c.PostJSON[types.UploadSession](cmd.Context(), clitypes.ProjectExport(c.EnvID(), resolved.ID), types.ExportProjectArchive{
				Volumes:     volumes,
				StopProject: opts.stop,
			})
			if err != nil {
				return fmt.Errorf("failed to export project: %w", err)
			}
			if len(volumes) > 0 && !strings.HasSuffix(exported.Data.Filename, types.ProjectBundleExtension) {
				cmdutil.AbortUploadSession(cmd.Context(), c, exported.Data.Kind, exported.Data.ID)
				return errors.New("the server does not support exporting volumes; upgrade Arcane")
			}

			outputFile := filepath.Base(exported.Data.Filename)
			if len(args) == 2 {
				outputFile = args[1]
			}
			info, statErr := os.Stat(outputFile)
			if statErr == nil && info.IsDir() {
				cmdutil.AbortUploadSession(cmd.Context(), c, exported.Data.Kind, exported.Data.ID)
				return fmt.Errorf("output path %s is a directory", outputFile)
			}
			special := statErr == nil && !info.Mode().IsRegular()
			written, err := cmdutil.DownloadUploadSession(cmd.Context(), c, exported.Data, cmdutil.DownloadOptions{
				OutputPath:   outputFile,
				Special:      special,
				ShowProgress: !cmdutil.JSONOutputEnabled(cmd),
			})
			if err != nil {
				return fmt.Errorf("failed to download project archive: %w", err)
			}
			if special {
				return nil
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(struct {
					Path    string   `json:"path"`
					Volumes []string `json:"volumes"`
					Bytes   int64    `json:"bytes"`
				}{
					Path:    outputFile,
					Volumes: volumes,
					Bytes:   written,
				})
			}
			output.Success("Saved %s (%s)", outputFile, output.Bytes(written))
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&opts.volumes, "volume", nil, "Compose key of a named or local external volume whose data to include (repeatable)")
	cmd.Flags().BoolVar(&opts.allVolumes, "all-volumes", false, "Include the data of every volume the export includes by default")
	cmd.Flags().BoolVar(&opts.stop, "stop", false, "Stop a running project while its volumes are exported, then start it again")
	return cmd
}

func newImportCmd() *cobra.Command {
	var opts struct {
		name  string
		start bool
	}
	cmd := &cobra.Command{
		Use:          "import <file>",
		Short:        "Create a project from an exported archive, restoring the volume data it holds",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			c.SetTimeout(30 * time.Minute)

			name := filepath.Base(args[0])
			for _, suffix := range []string{types.ProjectBundleExtension, ".tar.gz", ".tgz"} {
				if trimmed, ok := strings.CutSuffix(name, suffix); ok {
					name = trimmed
					break
				}
			}
			name = cmp.Or(opts.name, name)

			sessionID, err := cmdutil.UploadFileInChunks(cmd.Context(), c, cmdutil.UploadOptions{
				Kind:         types.UploadKindProjectArchive,
				FilePath:     args[0],
				ShowProgress: !cmdutil.JSONOutputEnabled(cmd),
			})
			if err != nil {
				return err
			}
			importErr := cmdutil.Run(cmd, c, cmdutil.Spec[types.CreateProjectResponse]{
				Method: http.MethodPost,
				Path:   clitypes.ProjectImport(c.EnvID()),
				Body: types.ImportProjectArchive{
					UploadID: sessionID,
					Name:     name,
					Start:    opts.start,
				},
				FailureMessage: "failed to import project",
				Render: func(data types.CreateProjectResponse) error {
					output.Success("Project %s imported%s", data.Name, kit.Ternary(opts.start, " and started", ""))
					output.KeyValue("ID", data.ID)
					return nil
				},
			})
			if importErr != nil {
				cmdutil.AbortUploadSession(cmd.Context(), c, types.UploadKindProjectArchive, sessionID)
			}
			return importErr
		},
	}
	cmd.Flags().StringVar(&opts.name, "name", "", "Name of the new project (default: the archive's file name)")
	cmd.Flags().BoolVar(&opts.start, "start", false, "Deploy the project after the import")
	cmdutil.SetScope(cmdutil.ScopeDestination, cmd)
	return cmd
}
