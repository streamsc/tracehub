package server_test

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tracehub/internal/api"
	"tracehub/internal/config"
	"tracehub/internal/keys"
	traceServer "tracehub/internal/server"
	"tracehub/internal/store"
	"tracehub/internal/syncer"
)

func TestServerBackfillsAlphaSessions(t *testing.T) {
	t.Run("valid archive", func(t *testing.T) {
		cfg, clientConfig := seedPendingBackfill(t, false)
		service, err := traceServer.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer service.Close()
		httpServer := httptest.NewServer(service.Handler())
		defer httpServer.Close()
		clientConfig.ServerURL = httpServer.URL
		remote := newRemote(t, clientConfig)
		search, err := remote.Search(context.Background(), store.SearchFilter{RepositoryURL: "ssh://git@example/model.git"})
		if err != nil || len(search.Sessions) != 1 {
			t.Fatalf("backfilled metadata search: %+v err=%v", search, err)
		}
		checkpoints, err := remote.Plan(context.Background(), []api.LocalSession{{SessionID: e2eSessionID, Size: search.Sessions[0].NextOffset}})
		if err != nil || checkpoints[e2eSessionID].PrefixSHA256 == "" {
			t.Fatalf("post-backfill plan failed: %+v err=%v", checkpoints, err)
		}
	})

	t.Run("tampered archive", func(t *testing.T) {
		cfg, _ := seedPendingBackfill(t, true)
		service, err := traceServer.New(cfg)
		if service != nil {
			service.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "backfill stored sessions") {
			t.Fatalf("tampered archive did not block startup: %v", err)
		}
	})
}

func seedPendingBackfill(t *testing.T, tamper bool) (config.Server, config.Client) {
	t.Helper()
	temp := t.TempDir()
	serverPrivate, serverPublic := filepath.Join(temp, "server.key"), filepath.Join(temp, "server.pub")
	devicePrivate, devicePublic := filepath.Join(temp, "device.key"), filepath.Join(temp, "device.pub")
	if err := keys.GenerateServer(serverPrivate, serverPublic); err != nil {
		t.Fatal(err)
	}
	if err := keys.GenerateDevice(devicePrivate, devicePublic); err != nil {
		t.Fatal(err)
	}
	cfg := config.Server{
		Database:          filepath.Join(temp, "tracehub.db"),
		ServerPrivateKeys: map[string]string{"server-1": serverPrivate},
		Devices:           map[string]config.Device{"device-a": {KeyID: "a-1", PublicKey: devicePublic, Enabled: true}},
	}
	service, err := traceServer.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(service.Handler())
	codexDir := filepath.Join(temp, "codex")
	sessionDir := filepath.Join(codexDir, "sessions", "2026", "08", "14")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessionDir, "rollout-2026-08-14T09-00-00-"+e2eSessionID+".jsonl")
	if err := os.WriteFile(path, []byte(syntheticSession(e2eSessionID)), 0o600); err != nil {
		t.Fatal(err)
	}
	clientConfig := config.Client{DeviceID: "device-a", ServerURL: httpServer.URL, CodexDir: codexDir, DeviceKeyID: "a-1", DevicePrivateKey: devicePrivate, ServerKeyID: "server-1", ServerPublicKey: serverPublic}
	remote := newRemote(t, clientConfig)
	if result, err := syncer.Run(context.Background(), codexDir, false, remote, io.Discard); err != nil || result.Chunks != 1 {
		t.Fatalf("seed sync: %+v err=%v", result, err)
	}
	httpServer.Close()
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`UPDATE sessions SET prefix_hash_state=X'',metadata_version=0,repository_url='',git_branch='',git_commit_hash=''`); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if tamper {
		if _, err := raw.Exec(`UPDATE chunks SET ciphertext=?`, bytes.Repeat([]byte{0}, 32)); err != nil {
			raw.Close()
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	clientConfig.ServerURL = ""
	return cfg, clientConfig
}
