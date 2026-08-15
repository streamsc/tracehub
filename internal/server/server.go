package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"filippo.io/age"

	"tracehub/internal/api"
	"tracehub/internal/archive"
	"tracehub/internal/auth"
	"tracehub/internal/codex"
	"tracehub/internal/config"
	"tracehub/internal/keys"
	"tracehub/internal/store"
)

type Server struct {
	config     config.Server
	store      *store.Store
	identities map[string]age.Identity
	devices    map[string]ed25519.PublicKey
}

type deviceContextKey struct{}

func New(cfg config.Server) (*Server, error) {
	db, err := store.Open(cfg.Database)
	if err != nil {
		return nil, err
	}
	s := &Server{config: cfg, store: db, identities: make(map[string]age.Identity), devices: make(map[string]ed25519.PublicKey)}
	for id, path := range cfg.ServerPrivateKeys {
		identity, err := keys.LoadServerIdentity(path)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("load server key %s: %w", id, err)
		}
		s.identities[id] = identity
	}
	for id, device := range cfg.Devices {
		if !device.Enabled {
			continue
		}
		publicKey, err := keys.LoadDevicePublic(device.PublicKey)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("load device %s: %w", id, err)
		}
		s.devices[id] = publicKey
	}
	if len(s.devices) == 0 {
		db.Close()
		return nil, errors.New("server has no enabled devices")
	}
	if err := s.backfillSessions(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("backfill stored sessions: %w", err)
	}
	return s, nil
}

func (s *Server) backfillSessions(ctx context.Context) error {
	pending, err := s.store.PendingBackfills(ctx)
	if err != nil {
		return err
	}
	for _, session := range pending {
		chunks, err := s.store.Chunks(ctx, session.DeviceID, session.AgentType, session.SessionID)
		if err != nil {
			return fmt.Errorf("load %s/%s: %w", session.DeviceID, session.SessionID, err)
		}
		hasher := sha256.New()
		var expected int64
		var metadata *codex.Metadata
		for _, chunk := range chunks {
			if chunk.StartOffset != expected {
				return fmt.Errorf("session %s has archive offset gap: expected %d, got %d", session.SessionID, expected, chunk.StartOffset)
			}
			identity, ok := s.identities[chunk.ServerKeyID]
			if !ok {
				return fmt.Errorf("session %s requires missing server key %s", session.SessionID, chunk.ServerKeyID)
			}
			plain, err := archive.Decrypt(chunk.Ciphertext, identity, codex.MaxLine+codex.TargetChunk)
			if err != nil {
				return fmt.Errorf("decrypt session %s at %d: %w", session.SessionID, chunk.StartOffset, err)
			}
			sum := sha256.Sum256(plain)
			if int64(len(plain)) != chunk.PlainSize || hex.EncodeToString(sum[:]) != chunk.PlainSHA256 {
				return fmt.Errorf("session %s failed archive integrity at %d", session.SessionID, chunk.StartOffset)
			}
			parsed, err := codex.ParseChunk(plain, chunk.StartOffset, session.SessionID)
			if err != nil {
				return fmt.Errorf("parse session %s at %d: %w", session.SessionID, chunk.StartOffset, err)
			}
			if parsed.Metadata != nil {
				metadata = parsed.Metadata
			}
			_, _ = hasher.Write(plain)
			expected = chunk.EndOffset
		}
		if expected != session.NextOffset || metadata == nil {
			return fmt.Errorf("session %s archive does not match stored offset or metadata", session.SessionID)
		}
		state, err := hasher.(encoding.BinaryMarshaler).MarshalBinary()
		if err != nil {
			return err
		}
		if err := s.store.FinishBackfill(ctx, session, state, *metadata); err != nil {
			return fmt.Errorf("finish session %s backfill: %w", session.SessionID, err)
		}
	}
	return nil
}

func (s *Server) Close() error { return s.store.Close() }

func (s *Server) Handler() http.Handler {
	data := http.NewServeMux()
	data.HandleFunc("POST /v1/sync/plan", s.handleSyncPlan)
	data.HandleFunc("PUT /v1/sync/chunks/{agent}/{session}/{start}", s.handleChunk)
	data.HandleFunc("GET /v1/devices", s.handleDevices)
	data.HandleFunc("POST /v1/sessions/search", s.handleSearch)
	data.HandleFunc("GET /v1/sessions/{device}/{agent}/{session}", s.handleSession)
	data.HandleFunc("GET /v1/sessions/{device}/{agent}/{session}/events", s.handleEvents)
	data.HandleFunc("GET /v1/sessions/{device}/{agent}/{session}/tool-output/{seq}", s.handleToolOutput)
	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	root.Handle("/", s.authenticate(data))
	return root
}

