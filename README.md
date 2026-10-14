# lantern-headless-iran

> **Unofficial fork of [`getlantern/lantern-headless-client`](https://github.com/getlantern/lantern-headless-client). Not affiliated with Lantern.**

**[راهنمای کامل فارسی](./README-fa.md)** · [What changed](#what-this-fork-changes) · [Quick start](#quick-start)

A headless Lantern client tuned for networks with **hard filtering** (tested in Iran), where:

- the IPs of Lantern's own proxy servers are usually **blocked**, and
- local DNS is **poisoned** (public domains resolve to private IPs such as `10.10.34.36`).

The client exposes local **HTTP** and **SOCKS5** ports; the browser (or any app) is pointed at them.

## What this fork changes

| Change | Why it matters here |
|---|---|
| **Connections to Lantern's servers can go through `--upstream-proxy`** | Only that short-lived TCP connection travels through your healthy proxy (Tor, Psiphon, a Cloudflare relay, …), while user traffic still runs through Lantern's own transport and **exits from the Lantern network**. This is the difference between "the server IP is blocked" and "the proxy does not work". |
| **Last-resort data path** | If no Lantern server is reachable at all, browser traffic falls back to the upstream proxy so the local ports keep answering instead of resetting every connection. |
| **Poisoned-DNS handling** | When local DNS answers with a private IP, the client keeps the hostname so it is resolved remotely by the proxy server instead of dialing an unreachable address. |
| **Clear startup diagnostics** | The client verifies the *data path* with a real request through its own proxy and reports `Data path OK` / `Data path is NOT working` (plus the egress IP), instead of only saying the ports are open. |
| **Domain fronting is always available** and never silently degrades, with `front scan` / `front import` / `front test` subcommands: the built-in IP scanner finds working IP/SNI pairs, `import` turns them into a local fronting config, `test` verifies it end-to-end. | Config fetching must survive when every Lantern domain is blocked. |
| **`--upstream-proxy` also serves the control plane** (config + auth) | One flag covers config fetch, server dialing and the data-path fallback. |
| **Server list with locations, and server pinning** | `servers` lists every server in the current config with its country and city; `start --server <name>` pins the connection to one server and ignores all the others. |
| **`update` refreshes the server list on demand, over the transport you pick** | `lantern update --via fronting` (default `auto` tries fronting → upstream → direct) fetches a fresh server list with the built-in IP scanner keeping fronting alive, and saves it to `user.conf` so `start` uses it immediately. |

## Requirements

- Go 1.24 or newer
- Reachability to the Go module proxy (if `proxy.golang.org` is blocked: `go env -w GOPROXY=https://proxy.golang.org,https://goproxy.cn,direct`)

## Build

```shell
go build -o bin/lantern .
./bin/lantern --help
```

## Quick start

```shell
./bin/lantern --data-path data --upstream-proxy socks5://127.0.0.1:1080 start \
  --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

`--upstream-proxy` accepts `socks5://`, `http://` or `https://` and should point at a proxy that
already works on your network (Tor, Psiphon, …). Then point the browser at the ports above.

**Firefox** (recommended, because local DNS is poisoned):
`Settings → General → Network Settings → Manual proxy configuration` → SOCKS Host `127.0.0.1`,
port `18071`, type **SOCKS v5**, and tick **`Proxy DNS when using SOCKS5`**.

**Chrome/Chromium**: `google-chrome --proxy-server="socks5://127.0.0.1:18071"`, or set the system
HTTP proxy to `127.0.0.1:18070`.

> Without a proxy configured in the browser, traffic stays direct and will not get through a
> filtered network. This client is headless: it never takes over system traffic by itself.

## What the startup messages mean

```
SUCCESS  Control plane reachable (config fetched over domain fronting)
INFO     Local proxy is listening — SOCKS5: 127.0.0.1:18071  HTTP: 127.0.0.1:18070
INFO     Verifying the data path with a real request through that proxy...
SUCCESS  Data path OK — HTTP 204 from https://www.google.com/generate_204 in 803ms
INFO     Traffic leaves this machine through IP 31.171.100.110
```

- **Control plane reachable** — the config was fetched. It says nothing about browser traffic and
  it also appears without `--upstream-proxy`.
- **Local proxy is listening** — the ports are bound (listening ≠ working).
- **Data path OK / NOT working** — the only line that reflects what the browser will experience:
  a real request is sent through the local proxy (2 endpoints × 3 attempts). On failure the client
  prints a `Hint:` telling you whether `--upstream-proxy` is missing or whether the upstream itself
  is unreachable.
- **Traffic leaves this machine through IP …** — the public IP the destination sees, i.e. whether
  traffic actually exits through a proxy.

## Listing servers and pinning to one

`servers` shows every server in the current config with its geographic location (as reported by
Lantern's own config). Without `--refresh` it reads the cached list from `<data-path>/user.conf`
(written by any successful `start`); `--refresh` fetches a fresh list through the control plane
using the same flags as `start` (`--upstream-proxy`, fronting config, …):

```shell
./bin/lantern --data-path data servers --refresh
./bin/lantern --data-path data servers --json   # machine-readable
./bin/lantern --data-path data servers --check  # probe each server: open or IP-blocked
```

With `--check` every server is TCP-probed in parallel and the table gains a `Reachable` column
(`open` / `unreachable`), plus a count. An `unreachable` server is almost certainly IP-blocked
from your network — it may still work through `--upstream-proxy`.

```
Name              | Address          | Protocol | Country       | City
flaky-frankfurt-01| 198.51.100.10:443| https    | DE Germany    | Frankfurt
sunny-tokyo-02    | 203.0.113.10:443 | https    | JP Japan      | Tokyo
```

To force the client onto one specific server, pass the name to `start` (matching is
case-insensitive; all other servers are removed from the config before any dialer is built):

```shell
./bin/lantern --data-path data start --server sunny-tokyo-02 \
  --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

The client logs `Using pinned server "sunny-tokyo-02" (Tokyo, Japan)` once the server appears in
the config, and warns with the list of available names if it does not. Note the trade-off: with
pinning there is **no failover** — if that server is blocked or dies, the connection fails until
you pick another one or drop the flag.

## Updating the server list and scanning for fronting IPs

`update` tells the client to refresh its server list right now — pick the transport yourself:

```shell
./bin/lantern --data-path data update --via fronting   # over domain fronting (default)
./bin/lantern --data-path data update --via upstream   # through --upstream-proxy
./bin/lantern --data-path data update --via direct     # direct connection
./bin/lantern --data-path data update                  # auto: fronting → upstream → direct
./bin/lantern --data-path data update --json           # machine-readable
./bin/lantern --data-path data update --check          # also probe which servers are reachable
```

The fetched list is saved to `<data-path>/user.conf`, so a following `start` (or `servers` without
`--refresh`) uses it immediately — even if the config service stays unreachable afterwards.

Fronting endpoints decay: CDN edges rotate and filtering blocks working IP/SNI pairs over time.
`update --via fronting` (and the fronting leg of `auto`) is therefore **self-healing**: if the
current fronting config no longer works, it runs a quick built-in scan (sampled to
`--scan-ips 15000` IPs), keeps the `--scan-top 20` fastest pairs, writes them to
`<data-path>/fronted-local.yaml.gz` and retries through the fresh endpoints. `--rescan` forces
that refresh even when the current config still works; `--no-scan` disables the fallback entirely
(e.g. for fast scripted runs).

When fronting stops working (the CDN IPs your fronting config points at die), the built-in IP
scanner finds fresh ones — no external scanner needed:

```shell
./bin/lantern --data-path data front scan --build
```

This walks the built-in Akamai ranges (sampled down to `--max-ips 60000` IPs), TCP-probes port
443, TLS-handshakes each live IP with the provider's masquerade domains plus alternate Akamai
shared hostnames (`a.akamaized.net`, `a.akamaihd.net` — so one SNI-blocked hostname doesn't
starve the scan), vets the surviving
IP/SNI pairs end-to-end, and with `--build` writes the result straight to
`<data-path>/fronted-local.yaml.gz`. Narrow the scan or target your own ranges with `--ranges
23.32.0.0/11,104.64.0.0/10` or `--cidrs ranges.txt`; `--sni` / `--snis` override the SNI candidates.
Without `--build` the pairs land in `<data-path>/scan-pairs.json`, ready for
`front import --pairs`. Run `front test` afterwards to verify fronting end-to-end, then
`update --via fronting`.

## Useful flags

| Flag | Meaning |
|---|---|
| `--upstream-proxy` | `socks5://` / `http://` / `https://` proxy used for config fetch, for reaching Lantern's (possibly IP-blocked) servers, and as the last-resort data path |
| `--data-path` | Where config, logs and the local fronting config live |
| `--fronted-config` | Explicit path to a local `fronted.yaml.gz` (auto-detected otherwise) |
| `--with-proxyless` / `--enable-dnstt` | Enable smart dialing / the DNS tunnel (off by default) |
| `--server <name>` | Pin `start` to a single server by name (see the `servers` command) |
| `update [--via fronting\|upstream\|direct\|auto] [--json] [--check]` | Refresh the cached server list over the chosen transport; auto-scans fresh fronting endpoints when fronting fails (`--rescan` / `--no-scan` / `--scan-ips` / `--scan-top` control that); `--check` probes which servers are reachable |
| `servers [--refresh] [--json] [--check]` | List Lantern servers and their country/city; `--check` adds per-server reachability |
| `front scan` | Built-in IP scanner: find working fronting endpoints (IP/SNI pairs); `--build` writes the fronting config |
| `front import` / `front test` | Build a fronting config from scanned pairs / verify fronting end-to-end |

Run `./bin/lantern --help` for the full list.

## Documentation

The **[Persian guide](./README-fa.md)** covers the full workflow: scanning IP ranges for
domain-fronting endpoints, building and testing the fronting config, refreshing the server list
with `update`, diagnosing dead servers, and the meaning of every log line (`i/o timeout`,
`Fell back to the upstream proxy`, `keeping the hostname …`).

## Credits and license

Based on the [Lantern headless client](https://github.com/getlantern/lantern-headless-client) by
The Lantern Team. Changes in this fork are released under the same
[Apache License 2.0](./LICENSE).

Lantern is a trademark of its owners; this project is an independent, unofficial fork.
