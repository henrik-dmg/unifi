package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/colindickson/unifi/internal/config"
)

// newTestClient creates a Client pointed at the given test server URL.
// It strips the scheme from the server URL to simulate a bare Host, then
// overrides baseURL directly (same package access).
func newTestClient(serverURL string) *Client {
	// Extract host (no scheme) so New() can build a URL, then override.
	host := strings.TrimPrefix(serverURL, "https://")
	host = strings.TrimPrefix(host, "http://")
	c := New(config.Config{Host: host, APIKey: "test-key"})
	// Override baseURL to point at the test server directly (http, not https).
	c.baseURL = serverURL
	return c
}

// pageResponse encodes a Page envelope as JSON bytes.
func pageResponse(offset, limit, count, totalCount int, items []any) []byte {
	data, _ := json.Marshal(items)
	env := map[string]any{
		"offset":     offset,
		"limit":      limit,
		"count":      count,
		"totalCount": totalCount,
		"data":       json.RawMessage(data),
	}
	b, _ := json.Marshal(env)
	return b
}

// ---- headers ----------------------------------------------------------------

func TestHeaders_APIKey_And_Accept(t *testing.T) {
	var gotAPIKey, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-API-KEY")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"info"}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Info(context.Background())
	if err != nil {
		t.Fatalf("Info error: %v", err)
	}
	if gotAPIKey != "test-key" {
		t.Errorf("X-API-KEY = %q, want %q", gotAPIKey, "test-key")
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want %q", gotAccept, "application/json")
	}
}

// ---- URL path ---------------------------------------------------------------

