package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colindickson/unifi/internal/config"
)

// --- FromEnv ---

func TestFromEnv_AllVars(t *testing.T) {
	t.Setenv("UNIFI_HOST", "https://192.168.1.1")
	t.Setenv("UNIFI_API_KEY", "secret")
	t.Setenv("UNIFI_SITE", "default")
	t.Setenv("UNIFI_INSECURE", "true")

	c := config.FromEnv()
	if c.Host != "https://192.168.1.1" {
		t.Errorf("Host = %q, want %q", c.Host, "https://192.168.1.1")
	}
	if c.APIKey != "secret" {
		t.Errorf("APIKey = %q, want %q", c.APIKey, "secret")
	}
	if c.Site != "default" {
		t.Errorf("Site = %q, want %q", c.Site, "default")
	}
	if !c.Insecure {
		t.Error("Insecure should be true")
	}
}

func TestFromEnv_InsecureTruthyValues(t *testing.T) {
	truthyValues := []string{"1", "true", "yes", "on", "TRUE", "YES", "ON", " true ", " 1 "}
	for _, v := range truthyValues {
		t.Run(v, func(t *testing.T) {
			t.Setenv("UNIFI_INSECURE", v)
			c := config.FromEnv()
			if !c.Insecure {
				t.Errorf("Insecure should be true for value %q", v)
			}
		})
	}
}

func TestFromEnv_InsecureFalseyValues(t *testing.T) {
	falseyValues := []string{"0", "false", "no", "off", "", "FALSE", "NO", "OFF"}
	for _, v := range falseyValues {
		t.Run(v, func(t *testing.T) {
			t.Setenv("UNIFI_INSECURE", v)
			c := config.FromEnv()
			if c.Insecure {
				t.Errorf("Insecure should be false for value %q", v)
			}
		})
	}
}

func TestFromEnv_Empty(t *testing.T) {
	// Ensure vars are unset.
	t.Setenv("UNIFI_HOST", "")
	t.Setenv("UNIFI_API_KEY", "")
	t.Setenv("UNIFI_SITE", "")
	t.Setenv("UNIFI_INSECURE", "")

	c := config.FromEnv()
	if c.Host != "" || c.APIKey != "" || c.Site != "" || c.Insecure {
		t.Errorf("Expected zero Config, got %+v", c)
	}
}

// --- Load / Save round-trip ---

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.json")

	orig := config.Config{
		Host:     "https://unifi.local",
		APIKey:   "mykey",
		Site:     "mysite",
		Insecure: true,
	}

	if err := config.Save(path, orig); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded != orig {
		t.Errorf("round-trip mismatch: got %+v, want %+v", loaded, orig)
	}
}

func TestSave_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := config.Save(path, config.Config{Host: "h", APIKey: "k"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file perm = %04o, want 0600", perm)
	}
}

func TestSave_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "config.json")

	if err := config.Save(path, config.Config{Host: "h", APIKey: "k"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should exist after Save: %v", err)
	}
}

func TestSave_PrettyPrinted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	c := config.Config{Host: "h", APIKey: "k", Site: "s"}
	if err := config.Save(path, c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Must be valid JSON.
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}

	// Must have indentation (pretty-printed).
	raw := string(data)
	if len(raw) == 0 {
		t.Fatal("empty output")
	}
	// Simple check: pretty-printed JSON contains newlines.
	found := false
	for _, ch := range raw {
		if ch == '\n' {
			found = true
			break
		}
	}
	if !found {
		t.Error("JSON output does not appear to be pretty-printed (no newlines)")
	}
}

func TestLoad_NonExistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")

	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load of non-existent file should return nil error, got: %v", err)
	}
	if c != (config.Config{}) {
		t.Errorf("expected zero Config, got %+v", c)
	}
}

