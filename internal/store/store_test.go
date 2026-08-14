package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"tracehub/internal/codex"
)

func TestPutSearchEventsAndDelete(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tracehub.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	meta := &codex.Metadata{SessionID: testID, StartedAt: "2026-08-14T01:00:00Z", CWD: "/work"}
	parsed := codex.ParsedChunk{Metadata: meta, LastActivity: "2026-08-14T01:01:00Z", Events: []codex.Event{{Seq: 100, Kind: "user_message", Text: "deploy model", OccurredAt: "2026-08-14T01:01:00Z"}}}
	chunk := Chunk{DeviceID: "desktop", AgentType: codex.AgentType, SessionID: testID, StartOffset: 0, EndOffset: 200, ServerKeyID: "server-1", PlainSHA256: "hash", PlainSize: 200, Ciphertext: []byte("cipher")}
	if duplicate, err := db.PutChunk(ctx, chunk, parsed); err != nil || duplicate {
		t.Fatalf("put chunk: duplicate=%v err=%v", duplicate, err)
	}
	if duplicate, err := db.PutChunk(ctx, chunk, parsed); err != nil || !duplicate {
		t.Fatalf("idempotent put: duplicate=%v err=%v", duplicate, err)
	}
	sessions, err := db.SearchSessions(ctx, SearchFilter{Query: "deploy model"})
	if err != nil || len(sessions) != 1 {
		t.Fatalf("search: sessions=%d err=%v", len(sessions), err)
	}
	events, next, err := db.Events(ctx, "desktop", codex.AgentType, testID, 0, 50, 256<<10)
	if err != nil || len(events) != 1 || next != 100 {
		t.Fatalf("events: %+v next=%d err=%v", events, next, err)
	}
	bad := chunk
	bad.StartOffset = 300
	bad.EndOffset = 400
	bad.PlainSHA256 = "other"
	if _, err := db.PutChunk(ctx, bad, codex.ParsedChunk{}); err == nil {
		t.Fatal("offset gap was accepted")
	}
	if session, err := db.Session(ctx, "desktop", codex.AgentType, testID); err != nil || session.NextOffset != 200 {
		t.Fatalf("failed transaction changed offset: %+v err=%v", session, err)
	}
	if err := db.DeleteSession(ctx, "desktop", codex.AgentType, testID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Session(ctx, "desktop", codex.AgentType, testID); err != ErrNotFound {
		t.Fatalf("deleted session still exists: %v", err)
	}
	for _, path := range []string{dbPath, dbPath + "-wal"} {
		content, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if bytes.Contains(content, []byte("deploy model")) {
			t.Fatalf("deleted plaintext remains in %s", path)
		}
	}
}

const testID = "019ffdf2-452e-7c60-bd5d-4d88b56ef31b"
