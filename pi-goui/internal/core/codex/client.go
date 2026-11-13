package codex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const usageURL = "https://chatgpt.com/backend-api/wham/usage"
const responseLimit = 1 << 20
const queryTimeout = 15 * time.Second

var (
	errClosed       = errors.New("Codex quota client closed")
	errConfig       = errors.New("Codex quota configuration unavailable")
	errCredentials  = errors.New("Codex OAuth credentials unavailable")
	errExpired      = errors.New("Codex OAuth credentials expired; pi must refresh them")
	errRequest      = errors.New("Codex quota request failed")
	errUnauthorized = errors.New("Codex quota authorization failed")
	errPayload      = errors.New("Codex quota response invalid")
	errTooLarge     = errors.New("Codex quota response too large")
)

// Client does not poll or refresh OAuth tokens. Fetch cancels an older Fetch;
// Close cancels outstanding requests and waits until their resources are closed.
// The agent directory is read only, and credentials never leave the fixed origin.
type Client struct {
	agentDir   string
	http       *http.Client
	now        func() time.Time
	timeout    time.Duration
	mu         sync.Mutex
	closed     bool
	generation uint64
	cancel     context.CancelFunc
	pending    sync.WaitGroup
}

func New(agentDir string) *Client {
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: queryTimeout,
		IdleConnTimeout: 90 * time.Second, MaxIdleConns: 2, MaxIdleConnsPerHost: 1, ForceAttemptHTTP2: true,
	}
	return &Client{agentDir: agentDir, now: time.Now, timeout: queryTimeout, http: &http.Client{
		Transport: transport, Timeout: queryTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}
func (c *Client) Cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
}
func (c *Client) Close() {
	c.mu.Lock()
	c.closed = true
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()
	c.pending.Wait()
	c.http.CloseIdleConnections()
}

func officialOrigin(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Opaque == "" && u.User == nil && strings.EqualFold(u.Scheme, "https") && strings.EqualFold(u.Hostname(), "chatgpt.com") && (u.Port() == "" || u.Port() == "443")
}

// readObject intentionally discards filesystem/JSON errors that can expose paths
// or secret input. Missing optional files are represented by an empty object.
func readObject(path string) (map[string]json.RawMessage, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > responseLimit {
		return nil, errConfig
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errConfig
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, responseLimit+1))
	if err != nil || len(data) > responseLimit {
		return nil, errConfig
	}
	var value map[string]json.RawMessage
	if json.Unmarshal(data, &value) != nil || value == nil {
		return nil, errConfig
	}
	return value, nil
}
func enabled(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var display map[string]json.RawMessage
	if json.Unmarshal(raw, &display) != nil || display == nil {
		return false
	}
	show, present := display["show"]
	return !present || trueValue(show)
}
func (c *Client) configured(id Identity) (bool, error) {
	if id.Provider != "openai-codex" || !officialOrigin(id.BaseURL) {
		return false, nil
	}
	if strings.TrimSpace(c.agentDir) == "" {
		return false, errConfig
	}
	models, err := readObject(filepath.Join(c.agentDir, "models.json"))
	if err != nil {
		return false, errConfig
	}
	var providers map[string]struct {
		BaseURL string `json:"baseUrl"`
	}
	if raw := models["providers"]; len(raw) > 0 && json.Unmarshal(raw, &providers) != nil {
		return false, errConfig
	}
	if base := providers["openai-codex"].BaseURL; base != "" && !officialOrigin(base) {
		return false, nil
	}
	settings, err := readObject(filepath.Join(c.agentDir, "settings.json"))
	if err != nil {
		return false, errConfig
	}
	return enabled(settings["usageDisplay"]), nil
}

type oauthRecord struct {
	Type      string          `json:"type"`
	Access    string          `json:"access"`
	AccountID string          `json:"accountId"`
	Expires   json.RawMessage `json:"expires"`
}

func (c *Client) Fetch(identity Identity) (snapshot Snapshot, err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return snapshot, errClosed
	}
	if c.cancel != nil {
		c.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	c.generation++
	generation := c.generation
	c.cancel = cancel
	c.pending.Add(1)
	c.mu.Unlock()
	defer func() {
		cancel()
		c.mu.Lock()
		if c.generation == generation {
			c.cancel = nil
		}
		c.mu.Unlock()
		c.pending.Done()
	}()
	eligible, e := c.configured(identity)
	if e != nil || !eligible {
		return snapshot, e
	}
	auth, e := readObject(filepath.Join(c.agentDir, "auth.json"))
	if e != nil {
		return snapshot, errCredentials
	}
	raw := auth["openai-codex"]
	if len(raw) == 0 {
		return snapshot, nil
	}
	var record oauthRecord
	if json.Unmarshal(raw, &record) != nil {
		return snapshot, errCredentials
	}
	if record.Type != "oauth" || strings.TrimSpace(record.Access) == "" {
		return snapshot, nil
	}
	snapshot.LoggedIn = true
	if strings.ContainsAny(record.Access, "\r\n") || strings.ContainsAny(record.AccountID, "\r\n") {
		return snapshot, errCredentials
	}
	now := c.now()
	if len(record.Expires) > 0 && string(record.Expires) != "null" {
		expiry, valid := number(record.Expires)
		if !valid {
			return snapshot, errCredentials
		}
		if expiry <= float64(now.UnixMilli()) {
			return snapshot, errExpired
		}
	}
	if ctx.Err() != nil {
		return snapshot, ctx.Err()
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if e != nil {
		return snapshot, errRequest
	}
	req.Header.Set("Authorization", "Bearer "+record.Access)
	req.Header.Set("Accept", "application/json")
	if record.AccountID != "" {
		req.Header.Set("ChatGPT-Account-Id", record.AccountID)
	}
	response, e := c.http.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return snapshot, ctx.Err()
		}
		return snapshot, errRequest
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return snapshot, errUnauthorized
	}
	if response.StatusCode != http.StatusOK {
		return snapshot, errRequest
	}
	data, e := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if ctx.Err() != nil {
		return snapshot, ctx.Err()
	}
	if e != nil {
		return snapshot, errRequest
	}
	if len(data) > responseLimit {
		return snapshot, errTooLarge
	}
	decoded, e := decode(data, now)
	if e != nil {
		return snapshot, e
	}
	decoded.LoggedIn = true
	return decoded, nil
}
