// Package client provides an HTTP client for the UniFi Network Integration API.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/colindickson/unifi/internal/config"
)

// MaxResponseBytes caps how much of an HTTP response body the client reads,
// guarding against a malicious or misbehaving server exhausting memory. It is a
// variable so callers and tests can adjust it.
var MaxResponseBytes int64 = 32 << 20 // 32 MiB

// Client is an HTTP client for the UniFi Network Integration API.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// New creates a Client from cfg. The per-call timeout is governed by the
// context passed to each method, so callers control cancellation. When
// cfg.Insecure is true the TLS transport skips certificate verification.
func New(cfg config.Config) *Client {
	// The host is normally validated (and normalized) before New is called;
	// normalize defensively here so a scheme-prefixed host still builds a
	// correct base URL. Fall back to a trimmed value if normalization fails.
	host, err := config.NormalizeHost(cfg.Host)
	if err != nil {
		host = strings.TrimRight(cfg.Host, "/")
	}
	baseURL := "https://" + host + "/proxy/network/integration/v1"

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.Insecure, //nolint:gosec
		},
	}

	return &Client{
		baseURL: baseURL,
		apiKey:  cfg.APIKey,
		httpClient: &http.Client{
			Transport: tr,
			// Strip the API key on a redirect to a different host so the
			// credential is never forwarded to an unexpected origin, and cap
			// the redirect chain (a custom CheckRedirect disables the default).
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("client: stopped after 10 redirects")
				}
				if len(via) > 0 && req.URL.Host != via[0].URL.Host {
					req.Header.Del("X-API-KEY")
				}
				return nil
			},
		},
	}
}

// APIError is returned for any non-2xx HTTP response.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	RetryAfter int
}

// Error implements the error interface.
func (e *APIError) Error() string {
	s := fmt.Sprintf("API error %d: %s", e.StatusCode, e.Message)
	if e.RetryAfter > 0 {
		s += fmt.Sprintf(" (retry-after: %ds)", e.RetryAfter)
	}
	return s
}

// Page is the pagination envelope returned by list endpoints.
type Page struct {
	Offset     int             `json:"offset"`
	Limit      int             `json:"limit"`
	Count      int             `json:"count"`
	TotalCount int             `json:"totalCount"`
	Data       json.RawMessage `json:"data"`
}

// do executes an HTTP request and returns the raw response body.
// For non-2xx responses it returns *APIError.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	fullURL := c.baseURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("client: marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("client: build request: %w", err)
	}

	req.Header.Set("X-API-KEY", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("client: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("client: read body: %w", err)
	}
	if int64(len(respBody)) > MaxResponseBytes {
		return nil, fmt.Errorf("client: response body too large (limit %d bytes)", MaxResponseBytes)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{StatusCode: resp.StatusCode}

		// Parse Retry-After for 429.
		if resp.StatusCode == http.StatusTooManyRequests {
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if n, err := strconv.Atoi(ra); err == nil {
					apiErr.RetryAfter = n
				}
			}
		}

		// Try to parse JSON error body.
		// Try "code"/"message" first, then "statusName"/"message".
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

// listRaw fetches one page of results from path with given offset and limit.
func (c *Client) listRaw(ctx context.Context, path string, offset, limit int) (Page, error) {
	q := url.Values{}
	q.Set("offset", strconv.Itoa(offset))
	q.Set("limit", strconv.Itoa(limit))

	b, err := c.do(ctx, http.MethodGet, path, q, nil)
	if err != nil {
		return Page{}, err
	}

	var page Page
	if err := json.Unmarshal(b, &page); err != nil {
		return Page{}, fmt.Errorf("client: decode page: %w", err)
	}
	return page, nil
}

// listAll fetches all pages from path, accumulating results.
// pageSize defaults to 200 when <= 0.
func (c *Client) listAll(ctx context.Context, path string, pageSize int) ([]json.RawMessage, error) {
	if pageSize <= 0 {
		pageSize = 200
	}

	var all []json.RawMessage
	offset := 0

	for {
		page, err := c.listRaw(ctx, path, offset, pageSize)
		if err != nil {
			return nil, err
		}

		var items []json.RawMessage
		if len(page.Data) > 0 && string(page.Data) != "null" {
			if err := json.Unmarshal(page.Data, &items); err != nil {
				return nil, fmt.Errorf("client: decode page data: %w", err)
			}
		}

		// Guard against infinite loop: stop if page returned zero items.
		if len(items) == 0 {
			break
		}

		all = append(all, items...)

		// Stop when we've collected all items.
		if offset+len(items) >= page.TotalCount {
			break
		}

		offset += len(items)
	}

	return all, nil
}

// unmarshalData decodes a Page's Data field into []json.RawMessage.
func unmarshalData(data json.RawMessage) ([]json.RawMessage, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("client: decode data: %w", err)
	}
	return items, nil
}

// list is a helper: when all==true fetches all pages, else fetches one page.
func (c *Client) list(ctx context.Context, path string, all bool, offset, limit int) ([]json.RawMessage, error) {
	if all {
		return c.listAll(ctx, path, 200)
	}
	page, err := c.listRaw(ctx, path, offset, limit)
	if err != nil {
		return nil, err
	}
	return unmarshalData(page.Data)
}

// ---- Public API methods -----------------------------------------------------

// Do issues an arbitrary request against the v1 base and returns the raw
// response body as json.RawMessage (nil on error). path is relative to the v1
// base, is expected to already start with "/", and to already have any {site}
// substituted/escaped by the caller; it is not url-escaped here since it
// legitimately contains slashes.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error) {
	b, err := c.do(ctx, method, path, query, body)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// ListPath paginates an arbitrary list endpoint. When all is true it fetches
