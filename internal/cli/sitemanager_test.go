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
