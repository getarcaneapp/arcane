package gitops

import (
	"cmp"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"kit.getarcane.app"

	"getarcane.dev/cli/v3/internal/cmdutil"
	"getarcane.dev/cli/v3/internal/output"
	clitypes "getarcane.dev/cli/v3/internal/types"
	"getarcane.dev/types/v3"
)

type (
	historyOptions struct {
		limit int
	}

	resolveOptions struct {
		strategy string
		force    bool
	}
)

func newPreviewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "preview <sync-id|name>",
		Short:        "Preview the files the next push to Git would change",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE:         runPreview,
	}
	cmdutil.SetResource(gitopsSyncRef, cmd)
	return cmd
}

func newHistoryCmd() *cobra.Command {
	var opts historyOptions
	cmd := &cobra.Command{
		Use:          "history <sync-id|name>",
		Short:        "List Git revisions that changed the pushed project files",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE:         opts.run,
	}
	cmd.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of revisions to show (max 200)")
	cmdutil.SetResource(gitopsSyncRef, cmd)
	return cmd
}

func newResolveCmd() *cobra.Command {
	var opts resolveOptions
	cmd := &cobra.Command{
		Use:          "resolve <sync-id|name>",
		Short:        "Resolve a push conflict that needs attention",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE:         opts.run,
	}
	cmdutil.EnumVarP(cmd, cmd.Flags(), &opts.strategy, "strategy", "", types.GitOpsBackupConflictUseArcane, "How to resolve the conflict", types.GitOpsBackupConflictUseArcane)
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Resolve without confirmation")
	cmdutil.SetResource(gitopsSyncRef, cmd)
	return cmd
}

func runPreview(cmd *cobra.Command, args []string) error {
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return err
	}

	resolved, _, err := gitopsSyncRef.Resolve(cmd.Context(), c, args[0], false)
	if err != nil {
		return err
	}

	return cmdutil.Run(cmd, c, cmdutil.Spec[types.GitOpsBackupPreview]{
		Method:         http.MethodGet,
		Path:           clitypes.GitOpsSyncBackupPreview(c.EnvID(), resolved.ID),
		FailureMessage: "failed to preview gitops push",
		Render: func(data types.GitOpsBackupPreview) error {
			output.Header("GitOps Push Preview")
			output.KeyValue("State", data.State)
			output.KeyValue("Remote Commit", cmp.Or(data.RemoteCommit, "-"))
			rows := make([][]string, 0, len(data.Changes)+len(data.Conflicts))
			for _, change := range data.Changes {
				rows = append(rows, []string{change.Path, change.Change, "false"})
			}
			for _, conflict := range data.Conflicts {
				rows = append(rows, []string{conflict.Path, conflict.Change, "true"})
			}
			return output.Table([]string{"PATH", "CHANGE", "CONFLICT"}, rows)
		},
	})
}

func (o *historyOptions) run(cmd *cobra.Command, args []string) error {
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return err
	}

	resolved, _, err := gitopsSyncRef.Resolve(cmd.Context(), c, args[0], false)
	if err != nil {
		return err
	}

	return cmdutil.Run(cmd, c, cmdutil.Spec[types.GitOpsBackupHistoryResponse]{
		Method:         http.MethodGet,
		Path:           cmdutil.AppendQuery(clitypes.GitOpsSyncBackupHistory(c.EnvID(), resolved.ID), url.Values{"limit": []string{strconv.Itoa(o.limit)}}),
		FailureMessage: "failed to list gitops push history",
		Render: func(data types.GitOpsBackupHistoryResponse) error {
			rows := make([][]string, len(data.Entries))
			for i, entry := range data.Entries {
				rows[i] = []string{
					entry.Commit[:min(len(entry.Commit), 12)],
					entry.Date.Format("2006-01-02 15:04:05"),
					entry.Author,
					entry.Message,
					strconv.Itoa(len(entry.Files)),
				}
			}
			return output.Table([]string{"COMMIT", "DATE", "AUTHOR", "MESSAGE", "FILES"}, rows)
		},
	})
}

func (o *resolveOptions) run(cmd *cobra.Command, args []string) error {
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return err
	}

	resolved, _, err := gitopsSyncRef.Resolve(cmd.Context(), c, args[0], false)
	if err != nil {
		return err
	}

	confirmed, err := cmdutil.ConfirmOrCancel(cmd, cmdutil.ConfirmOptions{
		Label: fmt.Sprintf("Overwrite the conflicting files in Git with the Arcane copies for gitops sync %s?", cmp.Or(resolved.Name, resolved.ID)),
		Force: o.force,
	})
	if err != nil || !confirmed {
		return err
	}

	return cmdutil.Run(cmd, c, cmdutil.Spec[types.GitOpsSyncResult]{
		Method:         http.MethodPost,
		Path:           clitypes.GitOpsSyncBackupResolve(c.EnvID(), resolved.ID),
		Body:           types.GitOpsResolveBackupConflictRequest{Strategy: o.strategy},
		FailureMessage: "failed to resolve gitops push conflict",
		Render: func(data types.GitOpsSyncResult) error {
			if data.Success {
				output.Success("Conflict resolved: %s", data.Message)
				return nil
			}
			return fmt.Errorf("resolve failed: %s", strings.TrimSpace(data.Message+" "+kit.FromPtr(data.Error)))
		},
	})
}
