package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	AgentType   = "codex"
	TargetChunk = 4 << 20
	MaxLine     = 64 << 20
)

var uuidPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

type Metadata struct {
	SessionID    string `json:"session_id"`
	StartedAt    string `json:"started_at,omitempty"`
	CWD          string `json:"cwd,omitempty"`
	ForkedFromID string `json:"forked_from_id,omitempty"`
	Source       string `json:"source,omitempty"`
}

type Source struct {
	Path string
	Size int64
	Metadata
}

type Event struct {
	Seq        int64  `json:"seq"`
	Kind       string `json:"kind"`
	OccurredAt string `json:"occurred_at,omitempty"`
	Phase      string `json:"phase,omitempty"`
	Text       string `json:"text,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolStatus string `json:"tool_status,omitempty"`
}

type ParsedChunk struct {
	Metadata     *Metadata
	Events       []Event
	LastActivity string
}

func Discover(root string) ([]Source, error) {
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("codex directory %s is not accessible", root)
	}
	byID := make(map[string]Source)
	foundRoot := false
	for _, subdir := range []string{"sessions", "archived_sessions"} {
		dir := filepath.Join(root, subdir)
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		foundRoot = true
		err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
				return nil
			}
			source, err := Inspect(path)
			if err != nil {
				return err
			}
			if previous, ok := byID[source.SessionID]; ok {
				return fmt.Errorf("duplicate Codex session %s: %s and %s", source.SessionID, previous.Path, source.Path)
			}
			byID[source.SessionID] = source
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if !foundRoot {
		return nil, fmt.Errorf("neither sessions nor archived_sessions exists under %s", root)
	}
	sources := make([]Source, 0, len(byID))
	for _, source := range byID {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].SessionID < sources[j].SessionID })
	return sources, nil
}

func Inspect(path string) (Source, error) {
	file, err := os.Open(path)
	if err != nil {
		return Source{}, err
	}
	defer file.Close()
	line, complete, err := readLine(bufio.NewReaderSize(file, 64<<10))
	if err != nil {
		return Source{}, fmt.Errorf("read %s: %w", path, err)
	}
	if !complete {
		return Source{}, fmt.Errorf("first line in %s is incomplete", path)
	}
	meta, err := parseSessionMeta(line)
	if err != nil {
		return Source{}, fmt.Errorf("first line in %s: %w", path, err)
	}
	ids := uuidPattern.FindAllString(filepath.Base(path), -1)
	if len(ids) == 0 || ids[len(ids)-1] != meta.SessionID {
		return Source{}, fmt.Errorf("file UUID does not match first session_meta ID in %s", path)
	}
	info, err := file.Stat()
	if err != nil {
		return Source{}, err
	}
	return Source{Path: path, Size: info.Size(), Metadata: meta}, nil
}

func ReadChunks(path string, start int64, consume func(start, end int64, plain []byte) error) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return start, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return start, err
	}
	if info.Size() < start {
		return start, fmt.Errorf("source file was truncated: size %d is below server offset %d", info.Size(), start)
	}
	if start > 0 {
		if _, err := file.Seek(start-1, io.SeekStart); err != nil {
			return start, err
		}
		boundary := []byte{0}
		if _, err := io.ReadFull(file, boundary); err != nil || boundary[0] != '\n' {
			return start, fmt.Errorf("server offset %d is not a JSONL line boundary", start)
		}
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return start, err
	}
	reader := bufio.NewReaderSize(file, 64<<10)
	offset := start
	chunkStart := start
	chunk := make([]byte, 0, TargetChunk)
	flush := func() error {
		if len(chunk) == 0 {
			return nil
		}
		copyOfChunk := append([]byte(nil), chunk...)
		if err := consume(chunkStart, chunkStart+int64(len(copyOfChunk)), copyOfChunk); err != nil {
			return err
		}
		chunkStart += int64(len(copyOfChunk))
		chunk = chunk[:0]
		return nil
	}
	for {
		line, complete, err := readLine(reader)
		if err != nil {
			return offset, fmt.Errorf("read %s at offset %d: %w", path, offset, err)
		}
		if !complete {
			break
		}
		if len(chunk) > 0 && len(chunk)+len(line) > TargetChunk {
			if err := flush(); err != nil {
				return offset, err
			}
		}
		chunk = append(chunk, line...)
		offset += int64(len(line))
		if len(chunk) >= TargetChunk {
			if err := flush(); err != nil {
				return offset, err
			}
		}
	}
	if err := flush(); err != nil {
		return offset, err
	}
	return offset, nil
}

func ParseChunk(plain []byte, baseOffset int64, expectedSessionID string) (ParsedChunk, error) {
	if len(plain) == 0 || plain[len(plain)-1] != '\n' {
		return ParsedChunk{}, errors.New("chunk does not end at a JSONL line boundary")
	}
	var result ParsedChunk
	offset := 0
	for lineIndex := 0; offset < len(plain); lineIndex++ {
		next := bytes.IndexByte(plain[offset:], '\n')
		if next < 0 {
			return ParsedChunk{}, errors.New("incomplete JSONL record")
		}
		next += offset + 1
		line := plain[offset:next]
		var envelope struct {
			Timestamp string          `json:"timestamp"`
			Type      string          `json:"type"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(bytes.TrimSpace(line), &envelope); err != nil {
			return ParsedChunk{}, fmt.Errorf("invalid JSONL at byte %d: %w", baseOffset+int64(offset), err)
		}
		if envelope.Timestamp > result.LastActivity {
			result.LastActivity = envelope.Timestamp
		}
		if baseOffset == 0 && lineIndex == 0 {
			meta, err := parseSessionMeta(line)
			if err != nil {
				return ParsedChunk{}, err
			}
			if meta.SessionID != expectedSessionID {
				return ParsedChunk{}, fmt.Errorf("session_meta ID %s does not match %s", meta.SessionID, expectedSessionID)
			}
			result.Metadata = &meta
		}
		seq := baseOffset + int64(offset)
		if event, ok, err := extractEvent(envelope.Type, envelope.Timestamp, envelope.Payload, seq); err != nil {
			return ParsedChunk{}, fmt.Errorf("parse event at byte %d: %w", seq, err)
		} else if ok {
			result.Events = append(result.Events, event)
		}
		offset = next
	}
	return result, nil
}

