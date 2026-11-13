package codex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

var official = Identity{Provider: "openai-codex", BaseURL: "https://chatgpt.com/backend-api/codex"}

const goodPayload = `{"rate_limit":{"primary_window":{"limit_window_seconds":18000,"used_percent":99,"reset_at":1800000000},"secondary_window":{"limit_window_seconds":604800,"used_percent":19.8,"reset_at":1800000000}},"credits":{"has_credits":true,"unlimited":false,"balance":"82334"}}`

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func put(t *testing.T, root, name string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T) *Client {
	t.Helper()
	c := New(t.TempDir())
	c.now = func() time.Time { return time.Unix(1700000000, 0) }
	put(t, c.agentDir, "auth.json", map[string]any{"openai-codex": map[string]any{"type": "oauth", "access": "FAKE-TEST-ACCESS", "refresh": "FAKE-UNUSED-REFRESH", "expires": 1800000000000, "accountId": "FAKE-ACCOUNT"}})
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected request")
		return nil, errors.New("blocked test network")
	})
	t.Cleanup(c.Close)
	return c
}
func TestFetchWeeklyQuotaAndCredits(t *testing.T) {
	c := fixture(t)
	requests := 0
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.String() != usageURL || r.Method != "GET" || r.Header.Get("Authorization") != "Bearer FAKE-TEST-ACCESS" || r.Header.Get("ChatGPT-Account-Id") != "FAKE-ACCOUNT" {
			t.Error("wrong request target, method or authentication")
		}
		return response(200, goodPayload), nil
	})
	snapshot, err := c.Fetch(official)
	if err != nil || !snapshot.LoggedIn || snapshot.RemainingPercent == nil || *snapshot.RemainingPercent != 80.2 || snapshot.Credits == nil || *snapshot.Credits != 82334 || snapshot.ResetAt.Unix() != 1800000000 || snapshot.FetchedAt != c.now() || requests != 1 {
		t.Fatalf("invalid quota snapshot: %+v, %v, calls=%d", snapshot, err, requests)
	}
}
func TestEligibilityNeverContactsOtherOriginsOrAPIKeys(t *testing.T) {
	for _, id := range []Identity{{"other", official.BaseURL}, {"openai-codex", ""}, {"openai-codex", "http://chatgpt.com"}, {"openai-codex", "https://chatgpt.com.evil.invalid"}, {"openai-codex", "https://chatgpt.com:8443"}, {"openai-codex", "https://user@chatgpt.com"}, {"openai-codex", "not a url"}} {
		c := fixture(t)
		s, err := c.Fetch(id)
		if err != nil || s.LoggedIn {
			t.Fatalf("ineligible identity passed: %+v %v", s, err)
		}
	}
	for _, record := range []any{nil, map[string]any{"type": "api_key", "key": "FAKE-KEY"}, map[string]any{"type": "oauth", "access": ""}} {
		c := fixture(t)
		put(t, c.agentDir, "auth.json", map[string]any{"openai-codex": record})
		s, err := c.Fetch(official)
		if err != nil || s.LoggedIn {
			t.Fatalf("non-OAuth record passed: %+v %v", s, err)
		}
	}
	c := fixture(t)
	put(t, c.agentDir, "models.json", map[string]any{"providers": map[string]any{"openai-codex": map[string]any{"baseUrl": "https://custom.invalid"}}})
	if s, err := c.Fetch(official); err != nil || s.LoggedIn {
		t.Fatal("provider override was not blocked")
	}
	c = fixture(t)
	put(t, c.agentDir, "settings.json", map[string]any{"usageDisplay": map[string]any{"show": false}})
	if s, err := c.Fetch(official); err != nil || s.LoggedIn {
		t.Fatal("explicit display opt-out was ignored")
	}
	c = fixture(t)
	if err := os.Remove(filepath.Join(c.agentDir, "auth.json")); err != nil {
		t.Fatal(err)
	}
	if s, err := c.Fetch(official); err != nil || s.LoggedIn {
		t.Fatal("missing OAuth record was accepted")
	}
}
func TestExpiredAndInvalidCredentialsAreSanitized(t *testing.T) {
	for _, record := range []map[string]any{{"type": "oauth", "access": "FAKE-SECRET", "expires": 1}, {"type": "oauth", "access": "FAKE-SECRET\r\nHost: other.invalid"}, {"type": "oauth", "access": "FAKE-SECRET", "accountId": "bad\naccount"}} {
		c := fixture(t)
		put(t, c.agentDir, "auth.json", map[string]any{"openai-codex": record})
		s, err := c.Fetch(official)
		if err == nil || !s.LoggedIn || strings.Contains(err.Error(), "FAKE") || strings.Contains(err.Error(), c.agentDir) {
			t.Fatal("invalid credential handling is not safe")
		}
	}
	c := fixture(t)
	if err := os.WriteFile(filepath.Join(c.agentDir, "auth.json"), []byte("FAKE-SECRET malformed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Fetch(official); err == nil || strings.Contains(err.Error(), "FAKE") || strings.Contains(err.Error(), c.agentDir) {
		t.Fatal("credential parse error leaked details")
	}
}
func TestResponseErrorsDoNotRetainPreviousAccountData(t *testing.T) {
	c := fixture(t)
	status, body := 200, goodPayload
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return response(status, body), nil })
	if s, err := c.Fetch(official); err != nil || s.Credits == nil {
		t.Fatal("setup quota failed")
	}
	for _, tc := range []struct {
		status int
		body   string
	}{{401, "FAKE-SECRET-RESPONSE"}, {403, "FAKE-SECRET-RESPONSE"}, {503, "FAKE-SECRET-RESPONSE"}, {200, "invalid FAKE-SECRET-RESPONSE"}, {200, "null"}, {200, "[]"}} {
		status, body = tc.status, tc.body
		s, err := c.Fetch(official)
		if err == nil || !s.LoggedIn || s.Credits != nil || s.RemainingPercent != nil || !s.FetchedAt.IsZero() {
			t.Fatalf("stale or invalid quota: %+v %v", s, err)
		}
		if strings.Contains(err.Error(), "FAKE") || strings.Contains(err.Error(), c.agentDir) {
			t.Fatal("response error leaked private details")
		}
	}
}
func TestRedirectsAreNotFollowedAndTransportHasNoProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://unused.invalid:1234")
	plain := New(t.TempDir())
	defer plain.Close()
	transport := plain.http.Transport.(*http.Transport)
	if transport.Proxy != nil || (transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify) {
		t.Fatal("unsafe production HTTP transport")
	}
	c := fixture(t)
	count := 0
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		count++
		out := response(302, "")
		out.Header.Set("Location", "https://other.invalid/collect")
		return out, nil
	})
	if s, err := c.Fetch(official); err == nil || !s.LoggedIn || count != 1 {
		t.Fatalf("redirect followed or silently accepted: %d %v", count, err)
	}
}

