package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/colindickson/unifi/internal/client"
	"github.com/colindickson/unifi/internal/config"
	"github.com/colindickson/unifi/internal/smclient"
)

// SMAPI is the subset of the Site Manager client used by the CLI, behind an
// interface so tests can inject a fake. *smclient.Client satisfies it.
type SMAPI interface {
	Hosts(ctx context.Context, all bool, limit int) ([]json.RawMessage, error)
	Host(ctx context.Context, id string) (json.RawMessage, error)
	Sites(ctx context.Context, all bool, limit int) ([]json.RawMessage, error)
	Devices(ctx context.Context, hostIDs []string, all bool, limit int) ([]json.RawMessage, error)
	ISPMetrics(ctx context.Context, mtype string, q url.Values) (json.RawMessage, error)
	QueryISPMetrics(ctx context.Context, mtype string, body json.RawMessage) (json.RawMessage, error)
	SDWANConfigs(ctx context.Context, all bool, limit int) ([]json.RawMessage, error)
	SDWANConfig(ctx context.Context, id string) (json.RawMessage, error)
	SDWANStatus(ctx context.Context, id string) (json.RawMessage, error)
	Do(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error)
	ListPath(ctx context.Context, path string, all bool, limit int) ([]json.RawMessage, error)
}

// newSMAPI constructs the SMAPI implementation; a package var so tests can fake it.
var newSMAPI = func(cfg config.Config) SMAPI { return smclient.New(cfg) }

// compile-time check.
var _ SMAPI = (*smclient.Client)(nil)

// buildSMAPI validates the resolved config for cloud use (API key only) and
// constructs the SMAPI.
func buildSMAPI(g globals, stderr io.Writer) (SMAPI, config.Config, bool) {
	eff := g.resolved
	if err := eff.ValidateCloud(); err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return nil, eff, false
	}
	return newSMAPI(eff), eff, true
}

// renderSMError renders a cloud error, adding a key hint on 401/403 (the cloud
// key differs from the local console key), else delegates to renderError.
func renderSMError(stderr io.Writer, err error) int {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && (apiErr.StatusCode == 401 || apiErr.StatusCode == 403) {
		fmt.Fprintf(stderr, "unifi: API error %d: %s — check your Site Manager API key "+
			"(create one at unifi.ui.com → Settings → API Keys; it is separate from the local console key)\n",
			apiErr.StatusCode, apiErr.Message)
		return 1
	}
	return renderError(stderr, err)
}

// cmdSiteManager routes `unifi site-manager <sub> <action> [args]`. sub is the
// subgroup token (hosts/sites/devices/isp-metrics/sdwan/api); positionals are
// the tokens after it.
func cmdSiteManager(sub string, positionals, args []string, stdout, stderr io.Writer) int {
	switch sub {
	case "hosts":
		return smHosts(positionals, args, stdout, stderr)
	case "sites":
		return smSites(positionals, args, stdout, stderr)
	case "devices":
		return smDevices(positionals, args, stdout, stderr)
	case "isp-metrics":
		return smISPMetrics(positionals, args, stdout, stderr)
	case "sdwan":
		return smSDWAN(positionals, args, stdout, stderr)
	case "api":
		return smAPI(positionals, args, stdout, stderr)
	case "", "help", "-h", "--help":
		printSiteManagerUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unifi: unknown site-manager subcommand %q\n", sub)
		printSiteManagerUsage(stderr)
		return 2
	}
}

// printSiteManagerUsage writes the site-manager group help.
func printSiteManagerUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: unifi site-manager (or: sm) <subcommand> [args]

