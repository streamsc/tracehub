package client

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"filippo.io/age"

	"tracehub/internal/api"
	"tracehub/internal/auth"
	"tracehub/internal/codex"
	"tracehub/internal/config"
	"tracehub/internal/keys"
	"tracehub/internal/store"
)

type Client struct {
	config    config.Client
	private   ed25519.PrivateKey
	recipient age.Recipient
	http      *http.Client
}

func New(cfg config.Client) (*Client, error) {
	base, err := url.Parse(cfg.ServerURL)
	if err != nil || base.Host == "" {
		return nil, errors.New("invalid server URL")
	}
	if base.Scheme != "https" && !(base.Scheme == "http" && isLoopback(base.Hostname())) {
		return nil, errors.New("server URL must use HTTPS unless it is loopback")
	}
	privateKey, err := keys.LoadDevicePrivate(cfg.DevicePrivateKey)
	if err != nil {
		return nil, err
	}
	recipient, err := keys.LoadServerRecipient(cfg.ServerPublicKey)
	if err != nil {
		return nil, err
	}
	return &Client{config: cfg, private: privateKey, recipient: recipient, http: &http.Client{Timeout: 5 * time.Minute}}, nil
}

func (c *Client) Recipient() age.Recipient { return c.recipient }

func (c *Client) Plan(ctx context.Context, sessions []api.LocalSession) (map[string]int64, error) {
	var response api.SyncPlanResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/sync/plan", api.SyncPlanRequest{AgentType: codex.AgentType, Sessions: sessions}, &response)
	return response.Offsets, err
}

func (c *Client) Upload(ctx context.Context, sessionID string, start, end int64, plainHash string, plainSize int64, ciphertext []byte) (api.UploadResponse, error) {
	path := fmt.Sprintf("/v1/sync/chunks/%s/%s/%d", codex.AgentType, url.PathEscape(sessionID), start)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.config.ServerURL+path, bytes.NewReader(ciphertext))
	if err != nil {
		return api.UploadResponse{}, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set(api.HeaderEndOffset, strconv.FormatInt(end, 10))
	req.Header.Set(api.HeaderPlainSHA256, plainHash)
	req.Header.Set(api.HeaderPlainSize, strconv.FormatInt(plainSize, 10))
	req.Header.Set(api.HeaderServerKeyID, c.config.ServerKeyID)
	if err := auth.SignRequest(req, c.config.DeviceID, c.config.DeviceKeyID, c.private); err != nil {
		return api.UploadResponse{}, err
	}
	var response api.UploadResponse
	err = c.execute(req, &response)
	return response, err
}

func (c *Client) Devices(ctx context.Context) (api.DevicesResponse, error) {
	var response api.DevicesResponse
	err := c.doJSON(ctx, http.MethodGet, "/v1/devices", nil, &response)
	return response, err
}

func (c *Client) Search(ctx context.Context, filter store.SearchFilter) (api.SearchResponse, error) {
	var response api.SearchResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/sessions/search", filter, &response)
	return response, err
}

func (c *Client) Session(ctx context.Context, deviceID, agentType, sessionID string) (store.Session, error) {
	var response store.Session
	path := fmt.Sprintf("/v1/sessions/%s/%s/%s", url.PathEscape(deviceID), url.PathEscape(agentType), url.PathEscape(sessionID))
	err := c.doJSON(ctx, http.MethodGet, path, nil, &response)
	return response, err
}

func (c *Client) Events(ctx context.Context, deviceID, agentType, sessionID string, after int64, limit int) (api.EventsResponse, error) {
	var response api.EventsResponse
	path := fmt.Sprintf("/v1/sessions/%s/%s/%s/events?after_seq=%d&limit=%d", url.PathEscape(deviceID), url.PathEscape(agentType), url.PathEscape(sessionID), after, limit)
	err := c.doJSON(ctx, http.MethodGet, path, nil, &response)
	return response, err
}

func (c *Client) ToolOutput(ctx context.Context, deviceID, agentType, sessionID string, seq int64) (api.ToolOutputResponse, error) {
	var response api.ToolOutputResponse
	path := fmt.Sprintf("/v1/sessions/%s/%s/%s/tool-output/%d", url.PathEscape(deviceID), url.PathEscape(agentType), url.PathEscape(sessionID), seq)
	err := c.doJSON(ctx, http.MethodGet, path, nil, &response)
	return response, err
}

func (c *Client) doJSON(ctx context.Context, method, path string, input, output any) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.config.ServerURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := auth.SignRequest(req, c.config.DeviceID, c.config.DeviceKeyID, c.private); err != nil {
		return err
	}
	return c.execute(req, output)
}

func (c *Client) execute(req *http.Request, output any) error {
	response, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var apiError api.ErrorResponse
		if json.Unmarshal(body, &apiError) == nil && apiError.Error != "" {
			return fmt.Errorf("server returned %s: %s", response.Status, apiError.Error)
		}
		return fmt.Errorf("server returned %s", response.Status)
	}
	if output == nil {
		return nil
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("decode server response: %w", err)
	}
	return nil
}

func PlainSHA256(plain []byte) string {
	sum := sha256.Sum256(plain)
	return hex.EncodeToString(sum[:])
}

func isLoopback(host string) bool {
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}