// all pages; otherwise it fetches the single page at offset/limit.
func (c *Client) ListPath(ctx context.Context, path string, all bool, offset, limit int) ([]json.RawMessage, error) {
	return c.list(ctx, path, all, offset, limit)
}

// Info fetches /info and returns the raw JSON.
func (c *Client) Info(ctx context.Context) (json.RawMessage, error) {
	b, err := c.do(ctx, http.MethodGet, "/info", nil, nil)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// Sites lists sites.
func (c *Client) Sites(ctx context.Context, all bool, offset, limit int) ([]json.RawMessage, error) {
	return c.list(ctx, "/sites", all, offset, limit)
}

// Devices lists devices for a site.
func (c *Client) Devices(ctx context.Context, siteID string, all bool, offset, limit int) ([]json.RawMessage, error) {
	return c.list(ctx, "/sites/"+url.PathEscape(siteID)+"/devices", all, offset, limit)
}

// Device fetches a single device.
func (c *Client) Device(ctx context.Context, siteID, deviceID string) (json.RawMessage, error) {
	b, err := c.do(ctx, http.MethodGet, "/sites/"+url.PathEscape(siteID)+"/devices/"+url.PathEscape(deviceID), nil, nil)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// DeviceStats fetches the latest statistics for a device.
func (c *Client) DeviceStats(ctx context.Context, siteID, deviceID string) (json.RawMessage, error) {
	path := "/sites/" + url.PathEscape(siteID) + "/devices/" + url.PathEscape(deviceID) + "/statistics/latest"
	b, err := c.do(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// DeviceAction posts an action to a device.
func (c *Client) DeviceAction(ctx context.Context, siteID, deviceID, action string) (json.RawMessage, error) {
	path := "/sites/" + url.PathEscape(siteID) + "/devices/" + url.PathEscape(deviceID) + "/actions"
	body := map[string]string{"action": action}
	b, err := c.do(ctx, http.MethodPost, path, nil, body)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// PortAction posts an action to a port on a device.
func (c *Client) PortAction(ctx context.Context, siteID, deviceID string, portIdx int, action string) (json.RawMessage, error) {
	path := fmt.Sprintf("/sites/%s/devices/%s/interfaces/ports/%d/actions", url.PathEscape(siteID), url.PathEscape(deviceID), portIdx)
	body := map[string]string{"action": action}
	b, err := c.do(ctx, http.MethodPost, path, nil, body)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// Clients lists clients for a site.
func (c *Client) Clients(ctx context.Context, siteID string, all bool, offset, limit int) ([]json.RawMessage, error) {
	return c.list(ctx, "/sites/"+url.PathEscape(siteID)+"/clients", all, offset, limit)
}

// ClientItem fetches a single client.
func (c *Client) ClientItem(ctx context.Context, siteID, clientID string) (json.RawMessage, error) {
	b, err := c.do(ctx, http.MethodGet, "/sites/"+url.PathEscape(siteID)+"/clients/"+url.PathEscape(clientID), nil, nil)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// ClientAction posts an action to a client.
func (c *Client) ClientAction(ctx context.Context, siteID, clientID, action string) (json.RawMessage, error) {
	path := "/sites/" + url.PathEscape(siteID) + "/clients/" + url.PathEscape(clientID) + "/actions"
	body := map[string]string{"action": action}
	b, err := c.do(ctx, http.MethodPost, path, nil, body)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// Vouchers lists hotspot vouchers for a site.
func (c *Client) Vouchers(ctx context.Context, siteID string, all bool, offset, limit int) ([]json.RawMessage, error) {
	return c.list(ctx, "/sites/"+url.PathEscape(siteID)+"/hotspot/vouchers", all, offset, limit)
}

// CreateVoucherRequest holds the fields for creating vouchers.
type CreateVoucherRequest struct {
	Name                 string `json:"name,omitempty"`
	Count                int    `json:"count"`
	TimeLimitMinutes     int    `json:"timeLimitMinutes"`
	DataUsageLimitMBytes *int   `json:"dataUsageLimitMBytes,omitempty"`
	RxRateLimitKbps      *int   `json:"rxRateLimitKbps,omitempty"`
	TxRateLimitKbps      *int   `json:"txRateLimitKbps,omitempty"`
	AuthorizedGuestLimit *int   `json:"authorizedGuestLimit,omitempty"`
}

// CreateVouchers creates vouchers for a site.
func (c *Client) CreateVouchers(ctx context.Context, siteID string, req CreateVoucherRequest) (json.RawMessage, error) {
	b, err := c.do(ctx, http.MethodPost, "/sites/"+url.PathEscape(siteID)+"/hotspot/vouchers", nil, req)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// DeleteVoucher deletes a voucher.
func (c *Client) DeleteVoucher(ctx context.Context, siteID, voucherID string) (json.RawMessage, error) {
	b, err := c.do(ctx, http.MethodDelete, "/sites/"+url.PathEscape(siteID)+"/hotspot/vouchers/"+url.PathEscape(voucherID), nil, nil)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// ---- Convenience decoded types for CLI table output -------------------------

// Site is a minimal decoded representation of a UniFi site.
type Site struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Device is a minimal decoded representation of a UniFi device.
type Device struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Model      string `json:"model"`
	State      string `json:"state"`
	MacAddress string `json:"macAddress"`
}

// ClientInfo is a minimal decoded representation of a UniFi client. It is named
// ClientInfo rather than Client to avoid conflicting with the Client type.
type ClientInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IPAddress  string `json:"ipAddress"`
	MacAddress string `json:"macAddress"`
}
