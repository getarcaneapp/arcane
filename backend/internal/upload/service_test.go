package upload

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	uploadtypes "github.com/getarcaneapp/arcane/types/v2/upload"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
)

func TestSessionChunkRoundTrip(t *testing.T) {
	service := &UploadService{root: filepath.Join(t.TempDir(), "uploads")}
	ctx := t.Context()

	chunkSize := uploadtypes.MinChunkSize
	payload := bytes.Repeat([]byte("a"), int(chunkSize))
	payload = append(payload, bytes.Repeat([]byte("b"), int(chunkSize))...)
	payload = append(payload, []byte("short-tail")...)

	session, err := service.CreateSession(ctx, uploadtypes.KindBuildWorkspace, uploadtypes.CreateSessionRequest{
		Filename:  "artifact.bin",
		Size:      int64(len(payload)),
		ChunkSize: chunkSize,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.TotalChunks != 3 {
		t.Fatalf("TotalChunks = %d, want 3", session.TotalChunks)
	}

	if _, err := service.WriteChunk(ctx, uploadtypes.KindBuildWorkspace, session.ID, 3, payload[:chunkSize]); !errors.Is(err, common.ErrUploadChunkInvalid) {
		t.Fatalf("out-of-range chunk error = %v, want ErrUploadChunkInvalid", err)
	}
	if _, err := service.WriteChunk(ctx, uploadtypes.KindBuildWorkspace, session.ID, 2, payload[:chunkSize]); !errors.Is(err, common.ErrUploadChunkInvalid) {
		t.Fatalf("wrong-length chunk error = %v, want ErrUploadChunkInvalid", err)
	}

	// Chunks arrive out of order; the session stays resumable in between.
	if _, err := service.WriteChunk(ctx, uploadtypes.KindBuildWorkspace, session.ID, 2, payload[2*chunkSize:]); err != nil {
		t.Fatalf("WriteChunk(2): %v", err)
	}
	updated, err := service.WriteChunk(ctx, uploadtypes.KindBuildWorkspace, session.ID, 0, payload[:chunkSize])
	if err != nil {
		t.Fatalf("WriteChunk(0): %v", err)
	}
	if got := updated.ReceivedChunks; len(got) != 2 || got[0] != 0 || got[1] != 2 || updated.Complete {
		t.Fatalf("session after partial upload = %+v", updated)
	}
	if _, _, _, err := service.Consume(ctx, uploadtypes.KindBuildWorkspace, session.ID); !errors.Is(err, common.ErrUploadSessionIncomplete) {
		t.Fatalf("incomplete Consume error = %v, want ErrUploadSessionIncomplete", err)
	}

	if _, err := service.WriteChunk(ctx, uploadtypes.KindBuildWorkspace, session.ID, 1, payload[chunkSize:2*chunkSize]); err != nil {
		t.Fatalf("WriteChunk(1): %v", err)
	}
	if _, _, _, err := service.Consume(ctx, uploadtypes.KindImage, session.ID); !errors.Is(err, common.ErrUploadKindMismatch) {
		t.Fatalf("wrong-kind Consume error = %v, want ErrUploadKindMismatch", err)
	}

	file, consumed, cleanup, err := service.Consume(ctx, uploadtypes.KindBuildWorkspace, session.ID)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if !consumed.Complete || consumed.Filename != "artifact.bin" {
		t.Fatalf("consumed session = %+v", consumed)
	}
	// Sessions are single-use: a concurrent second consumer must not see it.
	if _, _, _, err := service.Consume(ctx, uploadtypes.KindBuildWorkspace, session.ID); !errors.Is(err, common.ErrUploadSessionNotFound) {
		t.Fatalf("second Consume error = %v, want ErrUploadSessionNotFound", err)
	}
	assembled, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(assembled, payload) {
		t.Fatalf("assembled file differs from input (%d vs %d bytes)", len(assembled), len(payload))
	}
	cleanup()
	if _, err := os.Stat(filepath.Join(service.root, session.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session dir still present after cleanup: %v", err)
	}
}

func TestIngestSessionCreatesCompleteSession(t *testing.T) {
	service := &UploadService{root: filepath.Join(t.TempDir(), "uploads")}
	ctx := t.Context()
	payload := []byte("legacy-multipart-body")

	session, err := service.IngestSession(ctx, uploadtypes.KindBuildWorkspace, "legacy.bin", int64(len(payload)), bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("IngestSession: %v", err)
	}
	if !session.Complete {
		t.Fatalf("ingested session not complete: %+v", session)
	}

	file, _, cleanup, err := service.Consume(ctx, uploadtypes.KindBuildWorkspace, session.ID)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	defer cleanup()
	assembled, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(assembled, payload) {
		t.Fatalf("assembled = %q, want %q", assembled, payload)
	}

	if _, err := service.IngestSession(ctx, uploadtypes.KindBuildWorkspace, "short.bin", 10, bytes.NewReader([]byte("abc"))); err == nil {
		t.Fatal("IngestSession with short source unexpectedly succeeded")
	}
}

func TestPurgeExpiredSessionsRemovesOnlyStaleSessions(t *testing.T) {
	service := &UploadService{root: filepath.Join(t.TempDir(), "uploads")}
	ctx := t.Context()

	stale, err := service.CreateSession(ctx, uploadtypes.KindBuildWorkspace, uploadtypes.CreateSessionRequest{Filename: "stale.bin", Size: 1})
	if err != nil {
		t.Fatalf("CreateSession(stale): %v", err)
	}
	// Expiry is idle-based: backdate every mtime the purge inspects.
	idle := time.Now().Add(-48 * time.Hour)
	for _, name := range []string{"", "meta.json", "data"} {
		if err := os.Chtimes(filepath.Join(service.root, stale.ID, name), idle, idle); err != nil {
			t.Fatalf("backdate stale session: %v", err)
		}
	}
	fresh, err := service.CreateSession(ctx, uploadtypes.KindBuildWorkspace, uploadtypes.CreateSessionRequest{Filename: "fresh.bin", Size: 1})
	if err != nil {
		t.Fatalf("CreateSession(fresh): %v", err)
	}

	removed, err := service.PurgeExpiredSessions(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("PurgeExpiredSessions: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := service.GetSession(ctx, uploadtypes.KindBuildWorkspace, stale.ID); !errors.Is(err, common.ErrUploadSessionNotFound) {
		t.Fatalf("stale session error = %v, want ErrUploadSessionNotFound", err)
	}
	if _, err := service.GetSession(ctx, uploadtypes.KindBuildWorkspace, fresh.ID); err != nil {
		t.Fatalf("fresh session should survive purge: %v", err)
	}
}
