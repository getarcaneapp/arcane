// Package upload owns chunked upload sessions: filesystem-backed staging for
// large files that arrive as independently retryable chunks, consumed by the
// image, volume-backup, and build-workspace endpoints.
package upload

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	uploadtypes "github.com/getarcaneapp/arcane/types/v2/upload"
	"go.getarcane.app/acfs"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

const (
	sessionMetaFilename = "meta.json"
	sessionDataFilename = "data"
)

// uploadIDPattern matches the random hex component acfs.MkdirTemp generates
// for session directories; anything else is rejected before touching disk.
var uploadIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// UploadService stages chunked upload sessions on local disk. Sessions are
// node-local by design: chunks and the consuming request must reach the same
// instance, which holds for Arcane's single-instance-per-node deployment (for
// remote environments the session lives on that environment's agent).
type UploadService struct {
	settingsService *settings.SettingsService
	root            string
	locks           utils.KeyedMutex
}

// NewUploadService builds the session service rooted in the system temp
// directory; the root is created lazily on first session.
func NewUploadService(settingsService *settings.SettingsService) *UploadService {
	return &UploadService{
		settingsService: settingsService,
		root:            filepath.Join(os.TempDir(), "arcane-uploads"),
	}
}

func validFilenameForKindInternal(kind, filename string) bool {
	lower := strings.ToLower(filename)
	switch kind {
	case uploadtypes.KindImage:
		return strings.HasSuffix(lower, ".tar") || strings.HasSuffix(lower, ".tar.gz") ||
			strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".tar.xz")
	case uploadtypes.KindVolumeBackup:
		return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")
	case uploadtypes.KindBuildWorkspace:
		return true
	}
	return false
}

// CreateSession validates the request against the kind's filename and size
// rules and stages a new empty session on disk.
func (s *UploadService) CreateSession(ctx context.Context, kind string, request uploadtypes.CreateSessionRequest) (*uploadtypes.Session, error) {
	filename := path.Base(strings.ReplaceAll(request.Filename, "\\", "/"))
	if filename == "" || filename == "." || filename == "/" {
		return nil, fmt.Errorf("%w: a filename is required", common.ErrUploadSessionInvalid)
	}
	if !validFilenameForKindInternal(kind, filename) {
		return nil, fmt.Errorf("%w: file type not allowed for %s uploads", common.ErrUploadSessionInvalid, kind)
	}
	if request.Size <= 0 {
		return nil, fmt.Errorf("%w: size must be positive", common.ErrUploadSessionInvalid)
	}
	if kind == uploadtypes.KindImage {
		maxSizeMB := s.settingsService.GetIntSetting(ctx, "maxImageUploadSize", 500)
		if maxSizeBytes := int64(maxSizeMB) * 1024 * 1024; request.Size > maxSizeBytes {
			return nil, fmt.Errorf("%w: file size exceeds maximum allowed size of %d MB", common.ErrUploadSessionInvalid, maxSizeMB)
		}
	}
	chunkSize := request.ChunkSize
	if chunkSize == 0 {
		chunkSize = uploadtypes.DefaultChunkSize
	}
	if chunkSize < uploadtypes.MinChunkSize || chunkSize > uploadtypes.MaxChunkSize {
		return nil, fmt.Errorf("%w: chunk size must be between %d and %d bytes", common.ErrUploadSessionInvalid, uploadtypes.MinChunkSize, uploadtypes.MaxChunkSize)
	}

	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return nil, fmt.Errorf("create upload root: %w", err)
	}
	sessionDir, err := acfs.MkdirTemp(ctx, s.root, "/", "*")
	if err != nil {
		return nil, fmt.Errorf("create upload session directory: %w", err)
	}
	session := &uploadtypes.Session{
		ID:             path.Base(sessionDir),
		Kind:           kind,
		Filename:       filename,
		Size:           request.Size,
		ChunkSize:      chunkSize,
		TotalChunks:    int((request.Size + chunkSize - 1) / chunkSize),
		ReceivedChunks: []int{},
		CreatedAt:      time.Now().UTC(),
	}
	if err := acfs.WriteFile(ctx, s.root, path.Join(sessionDir, sessionDataFilename), nil, 0o600); err != nil {
		_ = acfs.RemoveAll(ctx, s.root, sessionDir)
		return nil, fmt.Errorf("create upload session data file: %w", err)
	}
	if err := s.writeMetaInternal(ctx, session); err != nil {
		_ = acfs.RemoveAll(ctx, s.root, sessionDir)
		return nil, err
	}
	return session, nil
}

