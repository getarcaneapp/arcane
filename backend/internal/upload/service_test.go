package upload

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
)

func TestSessionChunkRoundTrip(t *testing.T) {
	service := &UploadService{root: filepath.Join(t.TempDir(), "uploads")}
	ctx := t.Context()

	chunkSize := types.UploadMinChunkSize
	payload := bytes.Repeat([]byte("a"), int(chunkSize))
	payload = append(payload, bytes.Repeat([]byte("b"), int(chunkSize))...)
	payload = append(payload, []byte("short-tail")...)

	session, err := service.CreateSession(ctx, types.UploadKindBuildWorkspace, types.CreateUploadSessionRequest{
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

	if _, writeChunkErr := service.WriteChunk(ctx, types.UploadKindBuildWorkspace, session.ID, 3, payload[:chunkSize]); !errors.Is(writeChunkErr, common.ErrUploadChunkInvalid) {
		t.Fatalf("out-of-range chunk error = %v, want ErrUploadChunkInvalid", writeChunkErr)
	}
	if _, writeChunkErr2 := service.WriteChunk(ctx, types.UploadKindBuildWorkspace, session.ID, 2, payload[:chunkSize]); !errors.Is(writeChunkErr2, common.ErrUploadChunkInvalid) {
		t.Fatalf("wrong-length chunk error = %v, want ErrUploadChunkInvalid", writeChunkErr2)
	}

	// Chunks arrive out of order; the session stays resumable in between.
	if _, writeChunkErr3 := service.WriteChunk(ctx, types.UploadKindBuildWorkspace, session.ID, 2, payload[2*chunkSize:]); writeChunkErr3 != nil {
		t.Fatalf("WriteChunk(2): %v", writeChunkErr3)
	}
	updated, err := service.WriteChunk(ctx, types.UploadKindBuildWorkspace, session.ID, 0, payload[:chunkSize])
	if err != nil {
		t.Fatalf("WriteChunk(0): %v", err)
	}
	if got := updated.ReceivedChunks; len(got) != 2 || got[0] != 0 || got[1] != 2 || updated.Complete {
		t.Fatalf("session after partial upload = %+v", updated)
	}
	if _, _, _, consumeErr := service.Consume(ctx, types.UploadKindBuildWorkspace, session.ID); !errors.Is(consumeErr, common.ErrUploadSessionIncomplete) {
		t.Fatalf("incomplete Consume error = %v, want ErrUploadSessionIncomplete", consumeErr)
	}

	if _, writeChunkErr4 := service.WriteChunk(ctx, types.UploadKindBuildWorkspace, session.ID, 1, payload[chunkSize:2*chunkSize]); writeChunkErr4 != nil {
		t.Fatalf("WriteChunk(1): %v", writeChunkErr4)
	}
	if _, _, _, consumeErr2 := service.Consume(ctx, types.UploadKindImage, session.ID); !errors.Is(consumeErr2, common.ErrUploadKindMismatch) {
		t.Fatalf("wrong-kind Consume error = %v, want ErrUploadKindMismatch", consumeErr2)
	}

	file, consumed, cleanup, err := service.Consume(ctx, types.UploadKindBuildWorkspace, session.ID)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if !consumed.Complete || consumed.Filename != "artifact.bin" {
		t.Fatalf("consumed session = %+v", consumed)
	}
	// Sessions are single-use: a concurrent second consumer must not see it.
	if _, _, _, consumeErr3 := service.Consume(ctx, types.UploadKindBuildWorkspace, session.ID); !errors.Is(consumeErr3, common.ErrUploadSessionNotFound) {
		t.Fatalf("second Consume error = %v, want ErrUploadSessionNotFound", consumeErr3)
	}
	assembled, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(assembled, payload) {
		t.Fatalf("assembled file differs from input (%d vs %d bytes)", len(assembled), len(payload))
	}
	cleanup()
	if _, statErr := os.Stat(filepath.Join(service.root, session.ID)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("session dir still present after cleanup: %v", statErr)
	}
}

func TestIngestSessionCreatesCompleteSession(t *testing.T) {
	service := &UploadService{root: filepath.Join(t.TempDir(), "uploads")}
	ctx := t.Context()
	payload := []byte("legacy-multipart-body")

	session, err := service.IngestSession(ctx, types.UploadKindBuildWorkspace, "legacy.bin", int64(len(payload)), bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("IngestSession: %v", err)
	}
	if !session.Complete {
		t.Fatalf("ingested session not complete: %+v", session)
	}

	file, _, cleanup, err := service.Consume(ctx, types.UploadKindBuildWorkspace, session.ID)
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

	if _, ingestSessionErr := service.IngestSession(ctx, types.UploadKindBuildWorkspace, "short.bin", 10, bytes.NewReader([]byte("abc"))); ingestSessionErr == nil {
		t.Fatal("IngestSession with short source unexpectedly succeeded")
	}
}

func TestPurgeExpiredSessionsRemovesOnlyStaleSessions(t *testing.T) {
	service := &UploadService{root: filepath.Join(t.TempDir(), "uploads")}
	ctx := t.Context()

	stale, err := service.CreateSession(ctx, types.UploadKindBuildWorkspace, types.CreateUploadSessionRequest{Filename: "stale.bin", Size: 1})
	if err != nil {
		t.Fatalf("CreateSession(stale): %v", err)
	}
	// Expiry is idle-based: backdate every mtime the purge inspects.
	idle := time.Now().Add(-48 * time.Hour)
	for _, name := range []string{"", "meta.json", "data"} {
		if chtimesErr := os.Chtimes(filepath.Join(service.root, stale.ID, name), idle, idle); chtimesErr != nil {
			t.Fatalf("backdate stale session: %v", chtimesErr)
		}
	}
	fresh, err := service.CreateSession(ctx, types.UploadKindBuildWorkspace, types.CreateUploadSessionRequest{Filename: "fresh.bin", Size: 1})
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
	if _, getSessionErr := service.GetSession(ctx, types.UploadKindBuildWorkspace, stale.ID); !errors.Is(getSessionErr, common.ErrUploadSessionNotFound) {
		t.Fatalf("stale session error = %v, want ErrUploadSessionNotFound", getSessionErr)
	}
	if _, getSessionErr2 := service.GetSession(ctx, types.UploadKindBuildWorkspace, fresh.ID); getSessionErr2 != nil {
		t.Fatalf("fresh session should survive purge: %v", getSessionErr2)
	}
}
