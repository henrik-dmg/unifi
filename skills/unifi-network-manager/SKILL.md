---
name: unifi-network-manager
description: Use when the user wants to inspect or manage a UniFi / Ubiquiti network — listing sites, devices (access points, switches, gateways), WiFi clients, guest vouchers, restarting a device, cycling a PoE port, authorizing a guest, or configuring firewall zones/policies, DNS, VLANs, WLANs/SSIDs, ACL rules, port forwarding, traffic routes, RADIUS profiles, or WAN interfaces. Triggers on UniFi, Ubiquiti, access point, WiFi, PoE, firewall, VLAN, SSID, ACL, RADIUS.
---

# UniFi Network Manager (CLI)

## Overview

`unifi` is a CLI wrapping the UniFi Network Integration API
(`https://{host}/proxy/network/integration/v1`, `X-API-KEY` auth). Use it to
inspect and manage a local UniFi console. JSON is the default output — parse it
to answer.

## Safety (read first)

- **READ before WRITE** — inspect with list/get/stats before any mutation.
- **Every mutation requires `--yes`.** First state exactly what will happen and
  get the user's explicit confirmation in conversation (e.g. "This reboots AP
  <name> (id ...) — proceed?"), THEN run the command with `--yes`.
- You operate the user's real network. Never run a mutation the user did not
  approve, and keep actions scoped to the network they asked about. If a request
  is ambiguous or risky, stop and ask.

## Setup Check

1. Verify the tool: `command -v unifi`. If missing, tell the user to install it
   (repo has `make install`). Do NOT install it yourself without asking.
2. Test connectivity: `unifi info`.

If you see `host is required` or auth errors, config is missing. Precedence:
**flags > env (`UNIFI_HOST`, `UNIFI_API_KEY`, `UNIFI_SITE`, `UNIFI_INSECURE`) >
file** (`~/.config/unifi/config.json`). Have the user configure it themselves —
never ask for the API key in chat:

```
unifi configure --host <ip> --insecure   # set UNIFI_API_KEY in their env
```

Local consoles use self-signed certs, so `--insecure` is normally required.

## Workflow

- Discover the site first if unknown: `unifi sites list` → use the site `id` as
  `--site`. One site → use it (often named "default").
- Use `--all` for the complete list (handles pagination); else `--limit N`.

### Read commands

```
unifi info                                  # connectivity check
unifi sites list                            # get site id
unifi devices list --site <id>              # APs/switches/gateways
unifi devices get <deviceId> --site <id>
unifi devices stats <deviceId> --site <id>  # health telemetry
unifi clients list --site <id> --all        # connected devices
unifi clients get <clientId> --site <id>
unifi vouchers list --site <id>             # guest WiFi vouchers
```

`devices stats` is the only telemetry endpoint in the integration API.

### Mutations (confirm in conversation, THEN add `--yes`)

```
unifi devices restart <deviceId> --site <id> --yes          # reboots device
unifi devices port-cycle <deviceId> --port <N> --site <id> --yes  # power-cycles PoE port
unifi clients authorize <clientId> --site <id> --yes        # authorize a guest
unifi vouchers create --site <id> --count 5 --minutes 1440 --yes
unifi vouchers delete <voucherId> --site <id> --yes
```

Warn the user: **device restart** briefly drops all clients on that device;
**PoE port-cycle** reboots whatever device is powered on that port.

## Beyond the curated commands

For firewall, DNS, networks/VLANs, WLANs, ACLs, port forwarding, traffic routes,
RADIUS, WANs, VPN servers, or any other endpoint, the CLI reaches EVERY endpoint
via typed resource commands or the `api` passthrough. See `resources.md` in this
skill directory for the full path table and `api` usage.

## Troubleshooting

| Symptom | Cause / Fix |
|---|---|
| Exit code 2 | Bad command usage |
| Exit code 1 | Runtime / API error |
| `API error 401/403` | API key problem — re-run `unifi configure` |
| `API error 429 (retry after Ns)` | Rate limited — wait N seconds |
| x509 / certificate error | Add `--insecure` (or `UNIFI_INSECURE=true`) |
| `host is required` | Config missing — see Setup Check |