func (s *UploadService) sessionPathInternal(uploadID, filename string) (string, error) {
	if !uploadIDPattern.MatchString(uploadID) {
		return "", fmt.Errorf("%w: invalid upload ID", common.ErrUploadSessionNotFound)
	}
	return path.Join("/", uploadID, filename), nil
}

func (s *UploadService) loadMetaInternal(ctx context.Context, kind, uploadID string) (*uploadtypes.Session, error) {
	metaPath, err := s.sessionPathInternal(uploadID, sessionMetaFilename)
	if err != nil {
		return nil, err
	}
	payload, err := acfs.ReadFile(ctx, s.root, metaPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", common.ErrUploadSessionNotFound, uploadID)
	}
	var session uploadtypes.Session
	if err := json.Unmarshal(payload, &session); err != nil {
		return nil, fmt.Errorf("%w: corrupt session metadata", common.ErrUploadSessionNotFound)
	}
	if session.Kind != kind {
		return nil, fmt.Errorf("%w: session is for %s uploads", common.ErrUploadKindMismatch, session.Kind)
	}
	session.Complete = len(session.ReceivedChunks) == session.TotalChunks
	return &session, nil
}

func (s *UploadService) writeMetaInternal(ctx context.Context, session *uploadtypes.Session) error {
	session.Complete = len(session.ReceivedChunks) == session.TotalChunks
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode upload session metadata: %w", err)
	}
	if err := acfs.WriteFile(ctx, s.root, path.Join("/", session.ID, sessionMetaFilename), payload, 0o600); err != nil {
		return fmt.Errorf("write upload session metadata: %w", err)
	}
	return nil
}

// WriteChunk stores one chunk at its final offset and records it as received.
// Re-sending an already-received chunk is an idempotent overwrite.
func (s *UploadService) WriteChunk(ctx context.Context, kind, uploadID string, index int, data []byte) (*uploadtypes.Session, error) {
	defer s.locks.Lock(uploadID)()

	session, err := s.loadMetaInternal(ctx, kind, uploadID)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= session.TotalChunks {
		return nil, fmt.Errorf("%w: chunk index %d out of range [0,%d)", common.ErrUploadChunkInvalid, index, session.TotalChunks)
	}
	expected := session.ChunkSize
	if index == session.TotalChunks-1 {
		expected = session.Size - int64(index)*session.ChunkSize
	}
	if int64(len(data)) != expected {
		return nil, fmt.Errorf("%w: chunk %d is %d bytes, expected %d", common.ErrUploadChunkInvalid, index, len(data), expected)
	}

	dataPath, err := s.sessionPathInternal(uploadID, sessionDataFilename)
	if err != nil {
		return nil, err
	}
	if err := acfs.WriteAt(ctx, s.root, dataPath, int64(index)*session.ChunkSize, data); err != nil {
		return nil, fmt.Errorf("write chunk %d: %w", index, err)
	}
	if position, found := slices.BinarySearch(session.ReceivedChunks, index); !found {
		session.ReceivedChunks = slices.Insert(session.ReceivedChunks, position, index)
		if err := s.writeMetaInternal(ctx, session); err != nil {
			return nil, err
		}
	}
	return session, nil
}

// GetSession reports the session state, including which chunks have been
// received so clients can resume.
func (s *UploadService) GetSession(ctx context.Context, kind, uploadID string) (*uploadtypes.Session, error) {
	defer s.locks.RLock(uploadID)()
	return s.loadMetaInternal(ctx, kind, uploadID)
}

// DeleteSession aborts a session and discards its received chunks.
func (s *UploadService) DeleteSession(ctx context.Context, kind, uploadID string) error {
	defer s.locks.Lock(uploadID)()

	if _, err := s.loadMetaInternal(ctx, kind, uploadID); err != nil {
		return err
	}
	if err := acfs.RemoveAll(ctx, s.root, path.Join("/", uploadID)); err != nil {
		return fmt.Errorf("delete upload session: %w", err)
	}
	return nil
}

