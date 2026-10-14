package projects

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
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

func newTagsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "tags",
		Short:        "List project tags for the environment",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[[]project.TagOption]{
				Method:         http.MethodGet,
				Path:           types.ProjectsTags(c.EnvID()),
				FailureMessage: "failed to list project tags",
				Render: func(data []project.TagOption) error {
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

			resolved, _, err := projectRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			body := project.UpdateTag{
				Name:     cmp.Or(opts.tagRemove, opts.tagAdd),
				Attached: opts.tagAdd != "",
				Color:    project.TagColor(opts.tagColor),
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[project.UpdateTagResponse]{
				Method:         http.MethodPatch,
				Path:           types.ProjectTags(c.EnvID(), resolved.ID),
				Body:           body,
				FailureMessage: "failed to update project tag",
				Render: func(data project.UpdateTagResponse) error {
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
			resolved, _, err := projectRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[base.MessageResponse]{
				Method:         http.MethodPost,
				Path:           types.ProjectArchive(c.EnvID(), resolved.ID),
				FailureMessage: "failed to archive project",
				Render: func(base.MessageResponse) error {
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
			resolved, _, err := projectRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[base.MessageResponse]{
				Method:         http.MethodPost,
				Path:           types.ProjectUnarchive(c.EnvID(), resolved.ID),
				FailureMessage: "failed to unarchive project",
				Render: func(base.MessageResponse) error {
					output.Success("%s", fmt.Sprintf("Project %s unarchived successfully", resolved.Name))
					return nil
				},
			})
		},
	}
}

func newBuildCmd() *cobra.Command {
	var opts struct {
		buildServices []string
		buildProvider string
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
				endpoint:       types.ProjectBuild,
				failureMessage: "failed to build project images",
				successMessage: "Images built successfully for project %s",
				body: project.BuildRequest{
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

func newUpdateServicesCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "upgrade <project-id|name> [services...]",
		Short:        "Pull latest images and recreate services (all services when none are given)",
		Args:         cobra.MinimumNArgs(1),
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

			c.SetTimeout(30 * time.Minute)

			body := project.UpdateServicesRequest{Services: args[1:]}

			return cmdutil.Run(cmd, c, cmdutil.Spec[base.MessageResponse]{
				Method:         http.MethodPost,
				Path:           types.ProjectUpdateServices(c.EnvID(), resolved.ID),
				Body:           body,
				FailureMessage: "failed to update project services",
				Render: func(base.MessageResponse) error {
					output.Success("Services updated successfully for project %s", resolved.Name)
					return nil
				},
			})
		},
	}
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

			resolved, _, err := projectRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			reqPath := types.ProjectWorkspaceFile(c.EnvID(), resolved.ID) + "?relativePath=" + url.QueryEscape(args[1])
			return cmdutil.Run(cmd, c, cmdutil.Spec[workspace.FileContent]{
				Method:         http.MethodGet,
				Path:           reqPath,
				FailureMessage: "failed to get workspace file",
				Render: func(data workspace.FileContent) error {
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

			resolved, _, err := projectRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			reqPath := types.ProjectWorkspaceFileDownload(c.EnvID(), resolved.ID) + "?relativePath=" + url.QueryEscape(args[1])
			resp, err := c.Get(cmd.Context(), reqPath)
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

			file, err := os.Create(outputFile)
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", outputFile, err)
			}
			written, copyErr := output.Copy(cmd.Context(), file, resp.Body, resp.ContentLength, "Downloading workspace file", !cmdutil.JSONOutputEnabled(cmd))
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
