package cmdutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	charmLog "charm.land/log/v2"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"

	"getarcane.dev/cli/v3/internal/client"
	"getarcane.dev/cli/v3/internal/output"
	clitypes "getarcane.dev/cli/v3/internal/types"
	"getarcane.dev/types/v3"
)

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

// DownloadFileInChunks saves a complete upload session to outputPath chunk by chunk, then deletes the session.
func DownloadFileInChunks(ctx context.Context, c *client.Client, session types.UploadSession, outputPath string, showProgress bool) (written int64, err error) {
	defer AbortUploadSession(ctx, c, session.Kind, session.ID)

	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		return 0, fmt.Errorf("failed to create output file: %w", err)
	}
	file, err := acfs.Host{Special: true}.OpenFile(ctx, absPath, acfs.OpenOptions{Mode: 0o666, Write: true, Create: true, Truncate: true})
	if err != nil {
		return 0, fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to write output file: %w", closeErr)
		}
		if err != nil {
			_ = acfs.Host{}.Remove(context.WithoutCancel(ctx), absPath)
		}
	}()

	loading := output.StartTracker(ctx, output.Stderr(), showProgress, "Downloading chunks")
	loading.NextStep()
	defer loading.Stop()
	loading.SetProgress("", "Downloading", 0, session.Size, true)
	for index := range session.TotalChunks {
		var chunk []byte
		for attempt := 1; ; attempt++ {
			chunk, err = readChunk(ctx, c, clitypes.UploadSessionChunk(c.EnvID(), session.Kind, session.ID, index), session.ChunkSize)
			if err == nil {
				break
			}
			if attempt >= uploadChunkAttempts {
				return written, fmt.Errorf("failed to download file: %w", err)
			}
			charmLog.Default().Debugf("Retrying chunk %d after error: %v", index, err)
		}
		if _, err = file.Write(chunk); err != nil {
			return written, fmt.Errorf("failed to write output file: %w", err)
		}
		written += int64(len(chunk))
		loading.SetProgress("", "Downloading", written, session.Size, true)
	}
	if written != session.Size {
		return written, fmt.Errorf("downloaded %d of %d bytes", written, session.Size)
	}
	loading.Finish("", nil)
	return written, nil
}

func readChunk(ctx context.Context, c *client.Client, chunkPath string, chunkSize int64) ([]byte, error) {
	resp, err := c.RequestRaw(ctx, http.MethodGet, chunkPath, nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if statusErr := EnsureSuccessStatus(resp); statusErr != nil {
		return nil, statusErr
	}
	chunk, err := io.ReadAll(io.LimitReader(resp.Body, chunkSize+1))
	if err == nil && int64(len(chunk)) > chunkSize {
		err = fmt.Errorf("chunk is larger than %d bytes", chunkSize)
	}
	return chunk, err
}
