package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"tracehub/internal/admin"
	"tracehub/internal/api"
	"tracehub/internal/archive"
	"tracehub/internal/client"
	"tracehub/internal/codex"
	"tracehub/internal/config"
	"tracehub/internal/keys"
	"tracehub/internal/mcpproxy"
	traceServer "tracehub/internal/server"
	"tracehub/internal/store"
	"tracehub/internal/syncer"
)

const e2eSessionID = "019ffdf2-452e-7c60-bd5d-4d88b56ef31b"
const archivedSessionID = "019ffdf2-452e-7c60-bd5d-4d88b56ef32b"

func TestTwoDeviceEndToEndAndMCP(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	serverPrivate, serverPublic := filepath.Join(temp, "server.key"), filepath.Join(temp, "server.pub")
	deviceAPrivate, deviceAPublic := filepath.Join(temp, "device-a.key"), filepath.Join(temp, "device-a.pub")
	deviceBPrivate, deviceBPublic := filepath.Join(temp, "device-b.key"), filepath.Join(temp, "device-b.pub")
	for _, generate := range []func() error{
		func() error { return keys.GenerateServer(serverPrivate, serverPublic) },
		func() error { return keys.GenerateDevice(deviceAPrivate, deviceAPublic) },
		func() error { return keys.GenerateDevice(deviceBPrivate, deviceBPublic) },
	} {
		if err := generate(); err != nil {
			t.Fatal(err)
		}
	}
	serverConfig := config.Server{
		Listen:   "127.0.0.1:0",
		Database: filepath.Join(temp, "tracehub.db"),
		ServerPrivateKeys: map[string]string{
			"server-1": serverPrivate,
		},
		Devices: map[string]config.Device{
			"device-a": {KeyID: "a-1", PublicKey: deviceAPublic, Enabled: true},
			"device-b": {KeyID: "b-1", PublicKey: deviceBPublic, Enabled: true},
		},
	}
	service, err := traceServer.New(serverConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	httpServer := httptest.NewServer(service.Handler())
	defer httpServer.Close()
	codexDir := filepath.Join(temp, "codex")
	sessionDir := filepath.Join(codexDir, "sessions", "2026", "08", "14")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(sessionDir, "rollout-2026-08-14T09-00-00-"+e2eSessionID+".jsonl")
	content := syntheticSession(e2eSessionID)
	if err := os.WriteFile(sessionPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	archivedDir := filepath.Join(codexDir, "archived_sessions")
	if err := os.MkdirAll(archivedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	archivedPath := filepath.Join(archivedDir, "rollout-2026-08-14T09-00-00-"+archivedSessionID+".jsonl")
	if err := os.WriteFile(archivedPath, []byte(syntheticSession(archivedSessionID)), 0o600); err != nil {
		t.Fatal(err)
	}
	deviceA := newRemote(t, config.Client{DeviceID: "device-a", ServerURL: httpServer.URL, CodexDir: codexDir, DeviceKeyID: "a-1", DevicePrivateKey: deviceAPrivate, ServerKeyID: "server-1", ServerPublicKey: serverPublic})
	deviceB := newRemote(t, config.Client{DeviceID: "device-b", ServerURL: httpServer.URL, CodexDir: codexDir, DeviceKeyID: "b-1", DevicePrivateKey: deviceBPrivate, ServerKeyID: "server-1", ServerPublicKey: serverPublic})
	var syncOutput bytes.Buffer
	result, err := syncer.Run(ctx, codexDir, false, deviceA, &syncOutput)
	if err != nil || result.Sessions != 1 || result.Chunks != 1 {
		t.Fatalf("initial sync: %+v err=%v", result, err)
	}
	if !strings.Contains(syncOutput.String(), e2eSessionID) || strings.Contains(syncOutput.String(), archivedSessionID) {
		t.Fatalf("sync output included a disallowed source: %s", syncOutput.String())
	}
	if _, err := deviceB.Session(ctx, "device-a", codex.AgentType, archivedSessionID); err == nil {
		t.Fatal("excluded archived session was uploaded")
	}
	result, err = syncer.Run(ctx, codexDir, false, deviceA, io.Discard)
	if err != nil || result.Chunks != 0 {
		t.Fatalf("idempotent sync: %+v err=%v", result, err)
	}
	rewritten := strings.Replace(content, "deploy model", "deploy modal", 1)
	if err := os.WriteFile(sessionPath, []byte(rewritten), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := syncer.Run(ctx, codexDir, false, deviceA, io.Discard); err == nil || !strings.Contains(err.Error(), "prefix was rewritten") {
		t.Fatalf("rewritten source prefix was not rejected: %v", err)
	}
	if err := os.WriteFile(sessionPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	search, err := deviceB.Search(ctx, store.SearchFilter{Query: "deploy model"})
	if err != nil || len(search.Sessions) != 1 {
		t.Fatalf("cross-device search: %+v err=%v", search, err)
	}
	search, err = deviceB.Search(ctx, store.SearchFilter{RepositoryURL: "ssh://git@example/model.git", CWD: "/work/model"})
	if err != nil || len(search.Sessions) != 1 {
		t.Fatalf("metadata search: %+v err=%v", search, err)
	}
	search, err = deviceB.Search(ctx, store.SearchFilter{RepositoryURL: "SSH://git@example/model.git"})
	if err != nil || len(search.Sessions) != 0 {
		t.Fatalf("repository filter was not case-sensitive: %+v err=%v", search, err)
	}
	events, err := deviceB.Events(ctx, "device-a", codex.AgentType, e2eSessionID, 0, 0, 50)
	if err != nil || !events.UntrustedHistoricalData || len(events.Events) != 4 {
		t.Fatalf("read events: %+v err=%v", events, err)
	}
	var toolSeq int64
	for _, event := range events.Events {
		if event.Kind == "tool_output" {
			toolSeq = event.Seq
		}
		if strings.Contains(event.Text, "hidden reasoning") {
			t.Fatal("reasoning leaked through event API")
		}
	}
	toolOutput, err := deviceB.ToolOutput(ctx, "device-a", codex.AgentType, e2eSessionID, toolSeq)
	if err != nil || toolOutput.Output != "tool secret output" || !toolOutput.UntrustedHistoricalData {
		t.Fatalf("tool output: %+v err=%v", toolOutput, err)
	}
	badLine := []byte("{\"timestamp\":\"2026-08-14T01:05:30Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"agent_message\",\"message\":\"must not persist\"}}\n")
	badCiphertext, err := archive.Encrypt(badLine, deviceA.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deviceA.Upload(ctx, e2eSessionID, int64(len(content)), int64(len(content)+len(badLine)), strings.Repeat("0", 64), strings.Repeat("0", 64), int64(len(badLine)), badCiphertext); err == nil {
		t.Fatal("wrong plaintext hash was accepted")
	}
	validLine := []byte("{\"timestamp\":\"2026-08-14T01:05:45Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"agent_message\",\"message\":\"must roll back\"}}\n")
	validCiphertext, err := archive.Encrypt(validLine, deviceA.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deviceA.Upload(ctx, e2eSessionID, int64(len(content)), int64(len(content)+len(validLine)), client.PlainSHA256(validLine), strings.Repeat("0", 64), int64(len(validLine)), validCiphertext); err == nil {
		t.Fatal("wrong cumulative prefix hash was accepted")
	}
	invalidLine := []byte("not-json\n")
	invalidCiphertext, err := archive.Encrypt(invalidLine, deviceA.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deviceA.Upload(ctx, e2eSessionID, int64(len(content)), int64(len(content)+len(invalidLine)), client.PlainSHA256(invalidLine), client.PlainSHA256(append([]byte(content), invalidLine...)), int64(len(invalidLine)), invalidCiphertext); err == nil {
		t.Fatal("invalid JSON chunk was accepted")
	}
	info, err := deviceB.Session(ctx, "device-a", codex.AgentType, e2eSessionID)
	if err != nil || info.NextOffset != int64(len(content)) {
		t.Fatalf("failed upload changed offset: %+v err=%v", info, err)
	}

	appendFile(t, sessionPath, "{\"timestamp\":\"2026-08-14T01:06:00Z\",\"type\":\"event_msg\",\"payload\":")
	result, err = syncer.Run(ctx, codexDir, false, deviceA, io.Discard)
	if err != nil || result.Chunks != 0 {
		t.Fatalf("partial-line sync: %+v err=%v", result, err)
	}
	appendFile(t, sessionPath, "{\"type\":\"agent_message\",\"message\":\"later\",\"phase\":\"final\"}}\n")
	content += "{\"timestamp\":\"2026-08-14T01:06:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"agent_message\",\"message\":\"later\",\"phase\":\"final\"}}\n"
	result, err = syncer.Run(ctx, codexDir, false, deviceA, io.Discard)
	if err != nil || result.Chunks != 1 {
		t.Fatalf("append sync: %+v err=%v", result, err)
	}
	largeMessage := strings.Repeat("界", 100000)
	largeLine := fmt.Sprintf("{\"timestamp\":\"2026-08-14T01:07:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"agent_message\",\"message\":%q,\"phase\":\"final\"}}\n", largeMessage)
	appendFile(t, sessionPath, largeLine)
	content += largeLine
	result, err = syncer.Run(ctx, codexDir, false, deviceA, io.Discard)
	if err != nil || result.Chunks != 1 {
		t.Fatalf("large-message sync: %+v err=%v", result, err)
	}

	testMCP(t, deviceB, toolSeq, largeMessage)
	exportPath := filepath.Join(temp, "export", e2eSessionID+".jsonl")
	if err := admin.Export(ctx, serverConfig, "device-a", e2eSessionID, exportPath); err != nil {
		t.Fatal(err)
	}
	exported, err := os.ReadFile(exportPath)
	if err != nil || string(exported) != content {
		t.Fatalf("export mismatch: bytes=%d err=%v", len(exported), err)
	}
	if err := admin.Delete(ctx, serverConfig, "device-a", e2eSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := deviceB.Session(ctx, "device-a", codex.AgentType, e2eSessionID); err == nil {
		t.Fatal("deleted session is still queryable")
	}
}

func testMCP(t *testing.T, remote *client.Client, toolSeq int64, largeMessage string) {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := mcpproxy.NewServer(remote).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "tracehub-test", Version: "0.1.0-alpha.3"}, nil)
	clientSession, err := mcpClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	devices, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "list_devices", Arguments: map[string]any{}})
	if err != nil || devices.IsError || len(devices.Content) == 0 || !strings.Contains(devices.Content[0].(*mcp.TextContent).Text, "device-a") || !strings.Contains(devices.Content[0].(*mcp.TextContent).Text, "device-b") {
		t.Fatalf("MCP list_devices failed: %+v err=%v", devices, err)
	}
	search, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "search_sessions", Arguments: map[string]any{"query": "deploy model", "repository_url": "ssh://git@example/model.git", "cwd": "/work/model"}})
	if err != nil || search.IsError || len(search.Content) == 0 {
		t.Fatalf("MCP search failed: %+v err=%v", search, err)
	}
	text := search.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, e2eSessionID) {
		t.Fatalf("MCP search omitted session: %s", text)
	}
	info, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "get_session_info", Arguments: map[string]any{"device_id": "device-a", "session_id": e2eSessionID}})
	if err != nil || info.IsError || len(info.Content) == 0 || !strings.Contains(info.Content[0].(*mcp.TextContent).Text, "ssh://git@example/model.git") {
		t.Fatalf("MCP get_session_info failed: %+v err=%v", info, err)
	}
	tool, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "read_tool_output", Arguments: map[string]any{"device_id": "device-a", "session_id": e2eSessionID, "seq": toolSeq}})
	if err != nil || tool.IsError || len(tool.Content) == 0 || !strings.Contains(tool.Content[0].(*mcp.TextContent).Text, "tool secret output") {
		t.Fatalf("MCP read_tool_output failed: %+v err=%v", tool, err)
	}
	var allText strings.Builder
	var afterSeq int64
	var afterTextOffset int
	pages := 0
	for ; pages < 10; pages++ {
		read, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "read_session", Arguments: map[string]any{"device_id": "device-a", "session_id": e2eSessionID, "after_seq": afterSeq, "after_text_offset": afterTextOffset}})
		if err != nil || read.IsError || len(read.Content) == 0 {
			t.Fatalf("MCP read failed: %+v err=%v", read, err)
		}
		readText := read.Content[0].(*mcp.TextContent).Text
		if !strings.Contains(readText, "untrusted_historical_data") || strings.Contains(readText, "hidden reasoning") {
			t.Fatalf("MCP trust boundary failed: %s", readText)
		}
		var response api.EventsResponse
		if err := json.Unmarshal([]byte(readText), &response); err != nil {
			t.Fatalf("decode MCP read response: %v: %s", err, readText)
		}
		if len(response.Events) == 0 {
			break
		}
		for _, event := range response.Events {
			allText.WriteString(event.Text)
		}
		afterSeq, afterTextOffset = response.NextSeq, response.NextTextOffset
	}
	if pages < 2 || !strings.Contains(allText.String(), largeMessage) {
		t.Fatalf("MCP pagination omitted large message after %d pages", pages)
	}
}

