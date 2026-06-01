package cli

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// withStdin replaces the injectable stdin reader for the duration of a test.
func withStdin(t *testing.T, content string) {
	t.Helper()
	orig := stdinReader
	stdinReader = func() io.Reader { return strings.NewReader(content) }
	t.Cleanup(func() { stdinReader = orig })
}

func TestAPIDataFileStdin(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	withStdin(t, `{"name":"from-stdin"}`)
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("api", "POST", "/sites/{site}/networks", "--data-file", "-", "--yes")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	body, ok := f.doBody.(json.RawMessage)
	if !ok {
		t.Fatalf("body type %T want json.RawMessage", f.doBody)
	}
	if string(body) != `{"name":"from-stdin"}` {
		t.Errorf("body=%q want stdin content", string(body))
	}
}

func TestAPIStdinSizeLimited(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")

	orig := maxStdinBytes
	maxStdinBytes = 16
	t.Cleanup(func() { maxStdinBytes = orig })
	withStdin(t, `{"x":"`+strings.Repeat("a", 100)+`"}`)
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("api", "POST", "/sites/{site}/networks", "--data-file", "-", "--yes")
	if code == 0 {
		t.Fatalf("oversized stdin should fail, got code=0")
	}
	if f.doCalled {
		t.Error("Do should not be called when stdin exceeds the limit")
	}
	if !strings.Contains(errb, "too large") {
		t.Errorf("stderr %q should mention the size limit", errb)
	}
}

func TestAPIGetSubstitutesSite(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("api", "GET", "/sites/{site}/firewall/policies")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if !f.doCalled {
		t.Fatal("Do not called")
	}
	if f.doMethod != "GET" {
		t.Errorf("method=%q want GET", f.doMethod)
	}
	if f.doPath != "/sites/s1/firewall/policies" {
		t.Errorf("path=%q want /sites/s1/firewall/policies", f.doPath)
	}
}

func TestAPIPostWithoutYesBlocked(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("api", "POST", "/sites/{site}/networks")
	if code != 1 {
		t.Fatalf("code=%d want 1", code)
	}
	if f.doCalled {
		t.Error("Do should not be called without --yes")
	}
	if errb == "" {
		t.Error("expected stderr message")
	}
}

func TestAPIPostWithDataAndYes(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("api", "POST", "/sites/{site}/networks", "--data", `{"name":"x"}`, "--yes")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if !f.doCalled || f.doMethod != "POST" {
		t.Fatalf("Do not called as POST: called=%v method=%q", f.doCalled, f.doMethod)
	}
	body, ok := f.doBody.(json.RawMessage)
	if !ok {
		t.Fatalf("body type %T want json.RawMessage", f.doBody)
	}
	if string(body) != `{"name":"x"}` {
		t.Errorf("body=%q want {\"name\":\"x\"}", string(body))
	}
}

func TestAPIBadMethod(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, _ := run("api", "FROB", "/x")
	if code != 2 {
		t.Fatalf("code=%d want 2", code)
	}
	if f.doCalled {
		t.Error("Do should not be called for bad method")
	}
}

func TestAPIInvalidJSON(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("api", "GET", "/foo", "--data", "{bad")
	if code != 1 {
		t.Fatalf("code=%d want 1 stderr=%s", code, errb)
	}
	if f.doCalled {
		t.Error("Do should not be called for invalid JSON")
	}
}

func TestAPINoSiteTokenNeedsNoSite(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	// No UNIFI_SITE set.
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("api", "GET", "/info")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if !f.doCalled || f.doPath != "/info" {
		t.Errorf("Do path=%q want /info", f.doPath)
	}
}

func TestAPIQueryParams(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("api", "GET", "/info", "--query", "a=1", "--query", "b=2")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f.doQuery.Get("a") != "1" || f.doQuery.Get("b") != "2" {
		t.Errorf("query=%v want a=1 b=2", f.doQuery)
	}
}

func TestAPIPathWithoutLeadingSlash(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("api", "GET", "info")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f.doPath != "/info" {
		t.Errorf("path=%q want /info", f.doPath)
	}
}

func TestAPISiteTokenWithoutSiteErrors(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("api", "GET", "/sites/{site}/networks")
	if code != 1 {
		t.Fatalf("code=%d want 1", code)
	}
	if errb == "" {
		t.Error("expected stderr about missing site")
	}
}

func TestAPIMissingArgs(t *testing.T) {
	clearEnv(t)
	code, _, errb := run("api")
	if code == 0 {
		t.Fatalf("code=%d want nonzero", code)
	}
	if errb == "" {
		t.Error("expected stderr message for missing args")
	}
}

// TestAPISiteIdNotSubstituted ensures that :siteId is NOT treated as the :site
// token — it must be left as-is and not require UNIFI_SITE to be set.
func TestAPISiteIdNotSubstituted(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	// Deliberately no UNIFI_SITE — if :siteId were mis-matched as :site, the
	// call would fail with "site is required".
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("api", "GET", "/sites/:siteId/foo")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if !f.doCalled {
		t.Fatal("Do not called")
	}
	if f.doPath != "/sites/:siteId/foo" {
		t.Errorf("path=%q want /sites/:siteId/foo (must not be substituted)", f.doPath)
	}
}

// TestAPISiteTokenColonSubstituted ensures that the bare :site segment IS
// substituted when UNIFI_SITE is set.
func TestAPISiteTokenColonSubstituted(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("api", "GET", "/sites/:site/foo")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f.doPath != "/sites/s1/foo" {
		t.Errorf("path=%q want /sites/s1/foo", f.doPath)
	}
}
