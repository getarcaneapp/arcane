package upload

import (
	"cmp"
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

	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"
	"kit.getarcane.app/concurrency"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/types/v3"
)

type (
	UploadService struct {
		locks           concurrency.KeyedMutex
		settingsService *settings.SettingsService
		root            string
	}
)

const (
	sessionMetaFilename = "meta.json"
	sessionDataFilename = "data"
)

var uploadIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

func NewUploadService(settingsService *settings.SettingsService) *UploadService {
	return &UploadService{
		settingsService: settingsService,
		root:            filepath.Join(os.TempDir(), "arcane-uploads"),
	}
}

func validFilenameForKind(kind, filename string) bool {
	lower := strings.ToLower(filename)
	switch kind {
	case types.UploadKindImage:
		return strings.HasSuffix(lower, ".tar") || strings.HasSuffix(lower, ".tar.gz") ||
			strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".tar.xz")
	case types.UploadKindVolumeBackup:
		return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")
	case types.UploadKindBuildWorkspace:
		return true
	}
	return false
}

func (s *UploadService) CreateSession(ctx context.Context, kind string, request types.CreateUploadSessionRequest) (*types.UploadSession, error) {
	filename := path.Base(strings.ReplaceAll(request.Filename, "\\", "/"))
	if filename == "" || filename == "." || filename == "/" {
		return nil, fmt.Errorf("%w: a filename is required", common.ErrUploadSessionInvalid)
	}
	if !validFilenameForKind(kind, filename) {
		return nil, fmt.Errorf("%w: file type not allowed for %s uploads", common.ErrUploadSessionInvalid, kind)
	}
	if request.Size <= 0 {
		return nil, fmt.Errorf("%w: size must be positive", common.ErrUploadSessionInvalid)
	}
	if kind == types.UploadKindImage {
		maxSizeMB := s.settingsService.GetIntSetting(ctx, "maxImageUploadSize")
		if maxSizeBytes := int64(maxSizeMB) * 1024 * 1024; request.Size > maxSizeBytes {
			return nil, fmt.Errorf("%w: file size exceeds maximum allowed size of %d MB", common.ErrUploadSessionInvalid, maxSizeMB)
		}
	}
	chunkSize := cmp.Or(request.ChunkSize, types.UploadDefaultChunkSize)
	if chunkSize < types.UploadMinChunkSize || chunkSize > types.UploadMaxChunkSize {
		return nil, fmt.Errorf("%w: chunk size must be between %d and %d bytes", common.ErrUploadSessionInvalid, types.UploadMinChunkSize, types.UploadMaxChunkSize)
	}

	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return nil, fmt.Errorf("create upload root: %w", err)
	}
	sessionDir, err := acfs.Root{Path: s.root}.MkdirTemp(ctx, "/", "*")
	if err != nil {
		return nil, fmt.Errorf("create upload session directory: %w", err)
	}
	session := &types.UploadSession{
		ID:             path.Base(sessionDir),
		Kind:           kind,
		Filename:       filename,
		Size:           request.Size,
		ChunkSize:      chunkSize,
		TotalChunks:    int((request.Size + chunkSize - 1) / chunkSize),
		ReceivedChunks: []int{},
		CreatedAt:      time.Now().UTC(),
	}
	if writeErr := (acfs.Root{Path: s.root}).Write(ctx, path.Join(sessionDir, sessionDataFilename), nil, acfs.WriteOptions{Mode: 0o600}); writeErr != nil {
		_ = acfs.Root{Path: s.root}.RemoveAll(ctx, sessionDir)
		return nil, fmt.Errorf("create upload session data file: %w", writeErr)
	}
	if writeMetaErr := s.writeMeta(ctx, session); writeMetaErr != nil {
		_ = acfs.Root{Path: s.root}.RemoveAll(ctx, sessionDir)
		return nil, writeMetaErr
	}
	return session, nil
}

func (s *UploadService) sessionPath(uploadID, filename string) (string, error) {
	if !uploadIDPattern.MatchString(uploadID) {
		return "", fmt.Errorf("%w: invalid upload ID", common.ErrUploadSessionNotFound)
	}
	return path.Join("/", uploadID, filename), nil
}

func (s *UploadService) loadMeta(ctx context.Context, kind, uploadID string) (*types.UploadSession, error) {
	metaPath, err := s.sessionPath(uploadID, sessionMetaFilename)
	if err != nil {
		return nil, err
	}
	payload, err := acfs.Root{Path: s.root}.ReadFile(ctx, metaPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", common.ErrUploadSessionNotFound, uploadID)
	}
	var session types.UploadSession
	if unmarshalErr := json.Unmarshal(payload, &session); unmarshalErr != nil {
		return nil, fmt.Errorf("%w: corrupt session metadata", common.ErrUploadSessionNotFound)
	}
	if session.Kind != kind {
		return nil, fmt.Errorf("%w: session is for %s uploads", common.ErrUploadKindMismatch, session.Kind)
	}
	session.Complete = len(session.ReceivedChunks) == session.TotalChunks
	return &session, nil
}

func (s *UploadService) writeMeta(ctx context.Context, session *types.UploadSession) error {
	session.Complete = len(session.ReceivedChunks) == session.TotalChunks
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode upload session metadata: %w", err)
	}
	if writeErr := (acfs.Root{Path: s.root}).Write(ctx, path.Join("/", session.ID, sessionMetaFilename), payload, acfs.WriteOptions{Mode: 0o600}); writeErr != nil {
		return fmt.Errorf("write upload session metadata: %w", writeErr)
	}
	return nil
}

