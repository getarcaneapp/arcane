// Package transfer is the environment-side support for cross-environment
// transfers: durable resource holds and the spool that stages an archive so
// the manager can relay it in bounded, retryable byte ranges.
package transfer

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	transfertypes "github.com/getarcaneapp/arcane/types/v2/transfer"
	"github.com/google/uuid"
)

// ErrExportNotFound means the export expired, was closed, or never existed.
var ErrExportNotFound = errors.New("transfer export not found")

type spoolFileInternal struct {
	path     string
	size     int64
	sha256   string
	lastUsed time.Time
}

// Spool stages gzip-compressed tar archives on local disk so they can be
// served as byte ranges with a known size and hash.
type Spool struct {
	dir         string
	idleTimeout time.Duration
	mu          sync.Mutex
	files       map[string]*spoolFileInternal
}

// NewSpool stages exports under dir.
func NewSpool(dir string, idleTimeout time.Duration) *Spool {
	return &Spool{dir: dir, idleTimeout: idleTimeout, files: map[string]*spoolFileInternal{}}
}

// Start removes idle exports until ctx ends, then everything.
func (s *Spool) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.idleTimeout / 2)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				s.removeInternal(func(*spoolFileInternal) bool { return true })
				return
			case now := <-ticker.C:
				s.removeInternal(func(file *spoolFileInternal) bool { return now.Sub(file.lastUsed) > s.idleTimeout })
			}
		}
	}()
}

// Create compresses source into a spool file and returns its identity, size,
// and SHA-256 of the stored bytes.
func (s *Spool) Create(ctx context.Context, source io.Reader) (transfertypes.Export, error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return transfertypes.Export{}, fmt.Errorf("create spool directory: %w", err)
	}
	id := uuid.NewString()
	path := filepath.Join(s.dir, id+".tar.gz")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return transfertypes.Export{}, fmt.Errorf("create spool file: %w", err)
	}
	hasher := sha256.New()
	counter := &countingWriterInternal{}
	compressor := gzip.NewWriter(io.MultiWriter(file, hasher, counter))
	_, copyErr := io.Copy(compressor, readerWithContextInternal{ctx: ctx, reader: source})
	err = errors.Join(copyErr, compressor.Close(), file.Close())
	if err != nil {
		_ = os.Remove(path)
		return transfertypes.Export{}, fmt.Errorf("spool archive: %w", err)
	}
	s.mu.Lock()
	s.files[id] = &spoolFileInternal{path: path, size: counter.n, sha256: hex.EncodeToString(hasher.Sum(nil)), lastUsed: time.Now()}
	s.mu.Unlock()
	return transfertypes.Export{ExportID: id, Size: counter.n, SHA256: hex.EncodeToString(hasher.Sum(nil))}, nil
}

// ReadAt returns up to length bytes of the export starting at offset.
func (s *Spool) ReadAt(id string, offset, length int64) ([]byte, error) {
	s.mu.Lock()
	entry, ok := s.files[id]
	if ok {
		entry.lastUsed = time.Now()
	}
	s.mu.Unlock()
	if !ok {
		return nil, ErrExportNotFound
	}
	if offset < 0 || length <= 0 || offset > entry.size {
		return nil, fmt.Errorf("invalid range %d+%d for export of %d bytes", offset, length, entry.size)
	}
	file, err := os.Open(entry.path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	buffer := make([]byte, min(length, entry.size-offset))
	read, err := file.ReadAt(buffer, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buffer[:read], nil
}

// Remove deletes one export.
func (s *Spool) Remove(id string) {
	s.removeInternal(func(file *spoolFileInternal) bool { return file.path == filepath.Join(s.dir, id+".tar.gz") })
}

func (s *Spool) removeInternal(match func(*spoolFileInternal) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, file := range s.files {
		if !match(file) {
			continue
		}
		if err := os.Remove(file.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("transfer: failed to remove spooled export", "path", file.path, "error", err)
		}
		delete(s.files, id)
	}
}

type countingWriterInternal struct{ n int64 }

func (c *countingWriterInternal) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

type readerWithContextInternal struct {
	ctx    context.Context
	reader io.Reader
}

func (r readerWithContextInternal) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}
