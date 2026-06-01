package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
)

// queryFlag is a repeatable flag.Value that accumulates "k=v" tokens.
type queryFlag []string

func (q *queryFlag) String() string { return strings.Join(*q, ",") }
func (q *queryFlag) Set(v string) error {
	*q = append(*q, v)
	return nil
}

// dataFlags holds the shared --data / --data-file flag values used by both the
// api command and the resource registry.
type dataFlags struct {
	data     string
	dataFile string
}

// register binds --data and --data-file onto fs.
func (d *dataFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&d.data, "data", "", "inline JSON request body")
	fs.StringVar(&d.dataFile, "data-file", "", "read JSON request body from file (- for stdin)")
}

// body resolves the request body bytes from the flags. It returns
// (nil, false, nil) when no body was supplied, validates that any supplied body
// is valid JSON, and treats "-" as stdin for --data-file. data and data-file
// are mutually exclusive.
func (d *dataFlags) body(stdin io.Reader) (json.RawMessage, bool, error) {
	if d.data != "" && d.dataFile != "" {
		return nil, false, fmt.Errorf("--data and --data-file are mutually exclusive")
	}
	var raw []byte
	switch {
	case d.data != "":
		raw = []byte(d.data)
	case d.dataFile != "":
		if d.dataFile == "-" {
			b, err := io.ReadAll(io.LimitReader(stdin, maxStdinBytes+1))
			if err != nil {
				return nil, false, fmt.Errorf("read stdin: %w", err)
			}
			if int64(len(b)) > maxStdinBytes {
				return nil, false, fmt.Errorf("stdin body too large (limit %d bytes)", maxStdinBytes)
			}
			raw = b
		} else {
			b, err := os.ReadFile(d.dataFile)
			if err != nil {
				return nil, false, fmt.Errorf("read data file: %w", err)
			}
			raw = b
		}
	default:
		return nil, false, nil
	}
	if !json.Valid(raw) {
		return nil, false, fmt.Errorf("request body is not valid JSON")
	}
	return json.RawMessage(raw), true, nil
}

// stdinReader returns the input used for "--data-file -". It is a package
// variable so tests can inject a reader.
var stdinReader = func() io.Reader { return os.Stdin }

// maxStdinBytes caps how much data "--data-file -" will read, guarding against
// an unbounded pipe exhausting memory. It is a variable so tests can shrink it.
var maxStdinBytes int64 = 16 << 20 // 16 MiB

// validMethods are the HTTP methods accepted by the api passthrough.
var validMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
}

// cmdAPI implements `unifi api <METHOD> <path> [flags]`. method is the action
// token from splitTokens; positionals[0] is the path.
func cmdAPI(method string, positionals, args []string, stdout, stderr io.Writer) int {
	if method == "" || len(positionals) < 1 {
		fmt.Fprintln(stderr, "unifi: usage: api <METHOD> <path> [--data <json>] [--data-file <file>] [--query k=v]...")
		return 2
	}
	method = strings.ToUpper(method)
	if !validMethods[method] {
		fmt.Fprintf(stderr, "unifi: invalid method %q (want GET, POST, PUT, PATCH, or DELETE)\n", method)
		return 2
	}
	path := positionals[0]

	var g globals
	fs := newFlagSet("api", &g, stderr)
	var df dataFlags
	df.register(fs)
	var queries queryFlag
	fs.Var(&queries, "query", "repeatable query parameter k=v")
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
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

	if method != "GET" {
		if !g.mutationAllowed(stderr) {
			return 1
		}
	}

	api, eff, ok := buildAPI(g, stderr)
	if !ok {
		return 1
	}

	if pathHasSiteToken(path) {
		site, err := resolveSite(eff)
		if err != nil {
			fmt.Fprintf(stderr, "unifi: %v\n", err)
			return 1
		}
		path = substituteSite(path, site)
	}

	var reqBody any
	if hasBody {
		reqBody = body
	}

	ctx, cancel := ctxWithTimeout()
	defer cancel()
	raw, err := api.Do(ctx, method, path, query, reqBody)
	if err != nil {
		return renderError(stderr, err)
	}
	if err := printJSON(stdout, raw); err != nil {
		return renderError(stderr, err)
	}
	return 0
}