func (s *Server) ListenAndServe() error {
	httpServer := &http.Server{Addr: s.config.Listen, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	return httpServer.ListenAndServe()
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deviceID := r.Header.Get(auth.HeaderDevice)
		device, configured := s.config.Devices[deviceID]
		publicKey, loaded := s.devices[deviceID]
		if !configured || !loaded || !device.Enabled || device.KeyID != r.Header.Get(auth.HeaderKeyID) {
			writeError(w, http.StatusUnauthorized, "unknown or disabled device")
			return
		}
		if err := auth.VerifyRequest(r, publicKey); err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), deviceContextKey{}, deviceID)))
	})
}

func (s *Server) handleSyncPlan(w http.ResponseWriter, r *http.Request) {
	var request api.SyncPlanRequest
	if err := decodeJSON(r, &request, 2<<20); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.AgentType != codex.AgentType || len(request.Sessions) > api.MaxManifestSessions {
		writeError(w, http.StatusBadRequest, "unsupported agent type or oversized manifest")
		return
	}
	ids := make([]string, 0, len(request.Sessions))
	sizes := make(map[string]int64, len(request.Sessions))
	for _, session := range request.Sessions {
		if !codex.SafeSessionID(session.SessionID) || session.Size < 0 {
			writeError(w, http.StatusBadRequest, "invalid session manifest")
			return
		}
		if _, exists := sizes[session.SessionID]; exists {
			writeError(w, http.StatusBadRequest, "duplicate session ID in manifest")
			return
		}
		ids = append(ids, session.SessionID)
		sizes[session.SessionID] = session.Size
	}
	checkpoints, err := s.store.Checkpoints(r.Context(), deviceID(r), request.AgentType, ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response := make(map[string]api.SyncCheckpoint, len(checkpoints))
	for id, checkpoint := range checkpoints {
		if checkpoint.NextOffset > sizes[id] {
			writeError(w, http.StatusConflict, fmt.Sprintf("session %s was truncated below server offset %d", id, checkpoint.NextOffset))
			return
		}
		response[id] = api.SyncCheckpoint{NextOffset: checkpoint.NextOffset, PrefixSHA256: checkpoint.PrefixSHA256}
	}
	writeJSON(w, http.StatusOK, api.SyncPlanResponse{Sessions: response})
}

func (s *Server) handleChunk(w http.ResponseWriter, r *http.Request) {
	agentType := r.PathValue("agent")
	sessionID := r.PathValue("session")
	start, err := strconv.ParseInt(r.PathValue("start"), 10, 64)
	if err != nil || start < 0 || agentType != codex.AgentType || !codex.SafeSessionID(sessionID) {
		writeError(w, http.StatusBadRequest, "invalid chunk path")
		return
	}
	end, err := parseIntHeader(r, api.HeaderEndOffset)
	if err != nil || end <= start {
		writeError(w, http.StatusBadRequest, "invalid end offset")
		return
	}
	plainSize, err := parseIntHeader(r, api.HeaderPlainSize)
	if err != nil || plainSize != end-start || plainSize > codex.MaxLine+codex.TargetChunk {
		writeError(w, http.StatusBadRequest, "invalid plaintext size")
		return
	}
	plainHash := r.Header.Get(api.HeaderPlainSHA256)
	if len(plainHash) != sha256.Size*2 {
		writeError(w, http.StatusBadRequest, "invalid plaintext SHA-256")
		return
	}
	prefixHash := r.Header.Get(api.HeaderPrefixSHA256)
	if len(prefixHash) != sha256.Size*2 {
		writeError(w, http.StatusBadRequest, "invalid prefix SHA-256")
		return
	}
	keyID := r.Header.Get(api.HeaderServerKeyID)
	identity, ok := s.identities[keyID]
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown server key ID")
		return
	}
	ciphertext, err := io.ReadAll(io.LimitReader(r.Body, auth.MaxRequestBody+1))
	if err != nil || len(ciphertext) > auth.MaxRequestBody {
		writeError(w, http.StatusBadRequest, "invalid ciphertext body")
		return
	}
	plain, err := archive.Decrypt(ciphertext, identity, codex.MaxLine+codex.TargetChunk)
	if err != nil {
		writeError(w, http.StatusBadRequest, "decrypt chunk: "+err.Error())
		return
	}
	if int64(len(plain)) != plainSize {
		writeError(w, http.StatusBadRequest, "plaintext length mismatch")
		return
	}
	hash := sha256.Sum256(plain)
	if hex.EncodeToString(hash[:]) != strings.ToLower(plainHash) {
		writeError(w, http.StatusBadRequest, "plaintext SHA-256 mismatch")
		return
	}
	parsed, err := codex.ParseChunk(plain, start, sessionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	duplicate, err := s.store.PutChunk(r.Context(), store.Chunk{DeviceID: deviceID(r), AgentType: agentType, SessionID: sessionID, StartOffset: start, EndOffset: end, ServerKeyID: keyID, PlainSHA256: strings.ToLower(plainHash), PlainSize: plainSize, Ciphertext: ciphertext}, plain, strings.ToLower(prefixHash), parsed)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, api.UploadResponse{NextOffset: end, Duplicate: duplicate})
}

