package cmdutil

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	charmLog "charm.land/log/v2"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"

	"getarcane.dev/cli/v3/internal/client"
	"getarcane.dev/cli/v3/internal/output"
	clitypes "getarcane.dev/cli/v3/internal/types"
	"getarcane.dev/types/v3"
)

type DownloadOptions struct {
	OutputPath string
	// Path downloads from another endpoint than the upload session's own, such as a project export's.
	Path         string
	Special      bool
	ShowProgress bool
}

const (
	uploadChunkAttempts = 3
)

func UploadFileInChunks(ctx context.Context, c *client.Client, kind, filePath string, showProgress bool) (string, error) {
	log := charmLog.Default()

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	file, err := acfs.Host{}.OpenFile(ctx, absPath, acfs.OpenOptions{Read: true})
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()
	fileInfo, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}

	loading := output.StartTracker(ctx, output.Stderr(), showProgress, "Creating upload session", "Uploading chunks")
	loading.NextStep()
	defer loading.Stop()
	created, err := c.PostJSON[types.UploadSession](ctx, clitypes.UploadSessions(c.EnvID(), kind), types.CreateUploadSessionRequest{
		Filename: filepath.Base(filePath),
		Size:     fileInfo.Size(),
	})
	if err != nil {
		return "", fmt.Errorf("failed to create upload session: %w", err)
	}
	session := created.Data

	log.Debugf("Uploading %s as %d chunks of %d bytes (session %s)", filePath, session.TotalChunks, session.ChunkSize, session.ID)

	loading.NextStep()
	loading.SetProgress("", "Uploading", 0, fileInfo.Size(), true)
	var uploaded int64

	buf := make([]byte, session.ChunkSize)
	for index := range session.TotalChunks {
		expected := kit.Ternary(index == session.TotalChunks-1, session.Size-int64(index)*session.ChunkSize, session.ChunkSize)
		if _, readFullErr := io.ReadFull(file, buf[:expected]); readFullErr != nil {
			AbortUploadSession(ctx, c, kind, session.ID)
			return "", fmt.Errorf("failed to read file: %w", readFullErr)
		}

		chunkPath := clitypes.UploadSessionChunk(c.EnvID(), kind, session.ID, index)
		for attempt := 1; ; attempt++ {
			resp, chunkErr := c.RequestRaw(ctx, http.MethodPut, chunkPath, bytes.NewReader(buf[:expected]), map[string]string{"Content-Type": "application/octet-stream"})
			if chunkErr == nil {
				ok := resp.StatusCode >= 200 && resp.StatusCode < 300
				if !ok {
					errorBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
					chunkErr = fmt.Errorf("chunk %d failed (status %d): %s", index, resp.StatusCode, strings.TrimSpace(string(errorBody)))
				}
				_ = resp.Body.Close()
				if ok {
					break
				}
			}
			if attempt >= uploadChunkAttempts {
				AbortUploadSession(ctx, c, kind, session.ID)
				return "", fmt.Errorf("failed to upload file: %w", chunkErr)
			}
			log.Debugf("Retrying chunk %d after error: %v", index, chunkErr)
		}
		uploaded += expected
		loading.SetProgress("", "Uploading", uploaded, fileInfo.Size(), true)
	}

	loading.Finish("", nil)
	return session.ID, nil
}

func AbortUploadSession(ctx context.Context, c *client.Client, kind, sessionID string) {
	if resp, err := c.Request(context.WithoutCancel(ctx), http.MethodDelete, clitypes.UploadSession(c.EnvID(), kind, sessionID), nil); err == nil {
		_ = resp.Body.Close()
	}
}

func DownloadUploadSession(ctx context.Context, c *client.Client, session types.UploadSession, opts DownloadOptions) (written int64, err error) {
	defer AbortUploadSession(ctx, c, session.Kind, session.ID)

	resp, err := c.RequestRaw(ctx, http.MethodGet, cmp.Or(opts.Path, clitypes.UploadSessionDownload(c.EnvID(), session.Kind, session.ID)), nil, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to download file: %w", err)
	}
	defer func() {
		// The body is read to the end or the download already failed; closing it can't change the file written.
		_ = resp.Body.Close()
	}()
	if statusErr := EnsureSuccessStatus(resp); statusErr != nil {
		return 0, fmt.Errorf("failed to download file: %w", statusErr)
	}
	absPath, err := filepath.Abs(opts.OutputPath)
	if err != nil {
		return 0, fmt.Errorf("failed to create output file: %w", err)
	}
	if opts.Special {
		file, openErr := acfs.Host{Special: true}.OpenFile(ctx, absPath, acfs.OpenOptions{Write: true})
		if openErr != nil {
			return 0, fmt.Errorf("failed to open output file: %w", openErr)
		}
		defer func() {
			if closeErr := file.Close(); closeErr != nil && err == nil {
				err = fmt.Errorf("failed to write output file: %w", closeErr)
			}
		}()
		return copyUploadSession(ctx, file, resp.Body, session, opts.ShowProgress)
	}
	if entry, statErr := (acfs.Host{}).Stat(ctx, absPath, acfstypes.StatOptions{FollowFinal: true}); statErr == nil {
		absPath = entry.Path
	}
	file, tempPath, err := acfs.Host{}.CreateTemp(ctx, filepath.Dir(absPath), "."+filepath.Base(absPath)+".*.part")
	if err != nil {
		return 0, fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to write output file: %w", closeErr)
		}
		if err == nil {
			err = acfs.Host{}.Rename(ctx, tempPath, absPath)
		}
		// A partial file left behind is reported along with the failure.
		if err != nil {
			if removeErr := (acfs.Host{}).Remove(context.WithoutCancel(ctx), tempPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("remove partial download %s: %w", tempPath, removeErr))
			}
		}
	}()
	return copyUploadSession(ctx, file, resp.Body, session, opts.ShowProgress)
}

func copyUploadSession(ctx context.Context, file io.Writer, body io.Reader, session types.UploadSession, showProgress bool) (int64, error) {
	written, err := output.Copy(ctx, file, body, output.TransferOptions{
		Total:        session.Size,
		Label:        "Downloading",
		ShowProgress: showProgress,
	})
	if err != nil {
		return written, fmt.Errorf("failed to download file: %w", err)
	}
	if written != session.Size {
		return written, fmt.Errorf("downloaded %d of %d bytes", written, session.Size)
	}
	return written, nil
}
