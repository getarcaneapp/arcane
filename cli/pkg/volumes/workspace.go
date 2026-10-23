package volumes

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func newWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "workspace <volume-name> [path]",
		Short:        "Browse volume workspace files",
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			allowPrompt := cmdutil.AllowPrompt(cmd)
			resolved, _, err := volumeRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			result, err := c.GetJSON[types.Workspace](cmd.Context(), clitypes.VolumeWorkspace(c.EnvID(), resolved.Name))
			if err != nil {
				return fmt.Errorf("failed to get volume workspace: %w", err)
			}

			// The endpoint returns the full tree; an optional path argument
			// narrows the listing to that subtree.
			entries := result.Data.Files
			if len(args) == 2 {
				prefix := strings.Trim(args[1], "/")
				filtered := entries[:0:0]
				for _, entry := range entries {
					if entry.RelativePath == prefix || strings.HasPrefix(entry.RelativePath, prefix+"/") {
						filtered = append(filtered, entry)
					}
				}
				entries = filtered
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(entries)
			}

			headers := []string{"PATH", "TYPE", "SIZE", "MODIFIED"}
			rows := make([][]string, len(entries))
			for i, entry := range entries {
				entryType := "file"
				switch {
				case entry.IsDirectory:
					entryType = "dir"
				case entry.IsSymlink:
					entryType = "symlink"
				}
				rows[i] = []string{
					entry.RelativePath,
					entryType,
					kit.Ternary(entry.IsDirectory, "", output.Bytes(entry.Size)),
					entry.ModTime.Format(time.RFC3339),
				}
			}
			if tableErr := output.Table(headers, rows); tableErr != nil {
				return tableErr
			}
			if result.Data.FileTreeTruncated {
				output.Warning("File listing was truncated by the server")
			}
			return nil
		},
	}
	cmd.AddCommand(
		newWorkspaceCatCmd(),
		newWorkspaceDownloadCmd(),
		newWorkspacePutCmd(),
	)
	return cmd
}

func newWorkspaceCatCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "cat <volume-name> <path>",
		Short:        "Print a volume workspace file",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			allowPrompt := cmdutil.AllowPrompt(cmd)
			resolved, _, err := volumeRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			filePath := cmdutil.AppendQuery(
				clitypes.VolumeWorkspaceFile(c.EnvID(), resolved.Name),
				url.Values{"relativePath": []string{args[1]}},
			)
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.WorkspaceFileContent]{
				Method:         http.MethodGet,
				Path:           filePath,
				FailureMessage: "failed to get workspace file",
				Render: func(data types.WorkspaceFileContent) error {
					if data.Content == "" && data.ReadOnlyReason != "" {
						return fmt.Errorf("file %s has no printable content (%s); use `arcane volumes workspace download` instead", args[1], data.ReadOnlyReason)
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
		Use:          "download <volume-name> <path> [output-file]",
		Short:        "Download a volume workspace file",
		Args:         cobra.RangeArgs(2, 3),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			allowPrompt := cmdutil.AllowPrompt(cmd)
			resolved, _, err := volumeRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			// Downloading large files can take a long time
			c.SetTimeout(30 * time.Minute)

			downloadPath := cmdutil.AppendQuery(
				clitypes.VolumeWorkspaceFileDownload(c.EnvID(), resolved.Name),
				url.Values{"relativePath": []string{args[1]}},
			)
			resp, err := c.Get(cmd.Context(), downloadPath)
			if err != nil {
				return fmt.Errorf("failed to download workspace file: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if ensureSuccessStatusErr := cmdutil.EnsureSuccessStatus(resp); ensureSuccessStatusErr != nil {
				return fmt.Errorf("failed to download workspace file: %w", ensureSuccessStatusErr)
			}

			outputFile := ""
			if len(args) == 3 {
				outputFile = args[2]
			}
			if outputFile == "" {
				outputFile = path.Base(args[1])
				if _, params, parseErr := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); parseErr == nil {
					outputFile = cmp.Or(params["filename"], outputFile)
				}
			}

			file, openErr := os.OpenFile(outputFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
			if openErr != nil {
				return fmt.Errorf("failed to create file %s: %w", outputFile, openErr)
			}
			written, copyErr := output.Copy(cmd.Context(), file, resp.Body, resp.ContentLength, "Downloading workspace file", !cmdutil.JSONOutputEnabled(cmd))
			if copyErr != nil {
				_ = file.Close()
				return fmt.Errorf("failed to write file %s: %w", outputFile, copyErr)
			}
			if closeErr := file.Close(); closeErr != nil {
				return fmt.Errorf("failed to write file %s: %w", outputFile, closeErr)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(struct {
					Path  string `json:"path"`
					Bytes int64  `json:"bytes"`
				}{outputFile, written})
			}
			output.Success("File downloaded to %s", outputFile)
			return nil
		},
	}
}

func newWorkspacePutCmd() *cobra.Command {
	var opts struct {
		forceFlag bool
	}
	cmd := &cobra.Command{
		Use:          "put <volume-name> <local-file> <path>",
		Short:        "Write a local file into a volume workspace",
		Args:         cobra.ExactArgs(3),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			allowPrompt := !opts.forceFlag && cmdutil.AllowPrompt(cmd)
			resolved, _, err := volumeRef.Resolve(cmd.Context(), c, args[0], allowPrompt)
			if err != nil {
				return err
			}

			content, err := os.ReadFile(args[1])
			if err != nil {
				return fmt.Errorf("failed to read local file: %w", err)
			}

			relativePath := strings.Trim(filepath.ToSlash(args[2]), "/")
			if relativePath == "" {
				return errors.New("destination path is required")
			}

			confirmed, confirmationErr := cmdutil.ConfirmOrCancel(cmd, opts.forceFlag, fmt.Sprintf("Write %s to %s in volume %s?", args[1], relativePath, resolved.Name))
			if confirmationErr != nil || !confirmed {
				return confirmationErr
			}

			current, err := c.GetJSON[types.Workspace](cmd.Context(), clitypes.VolumeWorkspace(c.EnvID(), resolved.Name))
			if err != nil {
				return fmt.Errorf("failed to get volume workspace: %w", err)
			}

			operation := types.FileOpCreateFile
			if slices.ContainsFunc(current.Data.Files, func(entry types.WorkspaceFileEntry) bool {
				return entry.RelativePath == relativePath && !entry.IsDirectory
			}) {
				operation = types.FileOpUpdateFile
			}

			uploadIndex := 0
			manifest := types.VolumeWorkspaceUpdateManifest{
				FileTreeRevision: current.Data.FileTreeRevision,
				FileChanges: []types.VolumeWorkspaceFileChange{{
					Operation:    operation,
					RelativePath: relativePath,
					UploadIndex:  &uploadIndex,
				}},
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
			filePart, err := writer.CreateFormFile("files", path.Base(relativePath))
			if err != nil {
				return fmt.Errorf("failed to create workspace upload: %w", err)
			}
			if _, writeErr := filePart.Write(content); writeErr != nil {
				return fmt.Errorf("failed to write workspace upload: %w", writeErr)
			}
			if closeErr := writer.Close(); closeErr != nil {
				return fmt.Errorf("failed to finalize workspace upload: %w", closeErr)
			}

			resp, err := c.RequestRaw(cmd.Context(), http.MethodPatch, clitypes.VolumeWorkspace(c.EnvID(), resolved.Name), &requestBody, map[string]string{"Content-Type": writer.FormDataContentType()})
			if err != nil {
				return fmt.Errorf("failed to update volume workspace: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			responseBody, readErr := cmdutil.ReadJSONBody(resp)
			if readErr != nil {
				return fmt.Errorf("failed to update volume workspace: %w", readErr)
			}
			var result types.ApiResponse[jsontext.Value]
			if decodeErr := json.Unmarshal(responseBody, &result); decodeErr != nil {
				return fmt.Errorf("failed to parse response: %w", decodeErr)
			}
			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(result.Data)
			}

			output.Success("File %s written to volume %s", relativePath, resolved.Name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&opts.forceFlag, "force", "f", false, "Write without confirmation")
	return cmd
}
