# Contributing to unifi

Thanks for your interest in improving `unifi`! This is a small, dependency-free
Go CLI, and contributions are welcome.

## Development setup

You need **Go 1.23+**. Clone the repo and build:

```sh
make build        # builds ./unifi
make test         # runs the full test suite (with the race detector)
make vet          # go vet ./...
make fmt          # gofmt -w .
make all          # fmt, vet, test, then build
```

The project uses only the Go standard library — there are no third-party
dependencies and no `go.sum`. Please keep it that way unless there is a strong
reason to add a dependency; raise it in an issue first.

## Pull requests

- **Branch** from `main` using `feature/<short-desc>` or `fix/<short-desc>`.
- **Tests first.** This project is developed test-first. Every behavior change
  needs a test that fails before your change and passes after. Run `make test`
  before pushing — CI runs `gofmt -l`, `go vet`, and `go test -race`.
- **Keep commits focused** with one-line, imperative-mood subjects
  ("Add X", not "Added X").
- **Match the surrounding style.** Idiomatic Go, errors wrapped with `%w`,
  no secrets in logs, and `url.PathEscape` for any user-supplied path segment.

## Reporting bugs / requesting features

Open a GitHub issue. For bugs, include the command you ran (with secrets
redacted), what you expected, what happened, and your UniFi Network / UniFi OS
versions.

## Security

Please do **not** open a public issue for security vulnerabilities. See
[SECURITY.md](SECURITY.md) for how to report them privately.
