package upload

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/shirou/gopsutil/v4/disk"

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

	downloadReader struct {
		io.ReadCloser
		done func()
		eof  bool
	}

	WriteSessionOptions struct {
		Write    func(io.Writer) error
		Kind     string
		Filename string
		// Origin and OriginID record what wrote the session, such as an export of a project.
		Origin   string
		OriginID string
	}

	// reserveWriter stops a written session before it eats into the reserve, checking again every reserveCheckBytes.
	reserveWriter struct {
		ctx       context.Context
		w         io.Writer
		service   *UploadService
		unchecked int64
	}
)

const (
	sessionMetaFilename = "meta.json"
	sessionDataFilename = "data"
	// uploadReserveBytes is the least free space a staged upload leaves on the upload filesystem.
	uploadReserveBytes = 1 << 30
	reserveCheckBytes  = 64 << 20
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
	case types.UploadKindVolumeBackup, types.UploadKindProjectArchive:
		return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")
	case types.UploadKindBuildWorkspace:
		return true
	}
	return false
}

func sessionFilename(kind, requested string) (string, error) {
	filename := path.Base(strings.ReplaceAll(requested, "\\", "/"))
	if filename == "" || filename == "." || filename == "/" {
		return "", fmt.Errorf("%w: a filename is required", common.ErrUploadSessionInvalid)
	}
	if !validFilenameForKind(kind, filename) {
		return "", fmt.Errorf("%w: file type not allowed for %s uploads", common.ErrUploadSessionInvalid, kind)
	}
	return filename, nil
}

func (s *UploadService) createSessionDir(ctx context.Context) (string, error) {
	if err := (acfs.Host{}).MkdirAll(ctx, s.root, 0o700); err != nil {
		return "", fmt.Errorf("create upload root: %w", err)
	}
	sessionDir, err := acfs.Root{Path: s.root}.MkdirTemp(ctx, "/", "*")
	if err != nil {
		return "", fmt.Errorf("create upload session directory: %w", err)
	}
	return sessionDir, nil
}

func (s *UploadService) validateSize(ctx context.Context, kind string, size int64) error {
	if size <= 0 {
		return fmt.Errorf("%w: size must be positive", common.ErrUploadSessionInvalid)
	}
	if kind == types.UploadKindImage {
		maxSizeMB := s.settingsService.GetIntSetting(ctx, "maxImageUploadSize")
		if maxSizeBytes := int64(maxSizeMB) * 1024 * 1024; size > maxSizeBytes {
			return fmt.Errorf("%w: file size exceeds maximum allowed size of %d MB", common.ErrUploadSessionInvalid, maxSizeMB)
		}
	}
	return s.CheckFreeSpace(ctx, size)
}

// CheckFreeSpace refuses size bytes more when they would leave less than the reserve free. Staged uploads, including
// the expanded entries of a project bundle, keep the same room free as unpacking a project archive does, so a large one
// can't fill the disk other work needs.
func (s *UploadService) CheckFreeSpace(ctx context.Context, size int64) error {
	checked := s.root
	for {
		if _, statErr := os.Stat(checked); statErr == nil || filepath.Dir(checked) == checked {
			break
		}
		checked = filepath.Dir(checked)
	}
	usage, err := disk.UsageWithContext(ctx, checked)
	if err != nil {
		return fmt.Errorf("check free space for the upload: %w", err)
	}
	reserve := max(uploadReserveBytes, int64(usage.Total/20))
	if size > int64(usage.Free)-reserve {
		return fmt.Errorf("%w: the upload needs %s, more than can be stored while keeping %s free", common.ErrUploadSessionInvalid,
			units.BytesSize(float64(size)), units.BytesSize(float64(reserve)))
	}
	return nil
}