func TestLoad_BadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")

	if err := os.WriteFile(path, []byte("{not valid json"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := config.Load(path)
	if err == nil {
		t.Error("Load of bad JSON should return an error")
	}
}

// --- DefaultPath ---

func TestDefaultPath_EnvVar(t *testing.T) {
	t.Setenv("UNIFI_CONFIG", "/custom/path/config.json")
	got := config.DefaultPath()
	if got != "/custom/path/config.json" {
		t.Errorf("DefaultPath = %q, want /custom/path/config.json", got)
	}
}

func TestDefaultPath_HomeDir(t *testing.T) {
	t.Setenv("UNIFI_CONFIG", "")
	got := config.DefaultPath()
	if got == "" {
		t.Fatal("DefaultPath returned empty string")
	}
	// Must end with the expected suffix.
	want := filepath.Join(".config", "unifi", "config.json")
	if len(got) < len(want) || got[len(got)-len(want):] != want {
		t.Errorf("DefaultPath = %q, expected suffix %q", got, want)
	}
}

// --- Merge ---

func TestMerge_OverrideWins(t *testing.T) {
	base := config.Config{Host: "base-host", APIKey: "base-key", Site: "base-site"}
	override := config.Config{Host: "new-host", APIKey: "new-key", Site: "new-site"}

	result := config.Merge(base, override)
	if result.Host != "new-host" {
		t.Errorf("Host = %q, want new-host", result.Host)
	}
	if result.APIKey != "new-key" {
		t.Errorf("APIKey = %q, want new-key", result.APIKey)
	}
	if result.Site != "new-site" {
		t.Errorf("Site = %q, want new-site", result.Site)
	}
}

func TestMerge_EmptyOverridePreservesBase(t *testing.T) {
	base := config.Config{Host: "base-host", APIKey: "base-key", Site: "base-site", Insecure: false}
	override := config.Config{}

	result := config.Merge(base, override)
	if result.Host != "base-host" {
		t.Errorf("Host = %q, want base-host", result.Host)
	}
	if result.APIKey != "base-key" {
		t.Errorf("APIKey = %q, want base-key", result.APIKey)
	}
	if result.Site != "base-site" {
		t.Errorf("Site = %q, want base-site", result.Site)
	}
	if result.Insecure {
		t.Error("Insecure should remain false")
	}
}

func TestMerge_InsecureTrueOverrideWins(t *testing.T) {
	base := config.Config{Host: "h", APIKey: "k", Insecure: false}
	override := config.Config{Insecure: true}

	result := config.Merge(base, override)
	if !result.Insecure {
		t.Error("Insecure should be true when override.Insecure is true")
	}
}

func TestMerge_InsecureFalseOverridePreservesBase(t *testing.T) {
	// When base is true and override is false (zero value), base should be preserved.
	base := config.Config{Host: "h", APIKey: "k", Insecure: true}
	override := config.Config{Insecure: false}

	result := config.Merge(base, override)
	// Per spec: Insecure can only be turned ON by an override.
	if !result.Insecure {
		t.Error("Insecure should remain true (false override is zero value, base wins)")
	}
}

func TestMerge_PartialOverride(t *testing.T) {
	base := config.Config{Host: "base-host", APIKey: "base-key", Site: "base-site"}
	override := config.Config{Host: "new-host"}

	result := config.Merge(base, override)
	if result.Host != "new-host" {
		t.Errorf("Host = %q, want new-host", result.Host)
	}
	if result.APIKey != "base-key" {
		t.Errorf("APIKey = %q, want base-key (from base)", result.APIKey)
	}
	if result.Site != "base-site" {
		t.Errorf("Site = %q, want base-site (from base)", result.Site)
	}
}

func TestFromEnvSource_Output(t *testing.T) {
	t.Setenv("UNIFI_OUTPUT", "table")
	c, _ := config.FromEnvSource()
	if c.Output != "table" {
		t.Errorf("Output = %q, want table", c.Output)
	}
}

// --- Merge (Output) ---

func TestMerge_OutputNonEmptyWins(t *testing.T) {
	base := config.Config{Output: "table"}
	override := config.Config{Output: "json"}
	if result := config.Merge(base, override); result.Output != "json" {
		t.Errorf("Output = %q, want json", result.Output)
	}
}

func TestMerge_OutputEmptyOverridePreservesBase(t *testing.T) {
	base := config.Config{Output: "table"}
	override := config.Config{}
	if result := config.Merge(base, override); result.Output != "table" {
		t.Errorf("Output = %q, want table (base preserved)", result.Output)
	}
}

// --- ResolveOutput ---

func TestResolveOutput_Precedence(t *testing.T) {
	if got := config.ResolveOutput("file", "env", "flag"); got != "flag" {
		t.Errorf("ResolveOutput flag = %q, want flag", got)
	}
	if got := config.ResolveOutput("file", "env", ""); got != "env" {
		t.Errorf("ResolveOutput env = %q, want env", got)
	}
	if got := config.ResolveOutput("file", "", ""); got != "file" {
		t.Errorf("ResolveOutput file = %q, want file", got)
	}
}

func TestResolveOutput_DefaultWhenEmpty(t *testing.T) {
	if got := config.ResolveOutput("", "", ""); got != config.DefaultOutput {
		t.Errorf("ResolveOutput empty = %q, want %q", got, config.DefaultOutput)
	}
	if config.DefaultOutput != "json" {
		t.Errorf("DefaultOutput = %q, want json", config.DefaultOutput)
	}
}

// --- FromEnvSource ---

func TestFromEnvSource_InsecurePresent(t *testing.T) {
	t.Setenv("UNIFI_INSECURE", "false")
	c, set := config.FromEnvSource()
	if !set {
		t.Error("InsecureSet should be true when UNIFI_INSECURE is present (even if falsey)")
	}
	if c.Insecure {
		t.Error("Insecure should be false for value \"false\"")
	}
}

func TestFromEnvSource_InsecureAbsent(t *testing.T) {
	os.Unsetenv("UNIFI_INSECURE")
	t.Cleanup(func() { os.Unsetenv("UNIFI_INSECURE") })
	_, set := config.FromEnvSource()
	if set {
		t.Error("InsecureSet should be false when UNIFI_INSECURE is unset")
	}
}

// --- Resolve ---

func TestResolve_FlagFalseBeatsFileTrue(t *testing.T) {
	file := config.Config{Insecure: true}
	got := config.Resolve(file, config.Config{}, false, config.Config{Insecure: false}, true)
	if got.Insecure {
		t.Error("flag insecure=false should override file insecure=true")
	}
}

func TestResolve_EnvFalseBeatsFileTrueWhenNoFlag(t *testing.T) {
	file := config.Config{Insecure: true}
	got := config.Resolve(file, config.Config{Insecure: false}, true, config.Config{}, false)
	if got.Insecure {
		t.Error("env insecure=false should override file insecure=true when no flag set")
	}
}

func TestResolve_FileTruePreservedWhenNeitherSet(t *testing.T) {
	file := config.Config{Insecure: true}
	got := config.Resolve(file, config.Config{}, false, config.Config{}, false)
	if !got.Insecure {
		t.Error("file insecure=true should be preserved when neither flag nor env set")
	}
}

func TestResolve_FlagTrueBeatsEverything(t *testing.T) {
	file := config.Config{Insecure: false}
	got := config.Resolve(file, config.Config{Insecure: false}, true, config.Config{Insecure: true}, true)
	if !got.Insecure {
		t.Error("flag insecure=true should win over env/file")
	}
}

func TestResolve_StringPrecedenceFlagOverEnvOverFile(t *testing.T) {
	file := config.Config{Host: "file-host", APIKey: "file-key", Site: "file-site"}
	env := config.Config{Host: "env-host", APIKey: "env-key"}
	flags := config.Config{Host: "flag-host"}

	got := config.Resolve(file, env, false, flags, false)
	if got.Host != "flag-host" {
		t.Errorf("Host = %q, want flag-host", got.Host)
	}
	if got.APIKey != "env-key" {
		t.Errorf("APIKey = %q, want env-key", got.APIKey)
	}
	if got.Site != "file-site" {
		t.Errorf("Site = %q, want file-site", got.Site)
	}
}

func TestResolve_OutputDefaultsToJSON(t *testing.T) {
	got := config.Resolve(config.Config{}, config.Config{}, false, config.Config{}, false)
	if got.Output != "json" {
		t.Errorf("Output = %q, want json (default)", got.Output)
	}
}

func TestResolve_OutputPrecedenceFlagOverEnvOverFile(t *testing.T) {
	file := config.Config{Output: "file-fmt"}
	env := config.Config{Output: "env-fmt"}
	flags := config.Config{Output: "flag-fmt"}

	if got := config.Resolve(file, env, false, flags, false); got.Output != "flag-fmt" {
		t.Errorf("Output = %q, want flag-fmt", got.Output)
	}
	if got := config.Resolve(file, env, false, config.Config{}, false); got.Output != "env-fmt" {
		t.Errorf("Output = %q, want env-fmt", got.Output)
	}
	if got := config.Resolve(file, config.Config{}, false, config.Config{}, false); got.Output != "file-fmt" {
		t.Errorf("Output = %q, want file-fmt", got.Output)
	}
}

// --- Validate ---

func TestValidate_MissingHost(t *testing.T) {
	c := config.Config{APIKey: "key"}
	if err := c.Validate(); err == nil {
		t.Error("expected error when Host is empty")
	}
}

func TestValidate_MissingAPIKey(t *testing.T) {
	c := config.Config{Host: "https://unifi.local"}
	if err := c.Validate(); err == nil {
		t.Error("expected error when APIKey is empty")
	}
}

func TestValidate_Valid(t *testing.T) {
	c := config.Config{Host: "https://unifi.local", APIKey: "key"}
	if err := c.Validate(); err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

func TestValidate_BothMissing(t *testing.T) {
	c := config.Config{}
	if err := c.Validate(); err == nil {
		t.Error("expected error when both Host and APIKey are empty")
	}
}

func TestValidate_HostErrorMessage(t *testing.T) {
	c := config.Config{APIKey: "key"}
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "host") && !strings.Contains(msg, "UNIFI_HOST") {
		t.Errorf("error message %q should mention 'host' or 'UNIFI_HOST'", msg)
	}
}

func TestValidate_APIKeyErrorMessage(t *testing.T) {
	c := config.Config{Host: "https://unifi.local"}
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	found := false
	for _, kw := range []string{"api-key", "api_key", "UNIFI_API_KEY"} {
		if strings.Contains(msg, kw) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("error message %q should mention api-key or UNIFI_API_KEY", msg)
	}
}

func TestValidate_RejectsHostWithPath(t *testing.T) {
	for _, host := range []string{
		"192.168.1.1/../admin",
		"unifi.local/proxy",
		"https://attacker.com/path",
		"host with space",
		"unifi.local?x=1",
	} {
		c := config.Config{Host: host, APIKey: "key"}
		if err := c.Validate(); err == nil {
			t.Errorf("Validate(%q) should fail (path/scheme-with-path/whitespace not allowed)", host)
		}
	}
}

func TestValidate_AcceptsHostForms(t *testing.T) {
	for _, host := range []string{
		"192.168.1.1",
		"192.168.1.1:8443",
		"unifi.example.com",
		"unifi.local",
		"https://unifi.local", // scheme is tolerated and stripped
	} {
		c := config.Config{Host: host, APIKey: "key"}
		if err := c.Validate(); err != nil {
			t.Errorf("Validate(%q) should pass, got: %v", host, err)
		}
	}
}

func TestNormalizeHost(t *testing.T) {
	cases := map[string]string{
		"192.168.1.1":          "192.168.1.1",
		"https://unifi.local/": "unifi.local",
		"http://10.0.0.1:8443": "10.0.0.1:8443",
		"unifi.local/":         "unifi.local",
	}
	for in, want := range cases {
		got, err := config.NormalizeHost(in)
		if err != nil {
			t.Errorf("NormalizeHost(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidOutput(t *testing.T) {
	for _, s := range []string{"json", "table"} {
		if !config.ValidOutput(s) {
			t.Errorf("ValidOutput(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"xml", "csv", "yaml", "JSON", ""} {
		if config.ValidOutput(s) {
			t.Errorf("ValidOutput(%q) = true, want false", s)
		}
	}
}

func TestValidateCloud(t *testing.T) {
	if err := (config.Config{APIKey: "k"}).ValidateCloud(); err != nil {
		t.Errorf("APIKey present should validate, got %v", err)
	}
	err := (config.Config{}).ValidateCloud()
	if err == nil {
		t.Fatal("missing api key should fail ValidateCloud")
	}
	if !strings.Contains(err.Error(), "api-key") {
		t.Errorf("error %q should mention api-key", err)
	}
	// Host is NOT required/validated for cloud — even a host that Validate would
	// reject (contains a path) must not cause ValidateCloud to fail.
	if err := (config.Config{Host: "attacker.com/path", APIKey: "k"}).ValidateCloud(); err != nil {
		t.Errorf("cloud validation must not require/validate host, got %v", err)
	}
}

func TestInsecurePermsWarning(t *testing.T) {
	dir := t.TempDir()

	// 0600 file: no warning.
	good := filepath.Join(dir, "good.json")
	if err := config.Save(good, config.Config{Host: "h", APIKey: "k"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if w := config.InsecurePermsWarning(good); w != "" {
		t.Errorf("0600 file should not warn, got %q", w)
	}

	// 0644 file: warning.
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if w := config.InsecurePermsWarning(bad); w == "" {
		t.Error("0644 file should warn")
	}

	// Missing file: no warning.
	if w := config.InsecurePermsWarning(filepath.Join(dir, "nope.json")); w != "" {
		t.Errorf("missing file should not warn, got %q", w)
	}
}
