package cli

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/colindickson/unifi/internal/config"
)

// fakeSMAPI implements SMAPI, recording calls and returning canned data.
type fakeSMAPI struct {
	items []json.RawMessage
	obj   json.RawMessage
	err   error

	hostsAll   bool
	hostsLimit int
	hostID     string
	devHostIDs []string

	ispType  string
	ispQuery url.Values
	ispBody  json.RawMessage

	sdwanID     string
	sdwanStatus bool

	doCalled bool
	doMethod string
	doPath   string
}

func (f *fakeSMAPI) Hosts(ctx context.Context, all bool, limit int) ([]json.RawMessage, error) {
	f.hostsAll, f.hostsLimit = all, limit
	return f.items, f.err
}
func (f *fakeSMAPI) Host(ctx context.Context, id string) (json.RawMessage, error) {
	f.hostID = id
	return f.obj, f.err
}
func (f *fakeSMAPI) Sites(ctx context.Context, all bool, limit int) ([]json.RawMessage, error) {
	return f.items, f.err
}
func (f *fakeSMAPI) Devices(ctx context.Context, hostIDs []string, all bool, limit int) ([]json.RawMessage, error) {
	f.devHostIDs = hostIDs
	return f.items, f.err
}
func (f *fakeSMAPI) ISPMetrics(ctx context.Context, mtype string, q url.Values) (json.RawMessage, error) {
	f.ispType, f.ispQuery = mtype, q
	return f.obj, f.err
}
func (f *fakeSMAPI) QueryISPMetrics(ctx context.Context, mtype string, body json.RawMessage) (json.RawMessage, error) {
	f.ispType, f.ispBody = mtype, body
	return f.obj, f.err
}
func (f *fakeSMAPI) SDWANConfigs(ctx context.Context, all bool, limit int) ([]json.RawMessage, error) {
	return f.items, f.err
}
func (f *fakeSMAPI) SDWANConfig(ctx context.Context, id string) (json.RawMessage, error) {
	f.sdwanID = id
	return f.obj, f.err
}
func (f *fakeSMAPI) SDWANStatus(ctx context.Context, id string) (json.RawMessage, error) {
	f.sdwanID, f.sdwanStatus = id, true
	return f.obj, f.err
}
func (f *fakeSMAPI) Do(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error) {
	f.doCalled, f.doMethod, f.doPath = true, method, path
	return f.obj, f.err
}
func (f *fakeSMAPI) ListPath(ctx context.Context, path string, all bool, limit int) ([]json.RawMessage, error) {
	return f.items, f.err
}

func withFakeSMAPI(t *testing.T, f *fakeSMAPI) {
	t.Helper()
	orig := newSMAPI
	newSMAPI = func(cfg config.Config) SMAPI { return f }
	t.Cleanup(func() { newSMAPI = orig })
}

func TestSMClientSatisfiesInterface(t *testing.T) {
	// Compile-time check lives in sitemanager.go; this guards the test build.
	var _ SMAPI = (*fakeSMAPI)(nil)
}

func TestSM_HostsList_JSONAndTable(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeSMAPI{items: []json.RawMessage{json.RawMessage(`{"id":"h1","name":"HQ Console","ipAddress":"1.2.3.4"}`)}}
	withFakeSMAPI(t, f)

	code, out, errb := run("site-manager", "hosts", "list")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "[") || !json.Valid([]byte(out)) {
		t.Errorf("default should be a JSON array, got %q", out)
	}

	code, out, errb = run("site-manager", "hosts", "list", "-o", "table")
	if code != 0 {
		t.Fatalf("table code=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "HQ Console") {
		t.Errorf("table out %q missing host name", out)
	}
}

func TestSM_AliasSM(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeSMAPI{items: []json.RawMessage{json.RawMessage(`{"id":"s1","name":"Default"}`)}}
	withFakeSMAPI(t, f)
	code, _, errb := run("sm", "sites", "list")
	if code != 0 {
		t.Fatalf("sm alias code=%d stderr=%s", code, errb)
	}
}

func TestSM_RequiresAPIKeyNotHost(t *testing.T) {
	clearEnv(t) // no UNIFI_API_KEY
	f := &fakeSMAPI{}
	withFakeSMAPI(t, f)
	code, _, errb := run("site-manager", "hosts", "list")
	if code != 1 {
		t.Fatalf("code=%d want 1", code)
	}
	if !strings.Contains(errb, "api-key") {
		t.Errorf("stderr %q should mention api-key", errb)
	}

	// With only an API key (no host) it must succeed — host is irrelevant to cloud.
	clearEnv(t)
	t.Setenv("UNIFI_API_KEY", "k")
	f2 := &fakeSMAPI{items: []json.RawMessage{}}
	withFakeSMAPI(t, f2)
	code, _, errb = run("site-manager", "hosts", "list")
	if code != 0 {
		t.Fatalf("cloud should not require host: code=%d stderr=%s", code, errb)
	}
}

func TestSM_HostsGet(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeSMAPI{obj: json.RawMessage(`{"id":"h9"}`)}
	withFakeSMAPI(t, f)
	code, _, errb := run("site-manager", "hosts", "get", "h9")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f.hostID != "h9" {
		t.Errorf("hostID = %q, want h9", f.hostID)
	}
}

func TestSM_DevicesHostFilter(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeSMAPI{items: []json.RawMessage{}}
	withFakeSMAPI(t, f)
	code, _, errb := run("site-manager", "devices", "list", "--host-id", "h1", "--host-id", "h2")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if len(f.devHostIDs) != 2 || f.devHostIDs[0] != "h1" || f.devHostIDs[1] != "h2" {
		t.Errorf("devHostIDs = %v, want [h1 h2]", f.devHostIDs)
	}
}

