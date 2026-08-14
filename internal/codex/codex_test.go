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

func sessionLine(id string) string {
	return fmt.Sprintf("{\"timestamp\":\"2026-08-14T01:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":%q,\"timestamp\":\"2026-08-14T01:00:00Z\",\"cwd\":\"/work\",\"forked_from_id\":\"parent\",\"source\":\"cli\"}}\n", id)
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
	if source.SessionID != testSessionID || source.CWD != "/work" {
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