// IngestSession creates a session already containing the complete file. It
// backs the deprecated single-request multipart compatibility path, applying
// the same per-kind validation as chunked sessions.
func (s *UploadService) IngestSession(ctx context.Context, kind, filename string, size int64, source io.Reader) (*uploadtypes.Session, error) {
	session, err := s.CreateSession(ctx, kind, uploadtypes.CreateSessionRequest{Filename: filename, Size: size})
	if err != nil {
		return nil, err
	}
	if _, err := acfs.WriteFrom(ctx, s.root, path.Join("/", session.ID, sessionDataFilename), source, size, 0o600); err != nil {
		_ = acfs.RemoveAll(ctx, s.root, path.Join("/", session.ID))
		return nil, fmt.Errorf("write uploaded file: %w", err)
	}
	for index := range session.TotalChunks {
		session.ReceivedChunks = append(session.ReceivedChunks, index)
	}
	if err := s.writeMetaInternal(ctx, session); err != nil {
		_ = acfs.RemoveAll(ctx, s.root, path.Join("/", session.ID))
		return nil, err
	}
	return session, nil
}

// Consume validates that the session matches kind and is complete, then opens
// the assembled file for reading. The session metadata is removed under the
// session lock before returning, so a session can be consumed at most once
// even by concurrent requests; the returned cleanup closes the file and
// deletes the remaining session data.
func (s *UploadService) Consume(ctx context.Context, kind, uploadID string) (io.ReadSeekCloser, *uploadtypes.Session, func(), error) {
	defer s.locks.Lock(uploadID)()

	session, err := s.loadMetaInternal(ctx, kind, uploadID)
	if err != nil {
		return nil, nil, nil, err
	}
	if !session.Complete {
		return nil, nil, nil, fmt.Errorf("%w: %d of %d chunks received", common.ErrUploadSessionIncomplete, len(session.ReceivedChunks), session.TotalChunks)
	}
	dataPath, err := s.sessionPathInternal(uploadID, sessionDataFilename)
	if err != nil {
		return nil, nil, nil, err
	}
	file, size, err := acfs.OpenReadSeek(ctx, s.root, dataPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open upload session data: %w", err)
	}
	if size != session.Size {
		_ = file.Close()
		return nil, nil, nil, fmt.Errorf("%w: assembled size %d does not match declared size %d", common.ErrUploadSessionIncomplete, size, session.Size)
	}
	if err := acfs.Remove(ctx, s.root, path.Join("/", uploadID, sessionMetaFilename)); err != nil {
		_ = file.Close()
		return nil, nil, nil, fmt.Errorf("mark upload session consumed: %w", err)
	}
	cleanup := func() {
		_ = file.Close()
		// context.WithoutCancel: the session must be removed even when the
		// consuming request was cancelled mid-restore.
		_ = acfs.RemoveAll(context.WithoutCancel(ctx), s.root, path.Join("/", uploadID))
	}
	return file, session, cleanup, nil
}

// PurgeExpiredSessions removes sessions that have been idle for longer than
// maxAge. Idleness is judged by the newest modification time among the
// session's files — the data file advances on every chunk write and the
// metadata on every chunk recorded — so a slow but active upload is never
// swept, and the check runs under the session lock so an in-flight chunk
// write cannot race the removal.
func (s *UploadService) PurgeExpiredSessions(ctx context.Context, maxAge time.Duration) (int, error) {
	if _, err := os.Stat(s.root); os.IsNotExist(err) {
		return 0, nil
	}
	entries, err := acfs.List(ctx, s.root, "/")
	if err != nil {
		return 0, fmt.Errorf("list upload sessions: %w", err)
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for _, entry := range entries {
		if !entry.IsDirectory {
			continue
		}
		unlock := s.locks.Lock(entry.Name)
		lastActivity := entry.ModTime
		for _, filename := range []string{sessionDataFilename, sessionMetaFilename} {
			if info, statErr := acfs.Stat(ctx, s.root, path.Join("/", entry.Name, filename), true); statErr == nil && info.ModTime.After(lastActivity) {
				lastActivity = info.ModTime
			}
		}
		var removeErr error
		if lastActivity.Before(cutoff) {
			removeErr = acfs.RemoveAll(ctx, s.root, path.Join("/", entry.Name))
		}
		unlock()
		if removeErr != nil {
			return removed, fmt.Errorf("remove expired upload session %s: %w", entry.Name, removeErr)
		}
		if lastActivity.Before(cutoff) {
			removed++
		}
	}
	return removed, nil
}