Account-wide UniFi Site Manager cloud API (https://api.ui.com). Read-only.
Requires only an API key (create at unifi.ui.com → Settings → API Keys).

Subcommands:
  hosts list                     List UniFi OS consoles on the account
  hosts get <hostId>             Show a host
  sites list                     List sites across all hosts
  devices list [--host-id <id>]  List devices (repeat --host-id to filter)
  isp-metrics get <5m|1h>        ISP metrics (--duration 24h|7d|30d | --begin <ts> --end <ts>)
  isp-metrics query <5m|1h>      Filtered ISP query (--data <json> | --data-file <file>)
  sdwan list                     List SD-WAN configurations
  sdwan get <configId>           Show an SD-WAN config
  sdwan status <configId>        Show SD-WAN deployment status
  api <METHOD> <path>            Call any cloud endpoint (--data/--data-file/--query)

Flags: --all, --limit N, -o/--output json|table.
`)
}

// ---- shared helpers ----

// smPrintList runs a cloud list call and prints JSON (default) or a table built
// by the given row renderer.
func smPrintList(stdout, stderr io.Writer, g globals, items []json.RawMessage, header string, row func(*tabwriter.Writer, map[string]any)) int {
	if g.isJSON() {
		if err := printJSONList(stdout, items); err != nil {
			return renderError(stderr, err)
		}
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, header)
	for _, raw := range items {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		row(tw, m)
	}
	if err := tw.Flush(); err != nil {
		return renderError(stderr, err)
	}
	return 0
}

// firstField returns the first present, non-empty string field among keys.
func firstField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v := strField(m, k); v != "" {
			return v
		}
	}
	return ""
}

// ---- hosts ----

func smHosts(positionals, args []string, stdout, stderr io.Writer) int {
	action := "list"
	if len(positionals) > 0 {
		action = positionals[0]
	}
	switch action {
	case "list":
		var g globals
		fs := newFlagSet("site-manager hosts list", &g, stderr)
		if !parseFlags(fs, &g, args, stderr) {
			return 1
		}
		api, _, ok := buildSMAPI(g, stderr)
		if !ok {
			return 1
		}
		ctx, cancel := ctxWithTimeout()
		defer cancel()
		items, err := api.Hosts(ctx, g.all, g.limit)
		if err != nil {
			return renderSMError(stderr, err)
		}
		return smPrintList(stdout, stderr, g, items, "ID\tNAME\tIP", func(tw *tabwriter.Writer, m map[string]any) {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", strField(m, "id"),
				firstField(m, "name", "hostname"), firstField(m, "ipAddress", "ip"))
		})
	case "get":
		if len(positionals) < 2 {
			fmt.Fprintln(stderr, "unifi: site-manager hosts get requires a host id")
			return 2
		}
		id := positionals[1]
		var g globals
		fs := newFlagSet("site-manager hosts get", &g, stderr)
		if !parseFlags(fs, &g, args, stderr) {
			return 1
		}
		api, _, ok := buildSMAPI(g, stderr)
		if !ok {
			return 1
		}
		ctx, cancel := ctxWithTimeout()
		defer cancel()
		raw, err := api.Host(ctx, id)
		if err != nil {
			return renderSMError(stderr, err)
		}
		if err := printJSON(stdout, raw); err != nil {
			return renderError(stderr, err)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unifi: unknown site-manager hosts action %q\n", action)
		return 2
	}
}

// ---- sites ----

func smSites(positionals, args []string, stdout, stderr io.Writer) int {
	if len(positionals) > 0 && positionals[0] != "list" {
		fmt.Fprintf(stderr, "unifi: unknown site-manager sites action %q\n", positionals[0])
		return 2
	}
	var g globals
	fs := newFlagSet("site-manager sites list", &g, stderr)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	api, _, ok := buildSMAPI(g, stderr)
	if !ok {
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	items, err := api.Sites(ctx, g.all, g.limit)
	if err != nil {
		return renderSMError(stderr, err)
	}
	return smPrintList(stdout, stderr, g, items, "ID\tNAME", func(tw *tabwriter.Writer, m map[string]any) {
		fmt.Fprintf(tw, "%s\t%s\n", strField(m, "id"), firstField(m, "name", "desc"))
	})
}

// ---- devices ----

func smDevices(positionals, args []string, stdout, stderr io.Writer) int {
	if len(positionals) > 0 && positionals[0] != "list" {
		fmt.Fprintf(stderr, "unifi: unknown site-manager devices action %q\n", positionals[0])
		return 2
	}
	var g globals
	fs := newFlagSet("site-manager devices list", &g, stderr)
	var hostIDs queryFlag
	fs.Var(&hostIDs, "host-id", "filter by host id (repeatable)")
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	api, _, ok := buildSMAPI(g, stderr)
	if !ok {
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	items, err := api.Devices(ctx, []string(hostIDs), g.all, g.limit)
	if err != nil {
		return renderSMError(stderr, err)
	}
	return smPrintList(stdout, stderr, g, items, "ID\tNAME\tMODEL\tSTATUS", func(tw *tabwriter.Writer, m map[string]any) {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", strField(m, "id"),
			firstField(m, "name", "hostname"), firstField(m, "model", "shortname"),
			firstField(m, "status", "state"))
	})
}

// ---- isp-metrics ----

func smISPMetrics(positionals, args []string, stdout, stderr io.Writer) int {
	if len(positionals) < 1 {
		fmt.Fprintln(stderr, "unifi: site-manager isp-metrics requires an action (get or query)")
		return 2
	}
	action := positionals[0]
	switch action {
	case "get":
		if len(positionals) < 2 {
			fmt.Fprintln(stderr, "unifi: site-manager isp-metrics get requires a type (5m or 1h)")
			return 2
		}
		mtype := positionals[1]
		var g globals
		fs := newFlagSet("site-manager isp-metrics get", &g, stderr)
		var duration, begin, end string
		fs.StringVar(&duration, "duration", "", "24h|7d|30d")
		fs.StringVar(&begin, "begin", "", "begin timestamp (RFC3339)")
		fs.StringVar(&end, "end", "", "end timestamp (RFC3339)")
		if !parseFlags(fs, &g, args, stderr) {
			return 1
		}
		api, _, ok := buildSMAPI(g, stderr)
		if !ok {
			return 1
		}
		q := url.Values{}
		if duration != "" {
			q.Set("duration", duration)
		}
		if begin != "" {
			q.Set("beginTimestamp", begin)
		}
		if end != "" {
			q.Set("endTimestamp", end)
		}
		ctx, cancel := ctxWithTimeout()
		defer cancel()
		raw, err := api.ISPMetrics(ctx, mtype, q)
		if err != nil {
			return renderSMError(stderr, err)
		}
		if err := printJSON(stdout, raw); err != nil {
			return renderError(stderr, err)
		}
		return 0
	case "query":
		if len(positionals) < 2 {
			fmt.Fprintln(stderr, "unifi: site-manager isp-metrics query requires a type (5m or 1h)")
			return 2
		}
		mtype := positionals[1]
		var g globals
		fs := newFlagSet("site-manager isp-metrics query", &g, stderr)
		var df dataFlags
		df.register(fs)
		if !parseFlags(fs, &g, args, stderr) {
			return 1
		}
		body, hasBody, err := df.body(stdinReader())
		if err != nil {
			fmt.Fprintf(stderr, "unifi: %v\n", err)
			return 1
		}
		if !hasBody {
			fmt.Fprintln(stderr, "unifi: site-manager isp-metrics query requires --data or --data-file")
			return 1
		}
		api, _, ok := buildSMAPI(g, stderr)
		if !ok {
			return 1
		}
		ctx, cancel := ctxWithTimeout()
		defer cancel()
		raw, err := api.QueryISPMetrics(ctx, mtype, body)
		if err != nil {
			return renderSMError(stderr, err)
		}
		if err := printJSON(stdout, raw); err != nil {
			return renderError(stderr, err)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unifi: unknown site-manager isp-metrics action %q (want get or query)\n", action)
		return 2
	}
}

// ---- sdwan ----

func smSDWAN(positionals, args []string, stdout, stderr io.Writer) int {
	action := "list"
	if len(positionals) > 0 {
		action = positionals[0]
	}
	switch action {
	case "list":
		var g globals
		fs := newFlagSet("site-manager sdwan list", &g, stderr)
		if !parseFlags(fs, &g, args, stderr) {
			return 1
		}
		api, _, ok := buildSMAPI(g, stderr)
		if !ok {
			return 1
		}
		ctx, cancel := ctxWithTimeout()
		defer cancel()
		items, err := api.SDWANConfigs(ctx, g.all, g.limit)
		if err != nil {
			return renderSMError(stderr, err)
		}
		return smPrintList(stdout, stderr, g, items, "ID\tNAME\tSTATUS", func(tw *tabwriter.Writer, m map[string]any) {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", strField(m, "id"),
				firstField(m, "name"), firstField(m, "status", "state"))
		})
	case "get", "status":
		if len(positionals) < 2 {
			fmt.Fprintf(stderr, "unifi: site-manager sdwan %s requires a config id\n", action)
			return 2
		}
		id := positionals[1]
		var g globals
		fs := newFlagSet("site-manager sdwan "+action, &g, stderr)
		if !parseFlags(fs, &g, args, stderr) {
			return 1
		}
		api, _, ok := buildSMAPI(g, stderr)
		if !ok {
			return 1
		}
		ctx, cancel := ctxWithTimeout()
		defer cancel()
		var raw json.RawMessage
		var err error
		if action == "status" {
			raw, err = api.SDWANStatus(ctx, id)
		} else {
			raw, err = api.SDWANConfig(ctx, id)
		}
		if err != nil {
			return renderSMError(stderr, err)
		}
		if err := printJSON(stdout, raw); err != nil {
			return renderError(stderr, err)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unifi: unknown site-manager sdwan action %q\n", action)
		return 2
	}
}

// ---- cloud api passthrough ----

func smAPI(positionals, args []string, stdout, stderr io.Writer) int {
	if len(positionals) < 2 {
		fmt.Fprintln(stderr, "unifi: usage: site-manager api <METHOD> <path> [--data <json>] [--data-file <file>] [--query k=v]...")
		return 2
	}
	method := strings.ToUpper(positionals[0])
	if !validMethods[method] {
		fmt.Fprintf(stderr, "unifi: invalid method %q (want GET, POST, PUT, PATCH, or DELETE)\n", method)
		return 2
	}
	path := positionals[1]
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	var g globals
	fs := newFlagSet("site-manager api", &g, stderr)
	var df dataFlags
	df.register(fs)
	var queries queryFlag
	fs.Var(&queries, "query", "repeatable query parameter k=v")
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}

	body, hasBody, err := df.body(stdinReader())
	if err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return 1
	}

	query := url.Values{}
	for _, kv := range queries {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			fmt.Fprintf(stderr, "unifi: invalid --query %q (want k=v)\n", kv)
			return 1
		}
		query.Add(k, v)
	}
	if len(query) == 0 {
		query = nil
	}

	api, _, ok := buildSMAPI(g, stderr)
	if !ok {
		return 1
	}
	var reqBody any
	if hasBody {
		reqBody = body
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	raw, err := api.Do(ctx, method, path, query, reqBody)
	if err != nil {
		return renderSMError(stderr, err)
	}
	if err := printJSON(stdout, raw); err != nil {
		return renderError(stderr, err)
	}
	return 0
}
