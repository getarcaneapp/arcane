package projects

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/project"
	workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"
	"github.com/spf13/cobra"
)

var (
	tagAdd    string
	tagRemove string
	tagColor  string

	buildServices []string
	buildProvider string
	buildPush     bool
	buildLoad     bool
)

var tagsCmd = &cobra.Command{
	Use:          "tags",
	Short:        "List project tags for the environment",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		result, err := c.GetJSON[[]project.TagOption](cmd.Context(), types.ProjectsTags(c.EnvID()))
		if err != nil {
			return errors.WrapIf(err, "failed to list project tags")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("Project Tags")
		headers := []string{"NAME", "COLOR"}
		rows := make([][]string, len(result.Data))
		for i, tag := range result.Data {
			rows[i] = []string{tag.Name, string(tag.Color)}
		}
		output.Table(headers, rows)
		return nil
	},
}

var tagCmd = &cobra.Command{
	Use:          "tag <project-id|name>",
	Short:        "Attach or detach a project tag",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if (tagAdd == "") == (tagRemove == "") {
			return errors.New("exactly one of --add or --remove is required")
		}
		if tagRemove != "" && tagColor != "" {
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
			Name:     tagAdd,
			Attached: tagAdd != "",
			Color:    project.TagColor(tagColor),
		}
		if tagRemove != "" {
			body.Name = tagRemove
		}

		result, err := c.DoJSON[base.ApiResponse[project.UpdateTagResponse]](cmd.Context(), http.MethodPatch, types.ProjectTags(c.EnvID(), resolved.ID), body)
		if err != nil {
			return errors.WrapIf(err, "failed to update project tag")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		names := make([]string, len(result.Data.Tags))
		for i, tag := range result.Data.Tags {
			names[i] = tag.Name
		}
		output.Success("Tags updated for project %s", resolved.Name)
		output.KeyValue("Tags", strings.Join(names, ", "))
		return nil
	},
}

var archiveCmd = &cobra.Command{
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
		return cmdutil.RunPostAction[base.MessageResponse](cmd, c, cmdutil.PostActionSpec{
			Path:           types.ProjectArchive(c.EnvID(), resolved.ID),
			FailureMessage: "failed to archive project",
			SuccessMessage: fmt.Sprintf("Project %s archived successfully", resolved.Name),
			JSON:           jsonOutput,
		})
	},
}

var unarchiveCmd = &cobra.Command{
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
		return cmdutil.RunPostAction[base.MessageResponse](cmd, c, cmdutil.PostActionSpec{
			Path:           types.ProjectUnarchive(c.EnvID(), resolved.ID),
			FailureMessage: "failed to unarchive project",
			SuccessMessage: fmt.Sprintf("Project %s unarchived successfully", resolved.Name),
			JSON:           jsonOutput,
		})
	},
}

var buildCmd = &cobra.Command{
	Use:          "build <project-id|name>",
	Short:        "Build project images",
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

		c.SetTimeout(30 * time.Minute)

		body := struct {
			Services []string `json:"services,omitempty"`
			Provider string   `json:"provider,omitempty"`
			Push     *bool    `json:"push,omitempty"`
			Load     *bool    `json:"load,omitempty"`
		}{Services: buildServices, Provider: buildProvider}
		if cmd.Flags().Changed("push") {
			body.Push = &buildPush
		}
		if cmd.Flags().Changed("load") {
			body.Load = &buildLoad
		}

		resp, err := c.Post(cmd.Context(), types.ProjectBuild(c.EnvID(), resolved.ID), body)
		if err != nil {
			return errors.WrapIf(err, "failed to build project images")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to build project images")
		}

		if err := printOperationStreamInternal(resp.Body); err != nil {
			return errors.WrapIf(err, "failed to build project images")
		}

		output.Success("Images built successfully for project %s", resolved.Name)
		return nil
	},
}

var updateServicesCmd = &cobra.Command{
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

		body := struct {
			Services []string `json:"services,omitempty"`
		}{Services: args[1:]}

		result, err := c.PostJSON[base.MessageResponse](cmd.Context(), types.ProjectUpdateServices(c.EnvID(), resolved.ID), body)
		if err != nil {
			return errors.WrapIf(err, "failed to update project services")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Services updated successfully for project %s", resolved.Name)
		return nil
	},
}

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Work with project workspace files",
}

var workspaceCatCmd = &cobra.Command{
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
		result, err := c.GetJSON[workspacetypes.FileContent](cmd.Context(), reqPath)
		if err != nil {
			return errors.WrapIf(err, "failed to get workspace file")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		if result.Data.Content == "" && result.Data.ReadOnlyReason != "" {
			return errors.Errorf("file %s has no inline content (%s); use `arcane projects workspace download` instead", args[1], result.Data.ReadOnlyReason)
		}
		fmt.Print(result.Data.Content)
		return nil
	},
}

var workspaceDownloadCmd = &cobra.Command{
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
			return errors.WrapIf(err, "failed to download workspace file")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to download workspace file")
		}

		outputFile := path.Base(args[1])
		if len(args) == 3 {
			outputFile = args[2]
		}

		file, err := os.Create(outputFile)
		if err != nil {
			return errors.WrapIff(err, "failed to create file %s", outputFile)
		}
		written, copyErr := io.Copy(file, resp.Body)
		closeErr := file.Close()
		if copyErr != nil {
			return errors.WrapIf(copyErr, "failed to write workspace file")
		}
		if closeErr != nil {
			return errors.WrapIf(closeErr, "failed to write workspace file")
		}

		output.Success("Downloaded %s (%d bytes) to %s", args[1], written, outputFile)
		return nil
	},
}

func init() {
	ProjectsCmd.AddCommand(tagsCmd)
	ProjectsCmd.AddCommand(tagCmd)
	ProjectsCmd.AddCommand(archiveCmd)
	ProjectsCmd.AddCommand(unarchiveCmd)
	ProjectsCmd.AddCommand(buildCmd)
	ProjectsCmd.AddCommand(updateServicesCmd)
	ProjectsCmd.AddCommand(workspaceCmd)
	workspaceCmd.AddCommand(workspaceCatCmd)
	workspaceCmd.AddCommand(workspaceDownloadCmd)

	tagCmd.Flags().StringVar(&tagAdd, "add", "", "Tag name to attach")
	tagCmd.Flags().StringVar(&tagRemove, "remove", "", "Tag name to detach")
	tagCmd.Flags().StringVar(&tagColor, "color", "", "Tag color (used with --add)")

	buildCmd.Flags().StringArrayVar(&buildServices, "service", nil, "Service to build (repeatable; all services with build directives when omitted)")
	buildCmd.Flags().StringVar(&buildProvider, "provider", "", "Build provider override")
	buildCmd.Flags().BoolVar(&buildPush, "push", false, "Push built images")
	buildCmd.Flags().BoolVar(&buildLoad, "load", false, "Load built images into Docker")

	tagsCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	tagCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	updateServicesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	workspaceCatCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
