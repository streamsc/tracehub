package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
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
	meta := &codex.Metadata{SessionID: testID, StartedAt: "2026-08-14T01:00:00Z", CWD: "/work", RepositoryURL: "ssh://git@example/repo.git", GitBranch: "main", GitCommitHash: "abc123"}
	parsed := codex.ParsedChunk{Metadata: meta, LastActivity: "2026-08-14T01:01:00Z", Events: []codex.Event{{Seq: 100, Kind: "user_message", Text: "deploy model", OccurredAt: "2026-08-14T01:01:00Z"}}}
	chunk := Chunk{DeviceID: "desktop", AgentType: codex.AgentType, SessionID: testID, StartOffset: 0, EndOffset: 200, ServerKeyID: "server-1", PlainSHA256: "hash", PlainSize: 200, Ciphertext: []byte("cipher")}
	plain := bytes.Repeat([]byte("x"), 200)
	sum := sha256.Sum256(plain)
	prefixHash := hex.EncodeToString(sum[:])
	if duplicate, err := db.PutChunk(ctx, chunk, plain, prefixHash, parsed); err != nil || duplicate {
		t.Fatalf("put chunk: duplicate=%v err=%v", duplicate, err)
	}
	if duplicate, err := db.PutChunk(ctx, chunk, plain, prefixHash, parsed); err != nil || !duplicate {
		t.Fatalf("idempotent put: duplicate=%v err=%v", duplicate, err)
	}
	sessions, err := db.SearchSessions(ctx, SearchFilter{Query: "deploy model"})
	if err != nil || len(sessions) != 1 {
		t.Fatalf("search: sessions=%d err=%v", len(sessions), err)
	}
	for _, filter := range []SearchFilter{{RepositoryURL: meta.RepositoryURL}, {CWD: meta.CWD}, {RepositoryURL: meta.RepositoryURL, CWD: meta.CWD, Query: "deploy model"}} {
		sessions, err := db.SearchSessions(ctx, filter)
		if err != nil || len(sessions) != 1 || sessions[0].RepositoryURL != meta.RepositoryURL {
			t.Fatalf("metadata search %+v: sessions=%+v err=%v", filter, sessions, err)
		}
	}
	for _, filter := range []SearchFilter{{RepositoryURL: "SSH://git@example/repo.git"}, {CWD: "/WORK"}} {
		sessions, err := db.SearchSessions(ctx, filter)
		if err != nil || len(sessions) != 0 {
			t.Fatalf("case-sensitive search %+v returned %+v err=%v", filter, sessions, err)
		}
	}
	events, next, textOffset, err := db.Events(ctx, "desktop", codex.AgentType, testID, 0, 0, 50, 256<<10)
	if err != nil || len(events) != 1 || next != 100 || textOffset != 0 {
		t.Fatalf("events: %+v next=%d text_offset=%d err=%v", events, next, textOffset, err)
	}
	bad := chunk
	bad.StartOffset = 200
	bad.EndOffset = 203
	bad.PlainSize = 3
	bad.PlainSHA256 = "bad"
	if _, err := db.PutChunk(ctx, bad, []byte("bad"), strings.Repeat("0", 64), codex.ParsedChunk{}); err == nil {
		t.Fatal("wrong cumulative prefix hash was accepted")
	}
	bad = chunk
	bad.StartOffset = 300
	bad.EndOffset = 400
	bad.PlainSHA256 = "other"
	if _, err := db.PutChunk(ctx, bad, []byte("bad"), prefixHash, codex.ParsedChunk{}); err == nil {
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

func TestEventsResumeLargeUTF8Message(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "tracehub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	message := strings.Repeat("界", 100000)
	plain := []byte("x")
	sum := sha256.Sum256(plain)
	parsed := codex.ParsedChunk{
		Metadata: &codex.Metadata{SessionID: testID},
		Events:   []codex.Event{{Seq: 100, Kind: "agent_message", Text: message}},
	}
	chunk := Chunk{DeviceID: "desktop", AgentType: codex.AgentType, SessionID: testID, StartOffset: 0, EndOffset: 1, PlainSize: 1, PlainSHA256: hex.EncodeToString(sum[:]), Ciphertext: []byte("cipher")}
	if _, err := db.PutChunk(context.Background(), chunk, plain, hex.EncodeToString(sum[:]), parsed); err != nil {
		t.Fatal(err)
	}
	first, seq, offset, err := db.Events(context.Background(), "desktop", codex.AgentType, testID, 0, 0, 50, 256<<10)
	if err != nil || len(first) != 1 || !first[0].MoreText || offset == 0 || seq != 100 || first[0].TextOffset != 0 {
		t.Fatalf("first page: events=%+v seq=%d offset=%d err=%v", first, seq, offset, err)
	}
	replay, replaySeq, replayOffset, err := db.Events(context.Background(), "desktop", codex.AgentType, testID, 0, 0, 50, 256<<10)
	if err != nil || replaySeq != seq || replayOffset != offset || replay[0].Text != first[0].Text {
		t.Fatalf("cursor replay changed page: seq=%d offset=%d err=%v", replaySeq, replayOffset, err)
	}
	second, seq, nextOffset, err := db.Events(context.Background(), "desktop", codex.AgentType, testID, seq, offset, 50, 256<<10)
	if err != nil || len(second) != 1 || second[0].MoreText || nextOffset != 0 || second[0].TextOffset != offset {
		t.Fatalf("second page: events=%+v seq=%d offset=%d err=%v", second, seq, nextOffset, err)
	}
	if first[0].Text+second[0].Text != message {
		t.Fatal("paged message did not reconstruct original text")
	}
	if _, _, _, err := db.Events(context.Background(), "desktop", codex.AgentType, testID, 100, len(message)+1, 50, 256<<10); err == nil {
		t.Fatal("invalid event text cursor was accepted")
	}
}

func TestOpenMigratesAlphaSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tracehub.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`CREATE TABLE sessions (
id INTEGER PRIMARY KEY AUTOINCREMENT,
device_id TEXT NOT NULL, agent_type TEXT NOT NULL, session_id TEXT NOT NULL,
next_offset INTEGER NOT NULL DEFAULT 0, cwd TEXT NOT NULL DEFAULT '',
started_at TEXT NOT NULL DEFAULT '', last_activity_at TEXT NOT NULL DEFAULT '',
forked_from_id TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT '',
created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
UNIQUE(device_id,agent_type,session_id));`)
	if err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
	columns, err := tableColumnsFromDB(db.db, "sessions")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"repository_url", "git_branch", "git_commit_hash", "prefix_hash_state", "metadata_version"} {
		if !columns[name] {
			t.Fatalf("migration omitted column %s", name)
		}
	}
}

func tableColumnsFromDB(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

const testID = "019ffdf2-452e-7c60-bd5d-4d88b56ef31b"
