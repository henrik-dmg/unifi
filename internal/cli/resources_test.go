package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResourceNetworksList(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	f := &fakeAPI{}
	withFakeAPI(t, f)

	code, _, errb := run("networks", "list")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f.listPath != "/sites/s1/networks" {
		t.Errorf("listPath=%q want /sites/s1/networks", f.listPath)
	}
}

func TestResourceNetworksListTable(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_OUTPUT", "")
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	f := &fakeAPI{listRawItems: []json.RawMessage{
		json.RawMessage(`{"id":"n1","name":"LAN","enabled":true}`),
	}}
	withFakeAPI(t, f)

	code, out, errb := run("networks", "list", "-o", "table")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "n1") || !strings.Contains(out, "LAN") {
		t.Errorf("table out %q missing n1/LAN", out)
	}
}

func TestResourceFirewallPoliciesList(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	f := &fakeAPI{}
	withFakeAPI(t, f)

	code, _, errb := run("firewall", "policies", "list")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f.listPath != "/sites/s1/firewall/policies" {
		t.Errorf("listPath=%q want /sites/s1/firewall/policies", f.listPath)
	}
}

func TestResourceFirewallZonesGet(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("firewall", "zones", "get", "z1")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f.doMethod != "GET" || f.doPath != "/sites/s1/firewall/zones/z1" {
		t.Errorf("Do method=%q path=%q want GET /sites/s1/firewall/zones/z1", f.doMethod, f.doPath)
	}
}

func TestResourceDNSCreate(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("dns", "create", "--data", `{"x":1}`, "--yes")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f.doMethod != "POST" || f.doPath != "/sites/s1/dns/policies" {
		t.Errorf("Do method=%q path=%q want POST /sites/s1/dns/policies", f.doMethod, f.doPath)
	}
	body, _ := f.doBody.(json.RawMessage)
	if string(body) != `{"x":1}` {
		t.Errorf("body=%q want {\"x\":1}", string(body))
	}
}

func TestResourceDNSCreateNoYesBlocked(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, _ := run("dns", "create", "--data", `{"x":1}`)
	if code != 1 {
		t.Fatalf("code=%d want 1", code)
	}
	if f.doCalled {
		t.Error("Do should not be called without --yes")
	}
}

func TestResourceNetworksUpdate(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("networks", "update", "n1", "--data", `{"x":1}`, "--yes")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f.doMethod != "PUT" || f.doPath != "/sites/s1/networks/n1" {
		t.Errorf("Do method=%q path=%q want PUT /sites/s1/networks/n1", f.doMethod, f.doPath)
	}
}

func TestResourceDNSDelete(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("dns", "delete", "p1", "--yes")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f.doMethod != "DELETE" || f.doPath != "/sites/s1/dns/policies/p1" {
		t.Errorf("Do method=%q path=%q want DELETE /sites/s1/dns/policies/p1", f.doMethod, f.doPath)
	}
}

func TestResourceCountriesListNoSite(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	// No UNIFI_SITE.
	f := &fakeAPI{}
	withFakeAPI(t, f)

	code, _, errb := run("countries", "list")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f.listPath != "/countries" {
		t.Errorf("listPath=%q want /countries", f.listPath)
	}
}

func TestResourceBogusUnknown(t *testing.T) {
	clearEnv(t)
	code, _, _ := run("bogusresource", "list")
	if code != 2 {
		t.Fatalf("code=%d want 2", code)
	}
}

func TestFirewallBadSubcommand(t *testing.T) {
	clearEnv(t)
	// No host/apikey needed — error is reported before any API call.
	code, _, errb := run("firewall", "badsub", "list")
	if code != 2 {
		t.Fatalf("code=%d want 2", code)
	}
	// The error must NOT be the generic "unknown command" message — it must
	// mention that firewall is valid but the subcommand is wrong.
	if strings.Contains(errb, `unknown command "firewall"`) {
		t.Errorf("stderr %q must not say unknown command firewall (firewall IS a valid group)", errb)
	}
	// Must mention firewall and at least one valid sub.
	if !strings.Contains(errb, "firewall") {
		t.Errorf("stderr %q missing \"firewall\"", errb)
	}
	if !strings.Contains(errb, "zones") && !strings.Contains(errb, "policies") {
		t.Errorf("stderr %q missing \"zones\" or \"policies\"", errb)
	}
}

func TestResourceCountriesCreateNotAllowed(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, _ := run("countries", "create", "--data", `{"x":1}`, "--yes")
	if code != 2 {
		t.Fatalf("code=%d want 2", code)
	}
	if f.doCalled {
		t.Error("Do should not be called for disallowed op")
	}
}