func newRemote(t *testing.T, cfg config.Client) *client.Client {
	t.Helper()
	remote, err := client.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return remote
}

func appendFile(t *testing.T, path, value string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(value); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func syntheticSession(id string) string {
	return fmt.Sprintf("{\"timestamp\":\"2026-08-14T01:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":%q,\"timestamp\":\"2026-08-14T01:00:00Z\",\"cwd\":\"/work/model\",\"source\":\"cli\",\"git\":{\"repository_url\":\"ssh://git@example/model.git\",\"branch\":\"main\",\"commit_hash\":\"abc123\"}}}\n", id) +
		"{\"timestamp\":\"2026-08-14T01:01:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":\"deploy model\"}}\n" +
		"{\"timestamp\":\"2026-08-14T01:02:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"agent_message\",\"message\":\"working\",\"phase\":\"commentary\"}}\n" +
		"{\"timestamp\":\"2026-08-14T01:03:00Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"custom_tool_call\",\"name\":\"exec\",\"status\":\"completed\"}}\n" +
		"{\"timestamp\":\"2026-08-14T01:04:00Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"custom_tool_call_output\",\"output\":\"tool secret output\"}}\n" +
		"{\"timestamp\":\"2026-08-14T01:05:00Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"reasoning\",\"summary\":\"hidden reasoning\"}}\n"
}
