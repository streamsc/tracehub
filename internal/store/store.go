package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"

	"tracehub/internal/codex"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

type Chunk struct {
	DeviceID    string
	AgentType   string
	SessionID   string
	StartOffset int64
	EndOffset   int64
	ServerKeyID string
	PlainSHA256 string
	PlainSize   int64
	Ciphertext  []byte
}

type Session struct {
	ID             int64  `json:"id"`
	DeviceID       string `json:"device_id"`
	AgentType      string `json:"agent_type"`
	SessionID      string `json:"session_id"`
	NextOffset     int64  `json:"next_offset"`
	CWD            string `json:"cwd,omitempty"`
	StartedAt      string `json:"started_at,omitempty"`
	LastActivityAt string `json:"last_activity_at,omitempty"`
	ForkedFromID   string `json:"forked_from_id,omitempty"`
	Source         string `json:"source,omitempty"`
}

type SearchFilter struct {
	DeviceID string `json:"device_id,omitempty"`
	Query    string `json:"query,omitempty"`
	Start    string `json:"start,omitempty"`
	End      string `json:"end,omitempty"`
	AfterID  int64  `json:"after_id,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type Event struct {
	Seq        int64  `json:"seq"`
	Kind       string `json:"kind"`
	OccurredAt string `json:"occurred_at,omitempty"`
	Phase      string `json:"phase,omitempty"`
	Text       string `json:"text,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolStatus string `json:"tool_status,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.initialize(); err != nil {
		db.Close()
		return nil, err
	}
	for _, databaseFile := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(databaseFile, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			db.Close()
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) initialize() error {
	schema := `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
PRAGMA secure_delete=ON;
CREATE TABLE IF NOT EXISTS sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  device_id TEXT NOT NULL,
  agent_type TEXT NOT NULL,
  session_id TEXT NOT NULL,
  next_offset INTEGER NOT NULL DEFAULT 0,
  cwd TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL DEFAULT '',
  last_activity_at TEXT NOT NULL DEFAULT '',
  forked_from_id TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(device_id, agent_type, session_id)
);
CREATE TABLE IF NOT EXISTS chunks (
  device_id TEXT NOT NULL,
  agent_type TEXT NOT NULL,
  session_id TEXT NOT NULL,
  start_offset INTEGER NOT NULL,
  end_offset INTEGER NOT NULL,
  server_key_id TEXT NOT NULL,
  plain_sha256 TEXT NOT NULL,
  plain_size INTEGER NOT NULL,
  ciphertext BLOB NOT NULL,
  received_at TEXT NOT NULL,
  PRIMARY KEY(device_id, agent_type, session_id, start_offset),
  FOREIGN KEY(device_id, agent_type, session_id) REFERENCES sessions(device_id, agent_type, session_id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  device_id TEXT NOT NULL,
  agent_type TEXT NOT NULL,
  session_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  chunk_start INTEGER NOT NULL,
  kind TEXT NOT NULL,
  occurred_at TEXT NOT NULL DEFAULT '',
  phase TEXT NOT NULL DEFAULT '',
  text TEXT NOT NULL DEFAULT '',
  tool_name TEXT NOT NULL DEFAULT '',
  tool_status TEXT NOT NULL DEFAULT '',
  UNIQUE(device_id, agent_type, session_id, seq),
  FOREIGN KEY(device_id, agent_type, session_id) REFERENCES sessions(device_id, agent_type, session_id) ON DELETE CASCADE
);
CREATE VIRTUAL TABLE IF NOT EXISTS events_fts USING fts5(text, device_id UNINDEXED, agent_type UNINDEXED, session_id UNINDEXED, seq UNINDEXED);
INSERT INTO events_fts(events_fts, rank) VALUES('secure-delete', 1);
`
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) NextOffsets(ctx context.Context, deviceID, agentType string, sessionIDs []string) (map[string]int64, error) {
	result := make(map[string]int64, len(sessionIDs))
	for _, id := range sessionIDs {
		var offset int64
		err := s.db.QueryRowContext(ctx, `SELECT next_offset FROM sessions WHERE device_id=? AND agent_type=? AND session_id=?`, deviceID, agentType, id).Scan(&offset)
		if errors.Is(err, sql.ErrNoRows) {
			result[id] = 0
			continue
		}
		if err != nil {
			return nil, err
		}
		result[id] = offset
	}
	return result, nil
}

func (s *Store) PutChunk(ctx context.Context, chunk Chunk, parsed codex.ParsedChunk) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var next int64
	err = tx.QueryRowContext(ctx, `SELECT next_offset FROM sessions WHERE device_id=? AND agent_type=? AND session_id=?`, chunk.DeviceID, chunk.AgentType, chunk.SessionID).Scan(&next)
	if errors.Is(err, sql.ErrNoRows) {
		if chunk.StartOffset != 0 || parsed.Metadata == nil {
			return false, fmt.Errorf("new session must begin at offset 0 with session metadata")
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		meta := parsed.Metadata
		_, err = tx.ExecContext(ctx, `INSERT INTO sessions(device_id,agent_type,session_id,next_offset,cwd,started_at,last_activity_at,forked_from_id,source,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, chunk.DeviceID, chunk.AgentType, chunk.SessionID, 0, meta.CWD, meta.StartedAt, parsed.LastActivity, meta.ForkedFromID, meta.Source, now, now)
		if err != nil {
			return false, err
		}
		next = 0
	} else if err != nil {
		return false, err
	}
	if chunk.StartOffset < next {
		var end int64
		var hash string
		err := tx.QueryRowContext(ctx, `SELECT end_offset,plain_sha256 FROM chunks WHERE device_id=? AND agent_type=? AND session_id=? AND start_offset=?`, chunk.DeviceID, chunk.AgentType, chunk.SessionID, chunk.StartOffset).Scan(&end, &hash)
		if err == nil && end == chunk.EndOffset && hash == chunk.PlainSHA256 {
			return true, tx.Commit()
		}
		return false, fmt.Errorf("conflicting chunk at offset %d", chunk.StartOffset)
	}
	if chunk.StartOffset != next {
		return false, fmt.Errorf("offset conflict: expected %d, got %d", next, chunk.StartOffset)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO chunks(device_id,agent_type,session_id,start_offset,end_offset,server_key_id,plain_sha256,plain_size,ciphertext,received_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, chunk.DeviceID, chunk.AgentType, chunk.SessionID, chunk.StartOffset, chunk.EndOffset, chunk.ServerKeyID, chunk.PlainSHA256, chunk.PlainSize, chunk.Ciphertext, now)
	if err != nil {
		return false, err
	}
	for _, event := range parsed.Events {
		result, err := tx.ExecContext(ctx, `INSERT INTO events(device_id,agent_type,session_id,seq,chunk_start,kind,occurred_at,phase,text,tool_name,tool_status) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, chunk.DeviceID, chunk.AgentType, chunk.SessionID, event.Seq, chunk.StartOffset, event.Kind, event.OccurredAt, event.Phase, event.Text, event.ToolName, event.ToolStatus)
		if err != nil {
			return false, err
		}
		rowID, err := result.LastInsertId()
		if err != nil {
			return false, err
		}
		searchText := strings.TrimSpace(event.Text + " " + event.ToolName + " " + event.ToolStatus)
		if searchText != "" {
			_, err = tx.ExecContext(ctx, `INSERT INTO events_fts(rowid,text,device_id,agent_type,session_id,seq) VALUES(?,?,?,?,?,?)`, rowID, searchText, chunk.DeviceID, chunk.AgentType, chunk.SessionID, event.Seq)
			if err != nil {
				return false, err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE sessions SET next_offset=?,last_activity_at=CASE WHEN last_activity_at>? THEN last_activity_at ELSE ? END,updated_at=? WHERE device_id=? AND agent_type=? AND session_id=?`, chunk.EndOffset, parsed.LastActivity, parsed.LastActivity, now, chunk.DeviceID, chunk.AgentType, chunk.SessionID)
	if err != nil {
		return false, err
	}
	return false, tx.Commit()
}

func (s *Store) SearchSessions(ctx context.Context, filter SearchFilter) ([]Session, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	args := []any{filter.AfterID}
	where := []string{"s.id > ?"}
	join := ""
	if filter.Query != "" {
		join = ` JOIN events e ON e.device_id=s.device_id AND e.agent_type=s.agent_type AND e.session_id=s.session_id JOIN events_fts ON events_fts.rowid=e.id`
		where = append(where, "events_fts MATCH ?")
		args = append(args, quoteFTS(filter.Query))
	}
	if filter.DeviceID != "" {
		where = append(where, "s.device_id=?")
		args = append(args, filter.DeviceID)
	}
	if filter.Start != "" {
		where = append(where, "s.last_activity_at>=?")
		args = append(args, filter.Start)
	}
	if filter.End != "" {
		where = append(where, "s.last_activity_at<?")
		args = append(args, filter.End)
	}
	args = append(args, limit)
	query := `SELECT DISTINCT s.id,s.device_id,s.agent_type,s.session_id,s.next_offset,s.cwd,s.started_at,s.last_activity_at,s.forked_from_id,s.source FROM sessions s` + join + ` WHERE ` + strings.Join(where, " AND ") + ` ORDER BY s.id LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		var session Session
		if err := rows.Scan(&session.ID, &session.DeviceID, &session.AgentType, &session.SessionID, &session.NextOffset, &session.CWD, &session.StartedAt, &session.LastActivityAt, &session.ForkedFromID, &session.Source); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (s *Store) Session(ctx context.Context, deviceID, agentType, sessionID string) (Session, error) {
	var session Session
	err := s.db.QueryRowContext(ctx, `SELECT id,device_id,agent_type,session_id,next_offset,cwd,started_at,last_activity_at,forked_from_id,source FROM sessions WHERE device_id=? AND agent_type=? AND session_id=?`, deviceID, agentType, sessionID).Scan(&session.ID, &session.DeviceID, &session.AgentType, &session.SessionID, &session.NextOffset, &session.CWD, &session.StartedAt, &session.LastActivityAt, &session.ForkedFromID, &session.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return session, err
}

func (s *Store) Events(ctx context.Context, deviceID, agentType, sessionID string, after int64, limit int, maxBytes int) ([]Event, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seq,kind,occurred_at,phase,text,tool_name,tool_status FROM events WHERE device_id=? AND agent_type=? AND session_id=? AND seq>? ORDER BY seq LIMIT ?`, deviceID, agentType, sessionID, after, limit)
	if err != nil {
		return nil, after, err
	}
	defer rows.Close()
	var events []Event
	next := after
	used := 0
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.Seq, &event.Kind, &event.OccurredAt, &event.Phase, &event.Text, &event.ToolName, &event.ToolStatus); err != nil {
			return nil, after, err
		}
		remaining := maxBytes - used
		if remaining <= 0 {
			break
		}
		if len(event.Text) > remaining {
			event.Text = truncateUTF8(event.Text, remaining)
			event.Truncated = true
		}
		used += len(event.Text) + len(event.ToolName) + 128
		events = append(events, event)
		next = event.Seq
	}
	return events, next, rows.Err()
}

func (s *Store) EventChunk(ctx context.Context, deviceID, agentType, sessionID string, seq int64) (Chunk, error) {
	var chunk Chunk
	err := s.db.QueryRowContext(ctx, `SELECT c.device_id,c.agent_type,c.session_id,c.start_offset,c.end_offset,c.server_key_id,c.plain_sha256,c.plain_size,c.ciphertext FROM events e JOIN chunks c ON c.device_id=e.device_id AND c.agent_type=e.agent_type AND c.session_id=e.session_id AND c.start_offset=e.chunk_start WHERE e.device_id=? AND e.agent_type=? AND e.session_id=? AND e.seq=? AND e.kind='tool_output'`, deviceID, agentType, sessionID, seq).Scan(&chunk.DeviceID, &chunk.AgentType, &chunk.SessionID, &chunk.StartOffset, &chunk.EndOffset, &chunk.ServerKeyID, &chunk.PlainSHA256, &chunk.PlainSize, &chunk.Ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return Chunk{}, ErrNotFound
	}
	return chunk, err
}

func (s *Store) Chunks(ctx context.Context, deviceID, agentType, sessionID string) ([]Chunk, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device_id,agent_type,session_id,start_offset,end_offset,server_key_id,plain_sha256,plain_size,ciphertext FROM chunks WHERE device_id=? AND agent_type=? AND session_id=? ORDER BY start_offset`, deviceID, agentType, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chunks []Chunk
	for rows.Next() {
		var chunk Chunk
		if err := rows.Scan(&chunk.DeviceID, &chunk.AgentType, &chunk.SessionID, &chunk.StartOffset, &chunk.EndOffset, &chunk.ServerKeyID, &chunk.PlainSHA256, &chunk.PlainSize, &chunk.Ciphertext); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, ErrNotFound
	}
	return chunks, nil
}

func (s *Store) DeleteSession(ctx context.Context, deviceID, agentType, sessionID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `DELETE FROM events_fts WHERE rowid IN (SELECT id FROM events WHERE device_id=? AND agent_type=? AND session_id=?)`, deviceID, agentType, sessionID)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE device_id=? AND agent_type=? AND session_id=?`, deviceID, agentType, sessionID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

func quoteFTS(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }

func truncateUTF8(value string, max int) string {
	if len(value) <= max {
		return value
	}
	value = value[:max]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