func TestSM_AllAndLimitFlags(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeSMAPI{items: []json.RawMessage{}}
	withFakeSMAPI(t, f)
	code, _, errb := run("site-manager", "hosts", "list", "--all", "--limit", "10")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if !f.hostsAll || f.hostsLimit != 10 {
		t.Errorf("all=%v limit=%d, want true/10", f.hostsAll, f.hostsLimit)
	}
}

func TestSM_UnknownSubgroup(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeSMAPI{}
	withFakeSMAPI(t, f)
	code, _, errb := run("site-manager", "bogus")
	if code != 2 {
		t.Errorf("code=%d want 2", code)
	}
	if !strings.Contains(errb, "bogus") {
		t.Errorf("stderr %q should name the bad subgroup", errb)
	}
}

func TestSM_ISPMetricsGet(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeSMAPI{obj: json.RawMessage(`{"metrics":[]}`)}
	withFakeSMAPI(t, f)
	code, _, errb := run("site-manager", "isp-metrics", "get", "5m", "--duration", "24h")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f.ispType != "5m" {
		t.Errorf("ispType = %q, want 5m", f.ispType)
	}
	if f.ispQuery.Get("duration") != "24h" {
		t.Errorf("duration = %q, want 24h", f.ispQuery.Get("duration"))
	}
}

func TestSM_ISPMetricsGet_BeginEnd(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeSMAPI{obj: json.RawMessage(`{}`)}
	withFakeSMAPI(t, f)
	code, _, errb := run("site-manager", "isp-metrics", "get", "1h",
		"--begin", "2026-06-01T00:00:00Z", "--end", "2026-06-02T00:00:00Z")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f.ispQuery.Get("beginTimestamp") != "2026-06-01T00:00:00Z" {
		t.Errorf("beginTimestamp = %q", f.ispQuery.Get("beginTimestamp"))
	}
	if f.ispQuery.Get("endTimestamp") != "2026-06-02T00:00:00Z" {
		t.Errorf("endTimestamp = %q", f.ispQuery.Get("endTimestamp"))
	}
}

func TestSM_ISPMetricsGet_RequiresType(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeSMAPI{}
	withFakeSMAPI(t, f)
	code, _, errb := run("site-manager", "isp-metrics", "get")
	if code != 2 {
		t.Errorf("code=%d want 2", code)
	}
	if !strings.Contains(errb, "type") {
		t.Errorf("stderr %q should mention the type arg", errb)
	}
}

func TestSM_ISPMetricsQuery(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeSMAPI{obj: json.RawMessage(`{}`)}
	withFakeSMAPI(t, f)
	code, _, errb := run("site-manager", "isp-metrics", "query", "1h", "--data", `{"sites":["s1"]}`)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f.ispType != "1h" {
		t.Errorf("ispType = %q, want 1h", f.ispType)
	}
	if !strings.Contains(string(f.ispBody), "s1") {
		t.Errorf("body = %s, want sites filter", f.ispBody)
	}
}

func TestSM_ISPMetricsQuery_RequiresData(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeSMAPI{}
	withFakeSMAPI(t, f)
	code, _, errb := run("site-manager", "isp-metrics", "query", "1h")
	if code != 1 {
		t.Fatalf("query without --data should exit 1 (missing-body convention), got %d", code)
	}
	if !strings.Contains(errb, "data") {
		t.Errorf("stderr %q should mention --data", errb)
	}
}

func TestSM_SDWANListGetStatus(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_API_KEY", "k")

	f := &fakeSMAPI{items: []json.RawMessage{json.RawMessage(`{"id":"c1","name":"Hub","status":"active"}`)}}
	withFakeSMAPI(t, f)
	code, out, errb := run("site-manager", "sdwan", "list", "-o", "table")
	if code != 0 {
		t.Fatalf("list code=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "Hub") {
		t.Errorf("table out %q missing config name", out)
	}

	f2 := &fakeSMAPI{obj: json.RawMessage(`{"id":"c1"}`)}
	withFakeSMAPI(t, f2)
	code, _, errb = run("site-manager", "sdwan", "get", "c1")
	if code != 0 {
		t.Fatalf("get code=%d stderr=%s", code, errb)
	}
	if f2.sdwanID != "c1" || f2.sdwanStatus {
		t.Errorf("get routed wrong: id=%q status=%v", f2.sdwanID, f2.sdwanStatus)
	}

	f3 := &fakeSMAPI{obj: json.RawMessage(`{"state":"ok"}`)}
	withFakeSMAPI(t, f3)
	code, _, errb = run("site-manager", "sdwan", "status", "c1")
	if code != 0 {
		t.Fatalf("status code=%d stderr=%s", code, errb)
	}
	if f3.sdwanID != "c1" || !f3.sdwanStatus {
		t.Errorf("status routed wrong: id=%q status=%v", f3.sdwanID, f3.sdwanStatus)
	}
}

func TestSM_APIPassthrough(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeSMAPI{obj: json.RawMessage(`{"data":{"ok":true}}`)}
	withFakeSMAPI(t, f)
	code, _, errb := run("site-manager", "api", "GET", "/v1/hosts")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if !f.doCalled || f.doMethod != "GET" || f.doPath != "/v1/hosts" {
		t.Errorf("Do not called correctly: called=%v method=%q path=%q", f.doCalled, f.doMethod, f.doPath)
	}
}

func TestSM_APIPassthrough_InvalidMethod(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeSMAPI{}
	withFakeSMAPI(t, f)
	code, _, errb := run("site-manager", "api", "FETCH", "/v1/hosts")
	if code != 2 {
		t.Errorf("code=%d want 2", code)
	}
	if !strings.Contains(errb, "method") {
		t.Errorf("stderr %q should mention invalid method", errb)
	}
}
