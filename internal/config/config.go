// Package config manages configuration loading, saving, and resolution for
// the unifi CLI. It supports environment variables, JSON config files, and
// explicit flag overrides via Merge.
//
// The output format is configurable via the UNIFI_OUTPUT environment variable,
// the "output" config-file field, and the -o/--output flag, honouring the
// precedence flag > env > file. When none is set the default is json.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultOutput is the output format used when none is configured via flag,
// environment, or config file.
const DefaultOutput = "json"

// Config holds the connection parameters for a UniFi controller.
type Config struct {
	Host     string `json:"host"`
	APIKey   string `json:"api_key"`
	Site     string `json:"site"`
	Insecure bool   `json:"insecure"`
	Output   string `json:"output,omitempty"`
}

// FromEnv builds a Config from environment variables:
//
//	UNIFI_HOST, UNIFI_API_KEY, UNIFI_SITE, UNIFI_INSECURE, UNIFI_OUTPUT.
//
// UNIFI_INSECURE is truthy when its lowercased, trimmed value is one of
// "1", "true", "yes", or "on".
func FromEnv() Config {
	c, _ := FromEnvSource()
	return c
}

// FromEnvSource builds a Config from environment variables and reports whether
// UNIFI_INSECURE was present in the environment. The presence flag lets callers
// honour flag > env > file precedence for the tri-state Insecure value: an
// explicitly-set UNIFI_INSECURE=false can override a file's insecure: true.
func FromEnvSource() (Config, bool) {
	insecureVal, insecureSet := os.LookupEnv("UNIFI_INSECURE")
	return Config{
		Host:     os.Getenv("UNIFI_HOST"),
		APIKey:   os.Getenv("UNIFI_API_KEY"),
		Site:     os.Getenv("UNIFI_SITE"),
		Insecure: parseBool(insecureVal),
		Output:   os.Getenv("UNIFI_OUTPUT"),
	}, insecureSet
}

// parseBool returns true when s (lowercased and trimmed) is one of the
// recognised truthy tokens.
func parseBool(s string) bool {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// Load reads and unmarshals the JSON config file at path. If the file does
// not exist, a zero Config and nil error are returned. Any other read or
// parse error is returned as-is.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}

	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Save writes c as pretty-printed JSON (2-space indent) to path with mode
// 0600. Parent directories are created with mode 0700 as needed.
func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("config: create parent dir: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	// Append a trailing newline for POSIX compliance.
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("config: write file: %w", err)
	}
	return nil
}

// DefaultPath returns the path to the user's config file. If the environment
// variable UNIFI_CONFIG is set and non-empty it is returned as-is. Otherwise
// the path is <userHomeDir>/.config/unifi/config.json. If os.UserHomeDir
// fails, the path is relative: .config/unifi/config.json.
func DefaultPath() string {
	if p := os.Getenv("UNIFI_CONFIG"); p != "" {
		return p
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "unifi", "config.json")
	}
	return filepath.Join(home, ".config", "unifi", "config.json")
}

// Merge returns base with any non-zero field of override applied on top.
// String fields from override win when non-empty; override.Insecure wins
// only when true (Insecure can only be turned on by an override, which is
// acceptable for our precedence model).
func Merge(base, override Config) Config {
	result := base
	if override.Host != "" {
		result.Host = override.Host
	}
	if override.APIKey != "" {
		result.APIKey = override.APIKey
	}
	if override.Site != "" {
		result.Site = override.Site
	}
	if override.Output != "" {
		result.Output = override.Output
	}
	if override.Insecure {
		result.Insecure = true
	}
	return result
}

// ResolveOutput returns the effective output format honouring the precedence
// flag > env > file: the first non-empty value of flag, env, file is returned,
// falling back to DefaultOutput when all are empty.
func ResolveOutput(file, env, flag string) string {
	if flag != "" {
		return flag
	}
	if env != "" {
		return env
	}
	if file != "" {
		return file
	}
	return DefaultOutput
}

// Resolve combines file, env, and flag configs honouring the precedence
// flag > env > file for every field. String fields use non-empty-wins (flag
// over env over file). Insecure is treated as a tri-state: the flag value wins
// when flagInsecureSet, otherwise the env value wins when envInsecureSet,
// otherwise the file value is used. Output always resolves to a concrete value
// (DefaultOutput, "json", when none is set).
func Resolve(file, env Config, envInsecureSet bool, flags Config, flagInsecureSet bool) Config {
	result := Merge(Merge(file, env), flags)

	switch {
	case flagInsecureSet:
		result.Insecure = flags.Insecure
	case envInsecureSet:
		result.Insecure = env.Insecure
	default:
		result.Insecure = file.Insecure
	}
	result.Output = ResolveOutput(file.Output, env.Output, flags.Output)
	return result
}

// Validate returns a descriptive error when required fields are missing or the
// host is malformed. Host and APIKey are both required; the host must be a bare
// hostname or host:port (an http(s):// scheme is tolerated) without any path.
func (c Config) Validate() error {
	if c.Host == "" {
		return errors.New(`host is required (set --host, UNIFI_HOST, or run ` + "`unifi configure`)")
	}
	if _, err := NormalizeHost(c.Host); err != nil {
		return err
	}
	if c.APIKey == "" {
		return errors.New(`api-key is required (set --api-key, UNIFI_API_KEY, or run ` + "`unifi configure`)")
	}
	return nil
}

// NormalizeHost validates and canonicalizes a controller host. It accepts a
// bare host or host:port, optionally prefixed with an http(s):// scheme (which
// is stripped), and rejects values containing a path, query, fragment, or
// whitespace. The returned value has no scheme and no trailing slash. It is the
// single source of truth used by both Validate and the HTTP client.
func NormalizeHost(h string) (string, error) {
	s := strings.TrimSpace(h)
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	s = strings.TrimRight(s, "/")
	if s == "" {
		return "", errors.New("host is empty")
	}
	if strings.ContainsAny(s, "/?# \t\r\n") {
		return "", fmt.Errorf("host %q must be a hostname or host:port without a path", h)
	}
	return s, nil
}

// ValidOutput reports whether s is a recognized output format.
func ValidOutput(s string) bool {
	switch s {
	case "json", "table":
		return true
	}
	return false
}

// InsecurePermsWarning returns a non-empty warning when the file at path exists
// with permission bits readable or writable by group or others (i.e. looser
// than 0600). It returns "" when the file is absent or adequately restricted,
// so callers can print the result unconditionally.
func InsecurePermsWarning(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Sprintf("config file %s has permissions %04o; recommended 0600", path, perm)
	}
	return ""
}