// Re-sending an already-received chunk is an idempotent overwrite.
func (s *UploadService) WriteChunk(ctx context.Context, kind, uploadID string, index int, data []byte) (*types.UploadSession, error) {
	defer s.locks.Lock(uploadID)()

	session, err := s.loadMeta(ctx, kind, uploadID)
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

	dataPath, err := s.sessionPath(uploadID, sessionDataFilename)
	if err != nil {
		return nil, err
	}
	if writeAtErr := (acfs.Root{Path: s.root}).WriteAt(ctx, dataPath, int64(index)*session.ChunkSize, data); writeAtErr != nil {
		return nil, fmt.Errorf("write chunk %d: %w", index, writeAtErr)
	}
	if position, found := slices.BinarySearch(session.ReceivedChunks, index); !found {
		session.ReceivedChunks = slices.Insert(session.ReceivedChunks, position, index)
		if writeMetaErr := s.writeMeta(ctx, session); writeMetaErr != nil {
			return nil, writeMetaErr
		}
	}
	return session, nil
}

func (s *UploadService) GetSession(ctx context.Context, kind, uploadID string) (*types.UploadSession, error) {
	defer s.locks.RLock(uploadID)()
	return s.loadMeta(ctx, kind, uploadID)
}

func (s *UploadService) DeleteSession(ctx context.Context, kind, uploadID string) error {
	defer s.locks.Lock(uploadID)()

	if _, err := s.loadMeta(ctx, kind, uploadID); err != nil {
		return err
	}
	if err := (acfs.Root{Path: s.root}).RemoveAll(ctx, path.Join("/", uploadID)); err != nil {
		return fmt.Errorf("delete upload session: %w", err)
	}
	return nil
}

func (s *UploadService) IngestSession(ctx context.Context, kind, filename string, size int64, source io.Reader) (*types.UploadSession, error) {
	session, err := s.CreateSession(ctx, kind, types.CreateUploadSessionRequest{Filename: filename, Size: size})
	if err != nil {
		return nil, err
	}
	if _, writeFromErr := (acfs.Root{Path: s.root}).WriteFrom(ctx, path.Join("/", session.ID, sessionDataFilename), source, acfs.WriteFromOptions{Size: size, Mode: 0o600}); writeFromErr != nil {
		_ = acfs.Root{Path: s.root}.RemoveAll(ctx, path.Join("/", session.ID))
		return nil, fmt.Errorf("write uploaded file: %w", writeFromErr)
	}
	for index := range session.TotalChunks {
		session.ReceivedChunks = append(session.ReceivedChunks, index)
	}
	if writeMetaErr := s.writeMeta(ctx, session); writeMetaErr != nil {
		_ = acfs.Root{Path: s.root}.RemoveAll(ctx, path.Join("/", session.ID))
		return nil, writeMetaErr
	}
	return session, nil
}

func (s *UploadService) Consume(ctx context.Context, kind, uploadID string) (io.ReadSeekCloser, *types.UploadSession, func(), error) {
	defer s.locks.Lock(uploadID)()

	session, err := s.loadMeta(ctx, kind, uploadID)
	if err != nil {
		return nil, nil, nil, err
	}
	if !session.Complete {
		return nil, nil, nil, fmt.Errorf("%w: %d of %d chunks received", common.ErrUploadSessionIncomplete, len(session.ReceivedChunks), session.TotalChunks)
	}
	dataPath, err := s.sessionPath(uploadID, sessionDataFilename)
	if err != nil {
		return nil, nil, nil, err
	}
	file, size, err := acfs.Root{Path: s.root}.OpenReadSeek(ctx, dataPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open upload session data: %w", err)
	}
	if size != session.Size {
		_ = file.Close()
		return nil, nil, nil, fmt.Errorf("%w: assembled size %d does not match declared size %d", common.ErrUploadSessionIncomplete, size, session.Size)
	}
	if removeErr := (acfs.Root{Path: s.root}).Remove(ctx, path.Join("/", uploadID, sessionMetaFilename)); removeErr != nil {
		_ = file.Close()
		return nil, nil, nil, fmt.Errorf("mark upload session consumed: %w", removeErr)
	}
	cleanup := func() {
		_ = file.Close()
		// context.WithoutCancel: the session must be removed even when the
		// consuming request was cancelled mid-restore.
		_ = acfs.Root{Path: s.root}.RemoveAll(context.WithoutCancel(ctx), path.Join("/", uploadID))
	}
	return file, session, cleanup, nil
}

func (s *UploadService) PurgeExpiredSessions(ctx context.Context, maxAge time.Duration) (int, error) {
	if _, err := os.Stat(s.root); os.IsNotExist(err) {
		return 0, nil
	}
	entries, err := acfs.Root{Path: s.root}.List(ctx, "/")
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
			if info, statErr := (acfs.Root{Path: s.root}).Stat(ctx, path.Join("/", entry.Name, filename), acfstypes.StatOptions{FollowFinal: true}); statErr == nil && info.ModTime.After(lastActivity) {
				lastActivity = info.ModTime
			}
		}
		var removeErr error
		if lastActivity.Before(cutoff) {
			removeErr = acfs.Root{Path: s.root}.RemoveAll(ctx, path.Join("/", entry.Name))
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
