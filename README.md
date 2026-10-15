# lantern-headless-iran

**English** · [فارسی](./README-fa.md) · [Русский](./README-ru.md)

> **Unofficial** fork of [`getlantern/lantern-headless-client`](https://github.com/getlantern/lantern-headless-client). Not affiliated with Lantern.

A Lantern client **without a graphical interface** (headless), made for networks with heavy
filtering (tested in Iran). It opens two local ports, and your browser is pointed at them —
your traffic then leaves through the Lantern network.

## Download

Binaries are compiled **automatically by GitHub Actions** and published on the
[Releases](https://github.com/FossilizedProgrammer/lantern-headless-iran/releases) page.
Nothing is uploaded from anyone's computer:

| File | Platform |
|---|---|
| `lantern-headless-linux-amd64` | Linux, x86_64 |
| `lantern-headless-windows-amd64.exe` | Windows, 64-bit |

How it works: every push to `main` builds both binaries (to keep the build green), and every
version tag `v*` additionally creates a **Release** containing those same two files.

## Quick start

1. Download the binary for your system from **Releases**.
2. Run it. The example below assumes you already have a working local proxy (Tor, Psiphon, …)
   on port `1080`:

**Linux**

```shell
chmod +x lantern-headless-linux-amd64
./lantern-headless-linux-amd64 --data-path ./data --upstream-proxy socks5://127.0.0.1:1080 \
  start --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

**Windows (Command Prompt)**

```bat
lantern-headless-windows-amd64.exe --data-path data --upstream-proxy socks5://127.0.0.1:1080 start --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

`--upstream-proxy` is optional but strongly recommended on filtered networks. It accepts
`socks5://`, `http://` or `https://` and is used for everything at once: fetching the config,
opening the connection to Lantern's own servers (their IPs are often blocked), and as a
last-resort data path.

3. Point your browser at the local ports:

**Firefox** (recommended — local DNS is usually poisoned):
`Settings → General → Network Settings → Manual proxy configuration` → SOCKS Host `127.0.0.1`,
port `18071`, type **SOCKS v5**, tick **"Proxy DNS when using SOCKS5"**.

**Chrome/Chromium**

```shell
google-chrome --proxy-server="socks5://127.0.0.1:18071"
```

> The client is headless: it never changes system settings by itself. If the browser is not
> configured, traffic stays direct and will not pass through the filter.

## What the startup messages mean

```
SUCCESS  Control plane reachable (config fetched successfully)
INFO     Local proxy is listening — SOCKS5: 127.0.0.1:18071  HTTP: 127.0.0.1:18070
SUCCESS  Data path OK — HTTP 204 from https://www.google.com/generate_204 in 803ms
INFO     Traffic leaves this machine through IP 31.171.100.110
```

- **Control plane reachable** — the config was downloaded. It says nothing about browser traffic.
- **Local proxy is listening** — the ports are open (open ≠ working).
- **Data path OK / NOT working** — the line that matters: the client made a real request through
  its own proxy. On failure, the `Hint:` line right below tells you what to fix.
- **Traffic leaves this machine through IP …** — the public IP that the destination sees.

## Commands

| Command | What it does |
|---|---|
| `start` | Starts the local HTTP + SOCKS5 proxy |
| `update [--via upstream\|direct\|auto] [--check] [--json]` | Refreshes the saved server list |
| `servers [--refresh] [--check] [--json]` | Lists servers with country/city; `--check` shows which are reachable |
| `start --server <name>` | Pins the connection to a single server (no automatic failover) |
| `auth login / signup / logout` | Log in to / create a Lantern account |
| `--help`, `--version` | Help and version |

Examples (the global flags go **before** the command):

```shell
./lantern-headless-linux-amd64 --data-path ./data update --via upstream
./lantern-headless-linux-amd64 --data-path ./data servers --check
./lantern-headless-linux-amd64 --data-path ./data start --server sunny-tokyo-02
```

## Main options

| Option | Meaning |
|---|---|
| `--upstream-proxy` | Your healthy proxy: config fetch, reaching Lantern servers, last-resort path |
| `--data-path` | Where config and logs live (default `~/.lantern-headless`) |
| `--server <name>` | Pin `start` to one server by name (see `servers`) |
| `--with-proxyless` | Also try direct/smart dialing for control requests (off by default) |
| `--debug` / `--quiet` / `--raw` | Logging |

Full list: `--help`.

## Troubleshooting

- **`update` fails** — try `--via direct` first; if that fails too, set `--upstream-proxy` to a
  working proxy (direct access to the config service is usually blocked). `HTTP 401/403` means
  you should run `auth login` first.
- **`bind: address already in use`** — the port is taken; give other ports to
  `--http-proxy-addr` / `--socks-proxy-addr`.
- **SOCKS connects but pages don't open** — Lantern's servers are unreachable from your network;
  enable `--upstream-proxy`. If the log says `Fell back to the upstream proxy`, traffic is
  leaving through your proxy instead of the Lantern network.
- **A site resolves to `10.x.x.x`** — local DNS is poisoned; the client notices and resolves the
  hostname remotely (log line: `keeping the hostname so it is resolved remotely`).

## Build from source

Requires [Go](https://go.dev/dl/) 1.24 or newer:

```shell
go build -o bin/lantern .
./bin/lantern --help
```

## What this fork changes

- **`--upstream-proxy` does everything at once**: fetches the config, opens the (often blocked)
  TCP connection to Lantern's servers, and acts as the last-resort data path — so the local
  ports keep answering even when no Lantern server is reachable directly.
- **Poisoned-DNS handling**: a private IP returned by local DNS is never dialed; the hostname is
  kept and resolved remotely by the server.
- **`servers` and `update` commands**: server list with locations, reachability checks, and
  pinning `start` to one server.
- **Honest startup diagnostics**: a real request is made through the proxy before it claims
  anything works.

## License

[Apache License 2.0](./LICENSE) — same as upstream. Lantern is a trademark of its owners; this
project is an independent, unofficial fork.