func TestURL_CorrectPath_WithSiteID(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprint(w, string(pageResponse(0, 25, 0, 0, nil)))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Devices(context.Background(), "site-abc", false, 0, 25)
	if err != nil {
		t.Fatalf("Devices error: %v", err)
	}
	// newTestClient overrides baseURL to the test server URL (scheme+host only),
	// so the path seen by the server is just the path component, not the full base.
	want := "/sites/site-abc/devices"
	if gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestURL_EscapesSiteIDWithSpaceAndSlash(t *testing.T) {
	var gotRawPath, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawPath = r.URL.EscapedPath()
		gotPath = r.URL.Path
		fmt.Fprint(w, string(pageResponse(0, 25, 0, 0, nil)))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Devices(context.Background(), "a b/c", false, 0, 25)
	if err != nil {
		t.Fatalf("Devices error: %v", err)
	}
	// The site id must be percent-escaped on the wire.
	wantEscaped := "/sites/a%20b%2Fc/devices"
	if gotRawPath != wantEscaped {
		t.Errorf("escaped path = %q, want %q", gotRawPath, wantEscaped)
	}
	// And decode back to the original segment.
	wantDecoded := "/sites/a b/c/devices"
	if gotPath != wantDecoded {
		t.Errorf("decoded path = %q, want %q", gotPath, wantDecoded)
	}
}

// ---- listRaw ----------------------------------------------------------------

func TestListRaw_ParsesEnvelope(t *testing.T) {
	items := []any{map[string]string{"id": "d1"}, map[string]string{"id": "d2"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, string(pageResponse(0, 25, 2, 2, items)))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	page, err := c.listRaw(context.Background(), "/sites/s1/devices", 0, 25)
	if err != nil {
		t.Fatalf("listRaw error: %v", err)
	}
	if page.Offset != 0 || page.Limit != 25 || page.Count != 2 || page.TotalCount != 2 {
		t.Errorf("envelope mismatch: %+v", page)
	}
	var got []map[string]string
	if err := json.Unmarshal(page.Data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 2 || got[0]["id"] != "d1" || got[1]["id"] != "d2" {
		t.Errorf("data = %+v", got)
	}
}

// ---- listAll ----------------------------------------------------------------

func TestListAll_AccumulatesAcross3Pages(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		_ = offset
		call++
		switch call {
		case 1:
			fmt.Fprint(w, string(pageResponse(0, 2, 2, 5, []any{"a", "b"})))
		case 2:
			fmt.Fprint(w, string(pageResponse(2, 2, 2, 5, []any{"c", "d"})))
		case 3:
			fmt.Fprint(w, string(pageResponse(4, 2, 1, 5, []any{"e"})))
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	items, err := c.listAll(context.Background(), "/things", 2)
	if err != nil {
		t.Fatalf("listAll error: %v", err)
	}
	if len(items) != 5 {
		t.Errorf("got %d items, want 5", len(items))
	}
	if call != 3 {
		t.Errorf("expected 3 page calls, got %d", call)
	}
}

func TestListAll_StopsOnEmptyPage(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		switch call {
		case 1:
			fmt.Fprint(w, string(pageResponse(0, 3, 3, 100, []any{"a", "b", "c"})))
		case 2:
			// Empty page — guard against infinite loop.
			fmt.Fprint(w, string(pageResponse(3, 3, 0, 100, nil)))
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	items, err := c.listAll(context.Background(), "/things", 3)
	if err != nil {
		t.Fatalf("listAll error: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("got %d items, want 3", len(items))
	}
	if call != 2 {
		t.Errorf("expected 2 calls, got %d", call)
	}
}

// ---- error handling ---------------------------------------------------------

func TestError_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"code":"NOT_FOUND","message":"resource not found"}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Info(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
	if apiErr.Code != "NOT_FOUND" {
		t.Errorf("Code = %q, want NOT_FOUND", apiErr.Code)
	}
	if apiErr.Message != "resource not found" {
		t.Errorf("Message = %q", apiErr.Message)
	}
}

func TestError_500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"statusName":"INTERNAL_ERROR","message":"something broke"}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Info(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
	}
	// statusName field
	if apiErr.Code != "INTERNAL_ERROR" {
		t.Errorf("Code = %q, want INTERNAL_ERROR", apiErr.Code)
	}
}

func TestError_429_RetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"code":"RATE_LIMIT","message":"slow down"}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Info(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if apiErr.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", apiErr.StatusCode)
	}
	if apiErr.RetryAfter != 42 {
		t.Errorf("RetryAfter = %d, want 42", apiErr.RetryAfter)
	}
	// Error string should mention retry-after
	s := apiErr.Error()
	if !strings.Contains(s, "42") {
		t.Errorf("Error() = %q, want mention of 42", s)
	}
}

// ---- POST with body ---------------------------------------------------------

func TestDeviceAction_POSTWithBodyAndContentType(t *testing.T) {
	var gotMethod, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.DeviceAction(context.Background(), "site1", "dev1", "RESTART")
	if err != nil {
		t.Fatalf("DeviceAction error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if !strings.Contains(gotContentType, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	var body map[string]string
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["action"] != "RESTART" {
		t.Errorf("body action = %q, want RESTART", body["action"])
	}
}

// ---- CreateVouchers marshaling ----------------------------------------------

func TestCreateVouchers_OmitsNilPointerFields(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		fmt.Fprint(w, `{"id":"v1"}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	req := CreateVoucherRequest{
		Count:            3,
		TimeLimitMinutes: 60,
	}
	_, err := c.CreateVouchers(context.Background(), "site1", req)
	if err != nil {
		t.Fatalf("CreateVouchers error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := body["dataUsageLimitMBytes"]; ok {
		t.Error("dataUsageLimitMBytes should be omitted when nil")
	}
	if _, ok := body["rxRateLimitKbps"]; ok {
		t.Error("rxRateLimitKbps should be omitted when nil")
	}
	countVal, ok := body["count"]
	if !ok {
		t.Error("count field missing")
	}
	// JSON numbers decode as float64
	if cnt, _ := countVal.(float64); int(cnt) != 3 {
		t.Errorf("count = %v, want 3", countVal)
	}
}

// ---- Insecure TLS -----------------------------------------------------------

func TestNew_InsecureSkipVerify(t *testing.T) {
	c := New(config.Config{Host: "myhost", APIKey: "k", Insecure: true})
	tr, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", c.httpClient.Transport)
	}
	if !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be true when cfg.Insecure=true")
	}
}

func TestNew_SecureByDefault(t *testing.T) {
	c := New(config.Config{Host: "myhost", APIKey: "k"})
	tr, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T", c.httpClient.Transport)
	}
	if tr.TLSClientConfig != nil && tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be false by default")
	}
}

// ---- New baseURL trimming ---------------------------------------------------

func TestNew_BaseURL_TrimsTrailingSlash(t *testing.T) {
	c := New(config.Config{Host: "myhost.local/", APIKey: "k"})
	want := "https://myhost.local/proxy/network/integration/v1"
	if c.baseURL != want {
		t.Errorf("baseURL = %q, want %q", c.baseURL, want)
	}
}

// ---- APIError.Error() -------------------------------------------------------

func TestAPIError_Error_NoRetryAfter(t *testing.T) {
	e := &APIError{StatusCode: 404, Code: "NOT_FOUND", Message: "not found"}
	s := e.Error()
	if !strings.Contains(s, "404") {
		t.Errorf("Error() = %q missing status", s)
	}
	if !strings.Contains(s, "not found") {
		t.Errorf("Error() = %q missing message", s)
	}
	if strings.Contains(s, "retry") {
		t.Errorf("Error() = %q should not mention retry when RetryAfter=0", s)
	}
}

func TestAPIError_Error_WithRetryAfter(t *testing.T) {
	e := &APIError{StatusCode: 429, Code: "RATE_LIMIT", Message: "slow down", RetryAfter: 10}
	s := e.Error()
	if !strings.Contains(s, "10") {
		t.Errorf("Error() = %q missing retry-after value", s)
	}
}

// ---- Info -------------------------------------------------------------------

func TestInfo_ReturnsRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// newTestClient overrides baseURL to server URL, so path is just /info.
		if r.URL.Path != "/info" {
			t.Errorf("path = %q, want /info", r.URL.Path)
		}
		fmt.Fprint(w, `{"version":"8.0"}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	raw, err := c.Info(context.Background())
	if err != nil {
		t.Fatalf("Info error: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["version"] != "8.0" {
		t.Errorf("version = %q", m["version"])
	}
}

// ---- Sites ------------------------------------------------------------------

func TestSites_SinglePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, string(pageResponse(0, 25, 1, 1, []any{map[string]string{"id": "s1", "name": "Default"}})))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	items, err := c.Sites(context.Background(), false, 0, 25)
	if err != nil {
		t.Fatalf("Sites error: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("got %d items, want 1", len(items))
	}
	var s Site
	if err := json.Unmarshal(items[0], &s); err != nil {
		t.Fatalf("unmarshal site: %v", err)
	}
	if s.ID != "s1" || s.Name != "Default" {
		t.Errorf("site = %+v", s)
	}
}

// ---- TLS handshake with insecure server ------------------------------------

func TestNew_InsecureClient_CanHitTLSServer(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	// Build a client with Insecure=true pointed at the TLS test server.
	host := strings.TrimPrefix(srv.URL, "https://")
	c := New(config.Config{Host: host, APIKey: "k", Insecure: true})
	// baseURL already uses https:// from New(), so it will point at TLS server.
	raw, err := c.Info(context.Background())
	if err != nil {
		t.Fatalf("expected success with InsecureSkipVerify: %v", err)
	}
	_ = raw
}

// ---- TLSClientConfig nil when secure ----------------------------------------

func TestNew_Secure_TransportHasNilOrFalseTLS(t *testing.T) {
	c := New(config.Config{Host: "host", APIKey: "k", Insecure: false})
	tr, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected Transport type %T", c.httpClient.Transport)
	}
	if tr.TLSClientConfig != nil && tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify must be false for secure client")
	}
}

// ---- Pagination query params ------------------------------------------------

func TestListRaw_SendsOffsetAndLimitQueryParams(t *testing.T) {
	var gotOffset, gotLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOffset = r.URL.Query().Get("offset")
		gotLimit = r.URL.Query().Get("limit")
		fmt.Fprint(w, string(pageResponse(10, 5, 0, 10, nil)))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.listRaw(context.Background(), "/sites", 10, 5)
	if err != nil {
		t.Fatalf("listRaw error: %v", err)
	}
	if gotOffset != "10" {
		t.Errorf("offset = %q, want 10", gotOffset)
	}
	if gotLimit != "5" {
		t.Errorf("limit = %q, want 5", gotLimit)
	}
}

// ---- Error fallback to raw body --------------------------------------------

func TestError_FallbackToRawBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		fmt.Fprint(w, "service unavailable")
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Info(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if apiErr.StatusCode != 503 {
		t.Errorf("StatusCode = %d", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Message, "service unavailable") {
		t.Errorf("Message = %q, want raw body", apiErr.Message)
	}
}

// ---- TLS type assertion guard -----------------------------------------------

// Ensure *http.Transport is what we get back — guards against future refactors.
func TestNew_TransportType(t *testing.T) {
	for _, insecure := range []bool{true, false} {
		c := New(config.Config{Host: "h", APIKey: "k", Insecure: insecure})
		if _, ok := c.httpClient.Transport.(*http.Transport); !ok {
			t.Errorf("insecure=%v: Transport type = %T", insecure, c.httpClient.Transport)
		}
	}
}

// ---- ensure TLSClientConfig is set for insecure (not nil) -------------------

func TestNew_Insecure_TLSConfig_NotNil(t *testing.T) {
	c := New(config.Config{Host: "h", APIKey: "k", Insecure: true})
	tr := c.httpClient.Transport.(*http.Transport)
	if tr.TLSClientConfig == nil {
		t.Fatal("TLSClientConfig should not be nil when Insecure=true")
	}
	if !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be true")
	}
}

// ---- Do (generic request wrapper) ------------------------------------------

func TestDo_GET_ReturnsRawAndSendsHeaders(t *testing.T) {
	var gotPath, gotMethod, gotAPIKey, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAPIKey = r.Header.Get("X-API-KEY")
		gotAccept = r.Header.Get("Accept")
		fmt.Fprint(w, `{"hello":"world"}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	raw, err := c.Do(context.Background(), http.MethodGet, "/sites/s1/devices", nil, nil)
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/sites/s1/devices" {
		t.Errorf("path = %q, want /sites/s1/devices", gotPath)
	}
	if gotAPIKey != "test-key" {
		t.Errorf("X-API-KEY = %q, want test-key", gotAPIKey)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["hello"] != "world" {
		t.Errorf("body = %s", string(raw))
	}
}

func TestDo_POST_SendsBodyAndContentType(t *testing.T) {
	var gotMethod, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		fmt.Fprint(w, `{"id":"created"}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	raw, err := c.Do(context.Background(), http.MethodPost, "/sites/s1/things", nil, map[string]any{"name": "x"})
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if !strings.Contains(gotContentType, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["name"] != "x" {
		t.Errorf("body name = %v, want x", body["name"])
	}
	var resp map[string]string
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if resp["id"] != "created" {
		t.Errorf("resp = %s", string(raw))
	}
}

func TestDo_DELETE_ReturnsRaw(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	raw, err := c.Do(context.Background(), http.MethodDelete, "/sites/s1/things/t1", nil, nil)
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if len(raw) != 0 {
		t.Errorf("raw = %q, want empty", string(raw))
	}
}

func TestDo_PropagatesQueryParams(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	q := url.Values{}
	q.Set("foo", "bar")
	q.Set("baz", "qux")
	_, err := c.Do(context.Background(), http.MethodGet, "/sites", q, nil)
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	if gotQuery.Get("foo") != "bar" {
		t.Errorf("query foo = %q, want bar", gotQuery.Get("foo"))
	}
	if gotQuery.Get("baz") != "qux" {
		t.Errorf("query baz = %q, want qux", gotQuery.Get("baz"))
	}
}

func TestDo_404_ReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"code":"NOT_FOUND","message":"nope"}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	raw, err := c.Do(context.Background(), http.MethodGet, "/missing", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if raw != nil {
		t.Errorf("raw = %q, want nil on error", string(raw))
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
}

// ---- ListPath (generic list wrapper) ---------------------------------------

func TestListPath_PaginatesAcrossPages(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		switch call {
		case 1:
			fmt.Fprint(w, string(pageResponse(0, 200, 2, 3, []any{"a", "b"})))
		case 2:
			fmt.Fprint(w, string(pageResponse(2, 200, 1, 3, []any{"c"})))
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	items, err := c.ListPath(context.Background(), "/things", true, 0, 0)
	if err != nil {
		t.Fatalf("ListPath error: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("got %d items, want 3", len(items))
	}
	if call != 2 {
		t.Errorf("expected 2 page calls, got %d", call)
	}
}

func TestResponseBodySizeLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"x":"`+strings.Repeat("a", 1000)+`"}`)
	}))
	defer srv.Close()

	orig := MaxResponseBytes
	MaxResponseBytes = 64
	defer func() { MaxResponseBytes = orig }()

	c := newTestClient(srv.URL)
	_, err := c.Info(context.Background())
	if err == nil {
		t.Fatal("expected error for oversized response body")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error %q should mention the size limit", err)
	}
}

func TestRedirectStripsAPIKeyCrossHost(t *testing.T) {
	var gotKeyOnTarget string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKeyOnTarget = r.Header.Get("X-API-KEY")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/info", http.StatusFound)
	}))
	defer source.Close()

	c := newTestClient(source.URL)
	if _, err := c.Info(context.Background()); err != nil {
		t.Fatalf("Info after redirect: %v", err)
	}
	if gotKeyOnTarget != "" {
		t.Errorf("X-API-KEY must not be forwarded to a different host on redirect, got %q", gotKeyOnTarget)
	}
}

func TestContextCancellationAbortsRequest(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // never responds until the test ends
	}))
	defer srv.Close()
	defer close(block)

	c := newTestClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := c.Info(ctx)
	if err == nil {
		t.Fatal("expected the request to be aborted by the context deadline")
	}
}
