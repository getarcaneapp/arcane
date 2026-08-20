package cmdutil

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/logger"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	uploadtypes "github.com/getarcaneapp/arcane/types/v2/upload"
)

const uploadChunkAttempts = 3

// UploadFileInChunks streams a local file to the server through a chunked
// upload session of the given kind and returns the completed session ID. The
// file is sent as independently retried chunks so reverse-proxy body limits
// never see the full size. On failure the session is deleted server-side.
// When showProgress is true a progress bar is rendered.
func UploadFileInChunks(ctx context.Context, c *client.Client, kind, filePath string, showProgress bool) (string, error) {
	log := logger.GetLogger()

	file, err := os.Open(filePath)
	if err != nil {
		return "", errors.WrapIf(err, "failed to open file")
	}
	defer func() { _ = file.Close() }()
	fileInfo, err := file.Stat()
	if err != nil {
		return "", errors.WrapIf(err, "failed to stat file")
	}

	created, err := c.PostJSON[uploadtypes.Session](ctx, types.UploadSessions(c.EnvID(), kind), uploadtypes.CreateSessionRequest{
		Filename: filepath.Base(filePath),
		Size:     fileInfo.Size(),
	})
	if err != nil {
		return "", errors.WrapIf(err, "failed to create upload session")
	}
	session := created.Data

	log.Debugf("Uploading %s as %d chunks of %d bytes (session %s)", filePath, session.TotalChunks, session.ChunkSize, session.ID)

	var progressUI *output.Progress
	if showProgress {
		progressUI = output.StartProgress("Uploading", fileInfo.Size())
		defer progressUI.Stop()
	}

	deleteSession := func() {
		if resp, deleteErr := c.Delete(context.WithoutCancel(ctx), types.UploadSession(c.EnvID(), kind, session.ID)); deleteErr == nil {
			_ = resp.Body.Close()
		}
	}

	buf := make([]byte, session.ChunkSize)
	for index := range session.TotalChunks {
		expected := session.ChunkSize
		if index == session.TotalChunks-1 {
			expected = session.Size - int64(index)*session.ChunkSize
		}
		if _, err := io.ReadFull(file, buf[:expected]); err != nil {
			deleteSession()
			return "", errors.WrapIf(err, "failed to read file")
		}

		chunkPath := types.UploadSessionChunk(c.EnvID(), kind, session.ID, index)
		for attempt := 1; ; attempt++ {
			resp, chunkErr := c.RequestRaw(ctx, http.MethodPut, chunkPath, bytes.NewReader(buf[:expected]), map[string]string{"Content-Type": "application/octet-stream"})
			if chunkErr == nil {
				ok := resp.StatusCode >= 200 && resp.StatusCode < 300
				if !ok {
					errorBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
					chunkErr = errors.Errorf("chunk %d failed (status %d): %s", index, resp.StatusCode, strings.TrimSpace(string(errorBody)))
				}
				_ = resp.Body.Close()
				if ok {
					break
				}
			}
			if attempt >= uploadChunkAttempts {
				deleteSession()
				return "", errors.WrapIf(chunkErr, "failed to upload file")
			}
			log.Debugf("Retrying chunk %d after error: %v", index, chunkErr)
		}
		if progressUI != nil {
			progressUI.Add(expected)
		}
	}

	return session.ID, nil
}

// AbortUploadSession deletes a chunked upload session, ignoring errors. Use it
// when the consume step after a completed upload fails.
func AbortUploadSession(ctx context.Context, c *client.Client, kind, sessionID string) {
	if resp, err := c.Delete(context.WithoutCancel(ctx), types.UploadSession(c.EnvID(), kind, sessionID)); err == nil {
		_ = resp.Body.Close()
	}
}
