# UniFi typed resources and `api` passthrough reference

Load this when the user wants something beyond the curated
sites/devices/clients/vouchers commands — firewall, DNS, networks/VLANs, WLANs,
ACLs, port forwarding, traffic routes, RADIUS, WANs, VPN servers, etc.

## `api` — universal passthrough for ANY endpoint

```
unifi api <METHOD> <path> [--data <json>] [--data-file <file>] [--query k=v]...
```

- `<METHOD>` ∈ GET/POST/PUT/PATCH/DELETE. Output is always JSON.
- A `{site}` (or `:site`) token in the path is auto-replaced with the resolved
  site, so you usually don't need `--site`.
- `--data-file -` reads the body from stdin. `--data` and `--data-file` are
  mutually exclusive.
- Non-GET requires `--yes` (see safety in SKILL.md).

```
unifi api GET /sites/{site}/firewall/policies
unifi api GET /sites/{site}/clients --query limit=50
unifi api POST /sites/{site}/networks --data '{"name":"IoT","vlan":40}' --yes
unifi api DELETE /sites/{site}/port-forwards/<id> --yes
```

## Typed resource commands (prefer when available)

Forms: `unifi <group> list` · `get <id>` · `create --data '<json>'` ·
`update <id> --data '<json>'` · `delete <id>`.
For firewall the sub comes first: `unifi firewall <zones|policies> <action> [id]`.

Paths verified against UniFi Network application v10.4.57.

| Group | Path | Ops |
|---|---|---|
| `networks` | `/sites/{site}/networks` | list/get/create/update/delete |
| `firewall zones` | `/sites/{site}/firewall/zones` | list/get/create/update/delete |
| `firewall policies` | `/sites/{site}/firewall/policies` | list/get/create/update/delete |
| `acl-rules` | `/sites/{site}/acl-rules` | list/get/create/update/delete |
| `dns` | `/sites/{site}/dns/policies` | list/get/create/update/delete |
| `traffic-lists` | `/sites/{site}/traffic-matching-lists` | list/get/create/update/delete |
| `wans` | `/sites/{site}/wans` | list/get/create/update/delete |
| `vpn-servers` | `/sites/{site}/vpn/servers` | list/get/create/update/delete |
| `radius-profiles` | `/sites/{site}/radius/profiles` | list/get/create/update/delete |
| `device-tags` | `/sites/{site}/device-tags` | list/get/create/update/delete |
| `countries` | `/countries` (not site-scoped) | list |

No typed command on v10.4.57 (use the `api` passthrough): `wlans` (WiFi/SSIDs) →
`unifi api GET /sites/{site}/wlans`; `port-forwards`; `traffic-routes`.

Paths can vary by firmware. The console's **Settings → Integrations** is the
source of truth; if a typed path is wrong, fall back to `unifi api <METHOD> <path>`.

## Reading device stats

`unifi devices stats <id>` returns `uptimeSec`, `cpuUtilizationPct`,
`memoryUtilizationPct`, `loadAverage{1,5,15}Min`, and `uplink.txRateBps`/`rxRateBps`.
High memory (>90%) or load rising across the 1/5/15-min averages indicates a
struggling device. `-o table` renders a readable summary for humans.
