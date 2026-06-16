package smclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colindickson/unifi/internal/client"
	"github.com/colindickson/unifi/internal/config"
)

// newTestClient points a Client at the test server (http, base override).
func newTestClient(serverURL string) *Client {
	c := New(config.Config{APIKey: "test-key"})
	c.baseURL = strings.TrimRight(serverURL, "/")
	return c
}

// cursorResponse encodes a {data, nextToken} envelope.
func cursorResponse(nextToken string, items []any) string {
	data, _ := json.Marshal(items)
	env := map[string]any{"data": json.RawMessage(data)}
	if nextToken != "" {
		env["nextToken"] = nextToken
	}
	b, _ := json.Marshal(env)
	return string(b)
}

func TestNew_DefaultBaseURL(t *testing.T) {
	c := New(config.Config{APIKey: "k"})
	if c.baseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, DefaultBaseURL)
	}
}

func TestNew_BaseURLEnvOverride(t *testing.T) {
	t.Setenv("UNIFI_SITE_MANAGER_URL", "https://example.test/")
	c := New(config.Config{APIKey: "k"})
	if c.baseURL != "https://example.test" {
		t.Errorf("baseURL = %q, want trailing slash trimmed override", c.baseURL)
	}
}

func TestDo_SendsAPIKeyAndAccept(t *testing.T) {
	var gotKey, gotAccept, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-KEY")
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.Path
		fmt.Fprint(w, `{"data":{"ok":true}}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	raw, err := c.Do(context.Background(), http.MethodGet, "/v1/hosts", nil, nil)
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	if gotKey != "test-key" {
		t.Errorf("X-API-KEY = %q", gotKey)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotPath != "/v1/hosts" {
		t.Errorf("path = %q", gotPath)
	}
	// Do is the raw passthrough: it must NOT unwrap data.
	if !strings.Contains(string(raw), "data") {
		t.Errorf("Do should return the raw body incl. envelope, got %s", raw)
	}
}

func TestListPath_FollowsNextTokenWhenAll(t *testing.T) {
	var gotTokens []string
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTokens = append(gotTokens, r.URL.Query().Get("nextToken"))
		call++
		switch call {
		case 1:
			fmt.Fprint(w, cursorResponse("tok2", []any{map[string]string{"id": "a"}, map[string]string{"id": "b"}}))
		case 2:
			fmt.Fprint(w, cursorResponse("", []any{map[string]string{"id": "c"}}))
		default:
			t.Errorf("unexpected extra request (call %d)", call)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	items, err := c.ListPath(context.Background(), "/v1/hosts", true, 0)
	if err != nil {
		t.Fatalf("ListPath error: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("got %d items, want 3", len(items))
	}
	if call != 2 {
		t.Errorf("expected 2 page calls, got %d", call)
	}
	if gotTokens[0] != "" || gotTokens[1] != "tok2" {
		t.Errorf("nextToken sequence = %v, want ['', 'tok2']", gotTokens)
	}
}

func TestListPath_SinglePageWhenNotAll(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		// Always advertise a next token; not-all must still stop after one page.
		fmt.Fprint(w, cursorResponse("more", []any{map[string]string{"id": "a"}}))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	items, err := c.ListPath(context.Background(), "/v1/hosts", false, 25)
	if err != nil {
		t.Fatalf("ListPath error: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("got %d items, want 1", len(items))
	}
	if call != 1 {
		t.Errorf("not-all must fetch exactly one page, got %d calls", call)
	}
}

func TestListPath_SendsPageSize(t *testing.T) {
	var gotPageSize string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPageSize = r.URL.Query().Get("pageSize")
		fmt.Fprint(w, cursorResponse("", []any{}))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if _, err := c.ListPath(context.Background(), "/v1/hosts", false, 50); err != nil {
		t.Fatalf("ListPath error: %v", err)
	}
	if gotPageSize != "50" {
		t.Errorf("pageSize = %q, want 50", gotPageSize)
	}
}

func TestListPath_StopsOnEmptyPage(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		switch call {
		case 1:
			fmt.Fprint(w, cursorResponse("tok", []any{map[string]string{"id": "a"}}))
		case 2:
			// Empty data but still a token — guard against an infinite loop.
			fmt.Fprint(w, cursorResponse("tok", []any{}))
		default:
			t.Errorf("unexpected extra request (call %d)", call)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	items, err := c.ListPath(context.Background(), "/v1/hosts", true, 0)
	if err != nil {
		t.Fatalf("ListPath error: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("got %d items, want 1", len(items))
	}
	if call != 2 {
		t.Errorf("expected 2 calls then stop, got %d", call)
	}
}

func TestDo_429_ReturnsClientAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"code":"RATE_LIMIT","message":"slow down"}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Do(context.Background(), http.MethodGet, "/ea/sd-wan/configs", nil, nil)
	apiErr, ok := err.(*client.APIError)
	if !ok {
		t.Fatalf("error type = %T, want *client.APIError", err)
	}
	if apiErr.StatusCode != 429 || apiErr.RetryAfter != 7 {
		t.Errorf("apiErr = %+v, want 429/retry 7", apiErr)
	}
}

func TestDo_ErrorFallbackToRawBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		fmt.Fprint(w, "service unavailable")
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Do(context.Background(), http.MethodGet, "/v1/hosts", nil, nil)
	apiErr, ok := err.(*client.APIError)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if apiErr.StatusCode != 503 || !strings.Contains(apiErr.Message, "service unavailable") {
		t.Errorf("apiErr = %+v", apiErr)
	}
}
