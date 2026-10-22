package cmdutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/client"
	"github.com/getarcaneapp/arcane/cli/v3/internal/logger"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

const uploadChunkAttempts = 3

// UploadFileInChunks sends retried chunks with optional progress and aborts the session on failure.
// It returns the completed upload session ID.
func UploadFileInChunks(ctx context.Context, c *client.Client, kind, filePath string, showProgress bool) (string, error) {
	log := logger.GetLogger()

	file, err := os.Open(filePath)
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

// AbortUploadSession deletes a chunked upload session, ignoring errors. Use it
// when the consume step after a completed upload fails.
func AbortUploadSession(ctx context.Context, c *client.Client, kind, sessionID string) {
	if resp, err := c.Delete(context.WithoutCancel(ctx), clitypes.UploadSession(c.EnvID(), kind, sessionID)); err == nil {
		_ = resp.Body.Close()
	}
}
