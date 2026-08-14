package codex

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testSessionID = "019ffdf2-452e-7c60-bd5d-4d88b56ef31b"
const secondSessionID = "019ffdf2-452e-7c60-bd5d-4d88b56ef32b"

func sessionLine(id string) string {
	return sessionLineWithSource(id, `"cli"`)
}

func sessionLineWithSource(id, source string) string {
	return fmt.Sprintf("{\"timestamp\":\"2026-08-14T01:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":%q,\"timestamp\":\"2026-08-14T01:00:00Z\",\"cwd\":\"/work\",\"forked_from_id\":\"parent\",\"source\":%s}}\n", id, source)
}

func TestInspectParseAndIgnoreDuplicates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions", "2026", "08", "14")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-08-14T09-00-00-"+testSessionID+".jsonl")
	content := sessionLine(testSessionID) +
		"{\"timestamp\":\"2026-08-14T01:01:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":\"hello\"}}\n" +
		"{\"timestamp\":\"2026-08-14T01:01:00Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\"}}\n" +
		"{\"timestamp\":\"2026-08-14T01:02:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"agent_message\",\"message\":\"done\",\"phase\":\"final\"}}\n" +
		"{\"timestamp\":\"2026-08-14T01:03:00Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"custom_tool_call_output\",\"output\":\"ok\"}}\n" +
		"{\"timestamp\":\"2026-08-14T01:04:00Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"reasoning\"}}\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if source.SessionID != testSessionID || source.CWD != "/work" || source.Source != "cli" {
		t.Fatalf("unexpected source: %+v", source)
	}
	parsed, err := ParseChunk([]byte(content), 0, testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Events) != 3 {
		t.Fatalf("got %d indexed events, want 3", len(parsed.Events))
	}
	if parsed.Events[0].Kind != "user_message" || parsed.Events[1].Kind != "agent_message" || parsed.Events[2].Kind != "tool_output" {
		t.Fatalf("unexpected events: %+v", parsed.Events)
	}
}

func TestSessionMetaSourceShapes(t *testing.T) {
	objectLine := sessionLineWithSource(testSessionID, `{ "subagent": "review" }`)
	meta, err := parseSessionMeta([]byte(objectLine))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Source != `{"subagent":"review"}` {
		t.Fatalf("object source was not compacted: %q", meta.Source)
	}
	stringMeta, err := parseSessionMeta([]byte(sessionLine(testSessionID)))
	if err != nil || stringMeta.Source != "cli" {
		t.Fatalf("string source changed: %+v err=%v", stringMeta, err)
	}
	for _, source := range []string{"null", "[]", "1", "true", `{"subagent":`} {
		t.Run(source, func(t *testing.T) {
			if _, err := parseSessionMeta([]byte(sessionLineWithSource(testSessionID, source))); err == nil {
				t.Fatalf("invalid source %s was accepted", source)
			}
		})
	}
}

func TestDiscoverConfiguredSources(t *testing.T) {
	t.Run("sessions only", func(t *testing.T) {
		root := t.TempDir()
		writeCodexSession(t, root, "sessions", testSessionID)
		writeCodexSession(t, root, "archived_sessions", secondSessionID)
		sources, err := Discover(root, false)
		if err != nil || len(sources) != 1 || sources[0].SessionID != testSessionID {
			t.Fatalf("unexpected discovery: %+v err=%v", sources, err)
		}
	})

	t.Run("explicit archives", func(t *testing.T) {
		root := t.TempDir()
		writeCodexSession(t, root, "sessions", testSessionID)
		writeCodexSession(t, root, "archived_sessions", secondSessionID)
		sources, err := Discover(root, true)
		if err != nil || len(sources) != 2 {
			t.Fatalf("unexpected discovery: %+v err=%v", sources, err)
		}
	})

	t.Run("missing directories", func(t *testing.T) {
		root := t.TempDir()
		if _, err := Discover(root, false); err == nil {
			t.Fatal("missing sessions directory was accepted")
		}
		writeCodexSession(t, root, "archived_sessions", secondSessionID)
		if _, err := Discover(root, false); err == nil {
			t.Fatal("excluded archive directory satisfied discovery")
		}
		sources, err := Discover(root, true)
		if err != nil || len(sources) != 1 || sources[0].SessionID != secondSessionID {
			t.Fatalf("included archive directory was not discovered: %+v err=%v", sources, err)
		}
	})

	t.Run("duplicate boundary", func(t *testing.T) {
		root := t.TempDir()
		writeCodexSession(t, root, "sessions", testSessionID)
		writeCodexSession(t, root, "archived_sessions", testSessionID)
		if sources, err := Discover(root, false); err != nil || len(sources) != 1 {
			t.Fatalf("excluded duplicate blocked discovery: %+v err=%v", sources, err)
		}
		if _, err := Discover(root, true); err == nil || !strings.Contains(err.Error(), "duplicate Codex session") {
			t.Fatalf("included duplicate was not rejected: %v", err)
		}
	})
}

func writeCodexSession(t *testing.T, root, subdir, id string) string {
	t.Helper()
	dir := filepath.Join(root, subdir, "2026", "08", "14")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-08-14T09-00-00-"+id+".jsonl")
	if err := os.WriteFile(path, []byte(sessionLine(id)), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadChunksLeavesPartialLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	complete := sessionLine(testSessionID) + "{\"timestamp\":\"2026-08-14T01:01:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":\"hello\"}}\n"
	if err := os.WriteFile(path, []byte(complete+"{\"partial\":"), 0o600); err != nil {
		t.Fatal(err)
	}
	var got []byte
	offset, err := ReadChunks(path, 0, func(_, _ int64, plain []byte) error {
		got = append(got, plain...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if offset != int64(len(complete)) || string(got) != complete {
		t.Fatalf("partial line was not excluded: offset=%d", offset)
	}
}

func TestReadLineRejectsOver64MiB(t *testing.T) {
	reader := bufio.NewReaderSize(strings.NewReader(strings.Repeat("x", MaxLine+1)), 64<<10)
	if _, _, err := readLine(reader); err == nil {
		t.Fatal("oversized JSONL line was accepted")
	}
}

func TestReadChunksAccepts15MiBRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.jsonl")
	message := strings.Repeat("x", 15<<20)
	largeLine := fmt.Sprintf("{\"timestamp\":\"2026-08-14T01:01:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":%q}}\n", message)
	content := sessionLine(testSessionID) + largeLine
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var sizes []int
	_, err := ReadChunks(path, 0, func(_, _ int64, plain []byte) error {
		sizes = append(sizes, len(plain))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sizes) != 2 || sizes[1] < 15<<20 {
		t.Fatalf("large record was not isolated: %v", sizes)
	}
}

func TestParseRejectsWrongSessionAndInvalidJSON(t *testing.T) {
	if _, err := ParseChunk([]byte(sessionLine("11111111-1111-1111-1111-111111111111")), 0, testSessionID); err == nil {
		t.Fatal("wrong session ID was accepted")
	}
	if _, err := ParseChunk(bytes.Join([][]byte{[]byte(sessionLine(testSessionID)), []byte("not-json\n")}, nil), 0, testSessionID); err == nil {
		t.Fatal("invalid JSON was accepted")
	}
}
