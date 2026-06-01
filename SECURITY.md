# Security Policy

## Reporting a vulnerability

Please report security issues privately rather than opening a public issue.
Use GitHub's [private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)
on this repository, and you'll get a response as quickly as possible.

## Credential handling

`unifi` talks to your UniFi controller with an API key. To keep it safe:

- **Prefer `UNIFI_API_KEY` or `unifi configure`** over passing `--api-key` on the
  command line. Command-line arguments are visible to other local users via the
  process table (`ps`) and are written to your shell history.
- The config file (`~/.config/unifi/config.json`) is written with `0600`
  permissions and holds your API key in plaintext. Do not commit it or relax its
  permissions. `unifi` warns if it finds the file with looser permissions.
- TLS certificate verification is **on by default**. Only use `--insecure`
  (or `UNIFI_INSECURE=true`) for local consoles with self-signed certificates,
  and only when you trust the network path to the controller.
- `--data-file` reads from the local filesystem with your full permissions; do
  not point it at untrusted input in automated pipelines.
