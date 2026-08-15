package api

import (
	"tracehub/internal/store"
)

const (
	HeaderEndOffset     = "X-TraceHub-End-Offset"
	HeaderPlainSHA256   = "X-TraceHub-Plain-Sha256"
	HeaderPlainSize     = "X-TraceHub-Plain-Size"
	HeaderServerKeyID   = "X-TraceHub-Server-Key-Id"
	HeaderPrefixSHA256  = "X-TraceHub-Prefix-Sha256"
	MaxPageBytes        = 256 << 10
	MaxManifestSessions = 10000
)

type LocalSession struct {
	SessionID string `json:"session_id"`
	Size      int64  `json:"size"`
}

type SyncPlanRequest struct {
	AgentType string         `json:"agent_type"`
	Sessions  []LocalSession `json:"sessions"`
}

type SyncPlanResponse struct {
	Sessions map[string]SyncCheckpoint `json:"sessions"`
}

type SyncCheckpoint struct {
	NextOffset   int64  `json:"next_offset"`
	PrefixSHA256 string `json:"prefix_sha256"`
}

type DevicesResponse struct {
	Devices []string `json:"devices"`
}

type SearchResponse struct {
	Sessions []store.Session `json:"sessions"`
	NextID   int64           `json:"next_id"`
}

type EventsResponse struct {
	UntrustedHistoricalData bool          `json:"untrusted_historical_data"`
	Events                  []store.Event `json:"events"`
	NextSeq                 int64         `json:"next_seq"`
	NextTextOffset          int           `json:"next_text_offset,omitempty"`
}

type ToolOutputResponse struct {
	UntrustedHistoricalData bool   `json:"untrusted_historical_data"`
	Output                  string `json:"output"`
	Truncated               bool   `json:"truncated"`
}

type UploadResponse struct {
	NextOffset int64 `json:"next_offset"`
	Duplicate  bool  `json:"duplicate"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
