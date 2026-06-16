// Package smclient provides an HTTP client for the UniFi Site Manager (cloud)
// API at https://api.ui.com. It is parallel to internal/client (which targets a
// local console's Network Integration API): the Site Manager API is account-wide,
// cursor-paginated (pageSize/nextToken), read-only today, and authenticated with
// the same X-API-KEY header. It reuses client.APIError so the CLI renders cloud
// and local errors uniformly.
package smclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/colindickson/unifi/internal/client"
	"github.com/colindickson/unifi/internal/config"
)

// MaxResponseBytes caps how much of a response body is read.
var MaxResponseBytes int64 = 32 << 20 // 32 MiB

// DefaultBaseURL is the fixed Site Manager cloud endpoint.
const DefaultBaseURL = "https://api.ui.com"

// Client is an HTTP client for the UniFi Site Manager API.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// New creates a Client from cfg. Only cfg.APIKey is used; the base URL is fixed
// (DefaultBaseURL) unless overridden by UNIFI_SITE_MANAGER_URL (used by tests and
// to follow a future EA host move). The cloud endpoint presents a valid public
// certificate, so cfg.Insecure is intentionally ignored.
func New(cfg config.Config) *Client {
	base := os.Getenv("UNIFI_SITE_MANAGER_URL")
	if base == "" {
		base = DefaultBaseURL
	}
	base = strings.TrimRight(base, "/")

	return &Client{
		baseURL: base,
		apiKey:  cfg.APIKey,
		httpClient: &http.Client{
			Transport: &http.Transport{Proxy: http.ProxyFromEnvironment},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("smclient: stopped after 10 redirects")
				}
				if len(via) > 0 {
					orig := via[0].URL
					if req.URL.Host != orig.Host || (orig.Scheme == "https" && req.URL.Scheme == "http") {
						req.Header.Del("X-API-KEY")
					}
				}
				return nil
			},
		},
	}
}

// do executes an HTTP request and returns the raw body. Non-2xx responses return
// *client.APIError, mirroring internal/client's error parsing for a uniform CLI.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	fullURL := c.baseURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("smclient: marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("smclient: build request: %w", err)
	}
	req.Header.Set("X-API-KEY", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("smclient: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("smclient: read body: %w", err)
	}
	if int64(len(respBody)) > MaxResponseBytes {
		return nil, fmt.Errorf("smclient: response body too large (limit %d bytes)", MaxResponseBytes)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &client.APIError{StatusCode: resp.StatusCode}
		if resp.StatusCode == http.StatusTooManyRequests {
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if n, err := strconv.Atoi(ra); err == nil {
					apiErr.RetryAfter = n
				}
			}
		}
		var parsed struct {
			Code       string `json:"code"`
			StatusName string `json:"statusName"`
			Message    string `json:"message"`
		}
		if json.Unmarshal(respBody, &parsed) == nil {
			switch {
			case parsed.Code != "":
				apiErr.Code = parsed.Code
				apiErr.Message = parsed.Message
			case parsed.StatusName != "":
				apiErr.Code = parsed.StatusName
				apiErr.Message = parsed.Message
			default:
				apiErr.Message = string(respBody)
			}
		} else {
			apiErr.Message = string(respBody)
		}
		return nil, apiErr
	}

	return respBody, nil
}

// cursorPage is the Site Manager list envelope.
type cursorPage struct {
	Data      json.RawMessage `json:"data"`
	NextToken string          `json:"nextToken"`
}

// unmarshalItems decodes a cursor page's Data array into []json.RawMessage.
func unmarshalItems(data json.RawMessage) ([]json.RawMessage, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("smclient: decode data: %w", err)
	}
	return items, nil
}

// unwrapData returns the inner `data` payload of a single-object response, or the
// whole body when there is no `data` field (degrades gracefully).
func unwrapData(b []byte) json.RawMessage {
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(b, &env); err == nil && len(env.Data) > 0 {
		return env.Data
	}
	return json.RawMessage(b)
}

// listCursor fetches cursor-paginated results. base carries any extra query
// params (e.g. hostIds). When all is true it follows nextToken until empty;
// otherwise it returns the single first page. limit maps to pageSize when > 0.
func (c *Client) listCursor(ctx context.Context, path string, base url.Values, all bool, limit int) ([]json.RawMessage, error) {
	var out []json.RawMessage
	nextToken := ""
	const maxCursorPages = 100000
	pages := 0
	for {
		pages++
		if pages > maxCursorPages {
			return nil, fmt.Errorf("smclient: cursor pagination exceeded %d pages", maxCursorPages)
		}
		q := url.Values{}
		for k, vs := range base {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		if limit > 0 {
			q.Set("pageSize", strconv.Itoa(limit))
		}
		if nextToken != "" {
			q.Set("nextToken", nextToken)
		}

		b, err := c.do(ctx, http.MethodGet, path, q, nil)
		if err != nil {
			return nil, err
		}
		var page cursorPage
		if err := json.Unmarshal(b, &page); err != nil {
			return nil, fmt.Errorf("smclient: decode page: %w", err)
		}
		items, err := unmarshalItems(page.Data)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			break
		}
		out = append(out, items...)

		if !all || page.NextToken == "" {
			break
		}
		nextToken = page.NextToken
	}
	return out, nil
}

// Do issues an arbitrary request and returns the raw response body (no envelope
// unwrapping), backing the `site-manager api` passthrough.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error) {
	b, err := c.do(ctx, method, path, query, body)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// ListPath paginates an arbitrary cursor list endpoint.
func (c *Client) ListPath(ctx context.Context, path string, all bool, limit int) ([]json.RawMessage, error) {
	return c.listCursor(ctx, path, nil, all, limit)
}