func (s *Server) handleDevices(w http.ResponseWriter, _ *http.Request) {
	devices := make([]string, 0, len(s.devices))
	for id := range s.devices {
		devices = append(devices, id)
	}
	sort.Strings(devices)
	writeJSON(w, http.StatusOK, api.DevicesResponse{Devices: devices})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	var filter store.SearchFilter
	if err := decodeJSON(r, &filter, 1<<20); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sessions, err := s.store.SearchSessions(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var next int64
	if len(sessions) > 0 {
		next = sessions[len(sessions)-1].ID
	}
	writeJSON(w, http.StatusOK, api.SearchResponse{Sessions: sessions, NextID: next})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	session, err := s.store.Session(r.Context(), r.PathValue("device"), r.PathValue("agent"), r.PathValue("session"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	after, err := parseOptionalInt64(r.URL.Query().Get("after_seq"))
	if err != nil || after < 0 {
		writeError(w, http.StatusBadRequest, "invalid after_seq")
		return
	}
	textOffset64, err := parseOptionalInt64(r.URL.Query().Get("after_text_offset"))
	if err != nil || textOffset64 < 0 || int64(int(textOffset64)) != textOffset64 {
		writeError(w, http.StatusBadRequest, "invalid after_text_offset")
		return
	}
	textOffset := int(textOffset64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, next, nextTextOffset, err := s.store.Events(r.Context(), r.PathValue("device"), r.PathValue("agent"), r.PathValue("session"), after, textOffset, limit, api.MaxPageBytes)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.EventsResponse{UntrustedHistoricalData: true, Events: events, NextSeq: next, NextTextOffset: nextTextOffset})
}

func parseOptionalInt64(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func (s *Server) handleToolOutput(w http.ResponseWriter, r *http.Request) {
	seq, err := strconv.ParseInt(r.PathValue("seq"), 10, 64)
	if err != nil || seq < 0 {
		writeError(w, http.StatusBadRequest, "invalid event sequence")
		return
	}
	chunk, err := s.store.EventChunk(r.Context(), r.PathValue("device"), r.PathValue("agent"), r.PathValue("session"), seq)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	identity, ok := s.identities[chunk.ServerKeyID]
	if !ok {
		writeError(w, http.StatusInternalServerError, "missing server decryption key")
		return
	}
	plain, err := archive.Decrypt(chunk.Ciphertext, identity, codex.MaxLine+codex.TargetChunk)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	relative := seq - chunk.StartOffset
	if relative < 0 || relative >= int64(len(plain)) {
		writeError(w, http.StatusInternalServerError, "invalid event locator")
		return
	}
	remaining := plain[relative:]
	lineEnd := bytes.IndexByte(remaining, '\n')
	if lineEnd < 0 {
		writeError(w, http.StatusInternalServerError, "incomplete archived event")
		return
	}
	output, err := codex.ToolOutput(remaining[:lineEnd+1])
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	output, truncated := truncateUTF8(output, api.MaxPageBytes)
	writeJSON(w, http.StatusOK, api.ToolOutputResponse{UntrustedHistoricalData: true, Output: output, Truncated: truncated})
}

func parseIntHeader(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.Header.Get(name), 10, 64)
}

func decodeJSON(r *http.Request, dst any, max int64) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, max+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > max {
		return fmt.Errorf("JSON body exceeds %d bytes", max)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request contains multiple JSON values")
		}
		return err
	}
	return nil
}

func deviceID(r *http.Request) string { return r.Context().Value(deviceContextKey{}).(string) }

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
	} else {
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, api.ErrorResponse{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func truncateUTF8(value string, max int) (string, bool) {
	if len(value) <= max {
		return value, false
	}
	value = value[:max]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value, true
}