func (s *UploadService) CreateSession(ctx context.Context, kind string, request types.CreateUploadSessionRequest) (*types.UploadSession, error) {
	filename, err := sessionFilename(kind, request.Filename)
	if err != nil {
		return nil, err
	}
	if sizeErr := s.validateSize(ctx, kind, request.Size); sizeErr != nil {
		return nil, sizeErr
	}
	chunkSize := cmp.Or(request.ChunkSize, types.UploadDefaultChunkSize)
	if chunkSize < types.UploadMinChunkSize || chunkSize > types.UploadMaxChunkSize {
		return nil, fmt.Errorf("%w: chunk size must be between %d and %d bytes", common.ErrUploadSessionInvalid, types.UploadMinChunkSize, types.UploadMaxChunkSize)
	}

	sessionDir, err := s.createSessionDir(ctx)
	if err != nil {
		return nil, err
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

func (s *UploadService) ReadChunk(ctx context.Context, kind, uploadID string, index int) ([]byte, error) {
	defer s.locks.RLock(uploadID)()

	session, err := s.loadMeta(ctx, kind, uploadID)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= session.TotalChunks {
		return nil, fmt.Errorf("%w: chunk index %d out of range [0,%d)", common.ErrUploadChunkInvalid, index, session.TotalChunks)
	}
	if !session.Complete {
		return nil, fmt.Errorf("%w: %d of %d chunks received", common.ErrUploadSessionIncomplete, len(session.ReceivedChunks), session.TotalChunks)
	}
	dataPath, err := s.sessionPath(uploadID, sessionDataFilename)
	if err != nil {
		return nil, err
	}
	file, _, err := acfs.Root{Path: s.root}.OpenReadSeek(ctx, dataPath)
	if err != nil {
		return nil, fmt.Errorf("open upload session data: %w", err)
	}
	// Opened only for reading; closing it can't lose data, and read errors are returned as they happen.
	defer func() { _ = file.Close() }()
	if _, seekErr := file.Seek(int64(index)*session.ChunkSize, io.SeekStart); seekErr != nil {
		return nil, fmt.Errorf("seek chunk %d: %w", index, seekErr)
	}
	data := make([]byte, min(session.ChunkSize, session.Size-int64(index)*session.ChunkSize))
	if _, readErr := io.ReadFull(file, data); readErr != nil {
		return nil, fmt.Errorf("read chunk %d: %w", index, readErr)
	}
	return data, nil
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
	if err := s.validateSize(ctx, kind, size); err != nil {
		return nil, err
	}
	write := func(w io.Writer) error {
		if _, err := io.CopyN(w, source, size); err != nil {
			return fmt.Errorf("write uploaded file: %w", err)
		}
		return nil
	}
	return s.WriteSession(ctx, WriteSessionOptions{
		Kind:     kind,
		Filename: filename,
		Write:    write,
	})
}

func (w *reserveWriter) Write(p []byte) (int, error) {
	if w.unchecked <= 0 {
		if err := w.service.CheckFreeSpace(w.ctx, reserveCheckBytes); err != nil {
			return 0, fmt.Errorf("stop writing the upload: %w", err)
		}
		w.unchecked = reserveCheckBytes
	}
	n, err := w.w.Write(p)
	w.unchecked -= int64(n)
	return n, err
}

func (s *UploadService) WriteSession(ctx context.Context, opts WriteSessionOptions) (session *types.UploadSession, err error) {
	kind := opts.Kind
	filename, err := sessionFilename(kind, opts.Filename)
	if err != nil {
		return nil, err
	}
	sessionDir, err := s.createSessionDir(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			if removeErr := (acfs.Root{Path: s.root}).RemoveAll(context.WithoutCancel(ctx), sessionDir); removeErr != nil {
				err = errors.Join(err, fmt.Errorf("remove the partial upload session: %w", removeErr))
			}
		}
	}()
	file, err := acfs.Root{Path: s.root}.OpenFile(ctx, path.Join(sessionDir, sessionDataFilename), acfs.OpenOptions{
		Mode:      0o600,
		Write:     true,
		Create:    true,
		Exclusive: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create upload session data file: %w", err)
	}
	// What the callback writes isn't known up front, and it may stage more files while writing, so free space is
	// rechecked as the file grows.
	if writeErr := opts.Write(&reserveWriter{
		ctx:     ctx,
		w:       file,
		service: s,
	}); writeErr != nil {
		return nil, errors.Join(writeErr, file.Close())
	}
	info, err := file.Stat()
	if closeErr := file.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("write upload session data: %w", err)
	}
	if info.Size() <= 0 {
		return nil, fmt.Errorf("%w: the written file is empty", common.ErrUploadSessionInvalid)
	}
	totalChunks := int((info.Size() + types.UploadDefaultChunkSize - 1) / types.UploadDefaultChunkSize)
	received := make([]int, totalChunks)
	for index := range received {
		received[index] = index
	}
	session = &types.UploadSession{
		ID:             path.Base(sessionDir),
		Kind:           kind,
		Filename:       filename,
		Size:           info.Size(),
		ChunkSize:      types.UploadDefaultChunkSize,
		TotalChunks:    totalChunks,
		ReceivedChunks: received,
		CreatedAt:      time.Now().UTC(),
		Origin:         opts.Origin,
		OriginID:       opts.OriginID,
	}
	if writeMetaErr := s.writeMeta(ctx, session); writeMetaErr != nil {
		return nil, writeMetaErr
	}
	return session, nil
}

func (s *UploadService) DownloadSession(ctx context.Context, kind, uploadID string) (io.ReadCloser, *types.UploadSession, error) {
	defer s.locks.RLock(uploadID)()

	session, err := s.loadMeta(ctx, kind, uploadID)
	if err != nil {
		return nil, nil, err
	}
	if !session.Complete {
		return nil, nil, fmt.Errorf("%w: %d of %d chunks received", common.ErrUploadSessionIncomplete, len(session.ReceivedChunks), session.TotalChunks)
	}
	dataPath, err := s.sessionPath(uploadID, sessionDataFilename)
	if err != nil {
		return nil, nil, err
	}
	file, size, err := acfs.Root{Path: s.root}.OpenReadSeek(ctx, dataPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open upload session data: %w", err)
	}
	if size != session.Size {
		// Opened only for reading and refused; the size mismatch is the error that matters.
		_ = file.Close()
		return nil, nil, fmt.Errorf("%w: assembled size %d does not match declared size %d", common.ErrUploadSessionIncomplete, size, session.Size)
	}
	return &downloadReader{
		ReadCloser: file,
		done: func() {
			if deleteErr := s.DeleteSession(context.WithoutCancel(ctx), kind, uploadID); deleteErr != nil {
				slog.WarnContext(ctx, "failed to delete downloaded upload session", "uploadId", uploadID, "error", deleteErr)
			}
		},
	}, session, nil
}

func (r *downloadReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if errors.Is(err, io.EOF) {
		r.eof = true
	}
	return n, err
}

func (r *downloadReader) Close() error {
	err := r.ReadCloser.Close()
	if r.eof {
		r.done()
	}
	return err
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
		// Opened only for reading and refused; the size mismatch is the error that matters.
		_ = file.Close()
		return nil, nil, nil, fmt.Errorf("%w: assembled size %d does not match declared size %d", common.ErrUploadSessionIncomplete, size, session.Size)
	}
	if removeErr := (acfs.Root{Path: s.root}).Remove(ctx, path.Join("/", uploadID, sessionMetaFilename)); removeErr != nil {
		// Opened only for reading; the failed mark is the error that matters.
		_ = file.Close()
		return nil, nil, nil, fmt.Errorf("mark upload session consumed: %w", removeErr)
	}
	cleanup := func() {
		// Opened only for reading and fully consumed by the caller; closing it can't lose data.
		_ = file.Close()
		// context.WithoutCancel: the session must be removed even when the
		// consuming request was cancelled mid-restore.
		_ = acfs.Root{Path: s.root}.RemoveAll(context.WithoutCancel(ctx), path.Join("/", uploadID))
	}
	return file, session, cleanup, nil
}

func (s *UploadService) PurgeExpiredSessions(ctx context.Context, maxAge time.Duration) (int, error) {
	if exists, existsErr := (acfs.Host{}).Exists(ctx, s.root); existsErr == nil && !exists {
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