type countedBody struct {
	r      io.Reader
	read   int
	closed bool
}

func (b *countedBody) Read(p []byte) (int, error) { n, err := b.r.Read(p); b.read += n; return n, err }
func (b *countedBody) Close() error               { b.closed = true; return nil }
func TestResponseIsBoundedAndClosed(t *testing.T) {
	c := fixture(t)
	body := &countedBody{r: strings.NewReader(strings.Repeat("x", 2*responseLimit))}
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { r := response(200, ""); r.Body = body; return r, nil })
	_, err := c.Fetch(official)
	if !errors.Is(err, errTooLarge) || body.read != responseLimit+1 || !body.closed {
		t.Fatalf("response limit/cleanup failed: read=%d closed=%v err=%v", body.read, body.closed, err)
	}
}
func TestCancelTimeoutCloseAndNoLateSpawn(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		c := fixture(t)
		entered := make(chan struct{})
		c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			close(entered)
			<-r.Context().Done()
			return nil, r.Context().Err()
		})
		done := make(chan error, 1)
		go func() { _, err := c.Fetch(official); done <- err }()
		<-entered
		c.Cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("Cancel did not end Fetch")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		c := fixture(t)
		c.timeout = 20 * time.Millisecond
		c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
		if _, err := c.Fetch(official); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	})
	t.Run("close waits", func(t *testing.T) {
		c := fixture(t)
		entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var count atomic.Int32
		c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			count.Add(1)
			close(entered)
			<-r.Context().Done()
			close(cancelled)
			<-release
			return nil, r.Context().Err()
		})
		fetched := make(chan struct{})
		go func() { defer close(fetched); _, _ = c.Fetch(official) }()
		<-entered
		closed := make(chan struct{})
		go func() { c.Close(); close(closed) }()
		<-cancelled
		select {
		case <-closed:
			t.Fatal("Close returned before Fetch cleanup")
		default:
		}
		close(release)
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Fatal("Close did not finish")
		}
		<-fetched
		if _, err := c.Fetch(official); !errors.Is(err, errClosed) || count.Load() != 1 {
			t.Fatal("late request started after Close")
		}
		c.Close()
	})
}
func TestConcurrentFetchCancelsOlderAndCloses(t *testing.T) {
	c := fixture(t)
	var count atomic.Int32
	firstEntered := make(chan struct{})
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if count.Add(1) == 1 {
			close(firstEntered)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return response(200, goodPayload), nil
	})
	first := make(chan error, 1)
	go func() { _, err := c.Fetch(official); first <- err }()
	<-firstEntered
	if _, err := c.Fetch(official); err != nil {
		t.Fatal(err)
	}
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("previous identity request survived: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Fetch(official)
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, errClosed) {
				t.Error(err)
			}
		}()
	}
	c.Close()
	wg.Wait()
}