func ToolOutput(record []byte) (string, error) {
	var envelope struct {
		Type    string `json:"type"`
		Payload struct {
			Type   string          `json:"type"`
			Output json.RawMessage `json:"output"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(record), &envelope); err != nil {
		return "", err
	}
	if envelope.Type != "response_item" || (envelope.Payload.Type != "custom_tool_call_output" && envelope.Payload.Type != "function_call_output") {
		return "", errors.New("record is not a tool output")
	}
	var text string
	if json.Unmarshal(envelope.Payload.Output, &text) == nil {
		return text, nil
	}
	return string(envelope.Payload.Output), nil
}

func extractEvent(recordType, timestamp string, payload json.RawMessage, seq int64) (Event, bool, error) {
	if recordType == "event_msg" {
		var value struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Phase   string `json:"phase"`
		}
		if err := json.Unmarshal(payload, &value); err != nil {
			return Event{}, false, err
		}
		if value.Type == "user_message" || value.Type == "agent_message" {
			return Event{Seq: seq, Kind: value.Type, OccurredAt: timestamp, Phase: value.Phase, Text: value.Message}, true, nil
		}
		return Event{}, false, nil
	}
	if recordType != "response_item" {
		return Event{}, false, nil
	}
	var value struct {
		Type   string `json:"type"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(payload, &value); err != nil {
		return Event{}, false, err
	}
	switch value.Type {
	case "custom_tool_call", "function_call":
		return Event{Seq: seq, Kind: "tool_call", OccurredAt: timestamp, ToolName: value.Name, ToolStatus: value.Status}, true, nil
	case "custom_tool_call_output", "function_call_output":
		return Event{Seq: seq, Kind: "tool_output", OccurredAt: timestamp, ToolStatus: value.Status}, true, nil
	default:
		return Event{}, false, nil
	}
}

func parseSessionMeta(line []byte) (Metadata, error) {
	var envelope struct {
		Type    string `json:"type"`
		Payload struct {
			ID           string `json:"id"`
			SessionID    string `json:"session_id"`
			Timestamp    string `json:"timestamp"`
			CWD          string `json:"cwd"`
			ForkedFromID string `json:"forked_from_id"`
			Source       string `json:"source"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(line), &envelope); err != nil {
		return Metadata{}, err
	}
	if envelope.Type != "session_meta" {
		return Metadata{}, errors.New("first record is not session_meta")
	}
	id := envelope.Payload.ID
	if id == "" {
		id = envelope.Payload.SessionID
	}
	if !uuidPattern.MatchString(id) {
		return Metadata{}, errors.New("session_meta has no valid session ID")
	}
	return Metadata{SessionID: id, StartedAt: envelope.Payload.Timestamp, CWD: envelope.Payload.CWD, ForkedFromID: envelope.Payload.ForkedFromID, Source: envelope.Payload.Source}, nil
}

func readLine(reader *bufio.Reader) ([]byte, bool, error) {
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > MaxLine {
			return nil, false, fmt.Errorf("JSONL record exceeds %d bytes", MaxLine)
		}
		switch {
		case err == nil:
			return line, true, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return line, false, nil
		default:
			return nil, false, err
		}
	}
}

func SafeSessionID(id string) bool {
	return uuidPattern.FindString(id) == id && len(id) == 36 && !strings.ContainsAny(id, `/\\`)
}
