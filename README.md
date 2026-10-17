# lantern-headless-iran

**English** · [فارسی](./README-fa.md) · [Русский](./README-ru.md)

> **Unofficial** fork of [`getlantern/lantern-headless-client`](https://github.com/getlantern/lantern-headless-client). Not affiliated with Lantern.

A Lantern client **without a graphical interface** (headless), made for networks with heavy
filtering (tested in Iran). It opens two local ports, and your browser is pointed at them —
your traffic then leaves through the Lantern network.

## Two connection engines

`start` can run either of two engines; both expose the same local HTTP + SOCKS5 ports, so the
browser setup is identical.

- **`--engine radiance` — the official v10 engine (recommended).** This runs the **same engine as
  the official Lantern v10 client** (Radiance + lantern-box). It reaches Lantern's servers on its
  own with the modern transport (domain fronting, proxyless/smart dialing, …), so on most filtered
  networks **it connects with no upstream proxy at all** — this is the mode that actually gets past
  the filter. It needs the `lantern-radiance` helper binary next to the CLI (shipped in the
  Releases).
- **`--engine flashlight` (default) — this fork's original transport.** Lighter, but on networks
  that block Lantern's server IPs it needs a working `--upstream-proxy`. On its own it can only
  fetch the config through the Cloudflare alias and hand you a suitable exit IP via that upstream;
  it does **not** bypass the filter by itself.

> In the [MischiefPingu](https://github.com/) desktop app the radiance engine is the default and
> both binaries are bundled, so no flags are needed there.

## Download

Binaries are compiled **automatically by GitHub Actions** and published on the
[Releases](https://github.com/FossilizedProgrammer/lantern-headless-iran/releases) page.
Nothing is uploaded from anyone's computer:

| File | Platform | What it is |
|---|---|---|
| `lantern-headless-linux-amd64` | Linux, x86_64 | the CLI |
| `lantern-headless-windows-amd64.exe` | Windows, 64-bit | the CLI |
| `lantern-radiance` | Linux, x86_64 | the v10 engine helper |
| `lantern-radiance.exe` | Windows, 64-bit | the v10 engine helper |

**For `--engine radiance` put the `lantern-radiance` (`lantern-radiance.exe` on Windows) file in the
same folder as the CLI.** The CLI looks for it next to itself (or at `--radiance-engine`, or in
`$PATH`). `--engine flashlight` does not need it.

How it works: every push to `main` builds all four binaries (to keep the build green), and every
version tag `v*` additionally creates a **Release** containing those same files.

## Quick start (radiance — recommended)

1. Download the CLI **and** the matching `lantern-radiance` helper for your system from
   **Releases**, and put them in the same folder.
2. Run it — **no upstream proxy is required**:

**Linux**

```shell
chmod +x lantern-headless-linux-amd64 lantern-radiance
./lantern-headless-linux-amd64 --data-path ./data start --engine radiance \
  --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

**Windows (Command Prompt)**

```bat
lantern-headless-windows-amd64.exe --data-path data start --engine radiance --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

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

### Quick start (flashlight — legacy, needs an upstream)

If you prefer the old transport, or the radiance helper is missing, run the flashlight engine with
a working local proxy (Tor, Psiphon, …) on port `1080`:

**Linux**

```shell
./lantern-headless-linux-amd64 --data-path ./data --upstream-proxy socks5://127.0.0.1:1080 \
  start --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

**Windows (Command Prompt)**

```bat
lantern-headless-windows-amd64.exe --data-path data --upstream-proxy socks5://127.0.0.1:1080 start --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

`--upstream-proxy` accepts `socks5://`, `http://` or `https://`. In the flashlight engine it does
everything at once: fetch the config, open the (often IP-blocked) connection to Lantern's own
servers, and act as the last-resort data path. With `--engine radiance` it is optional and only
used to bootstrap the control plane.

## What the startup messages mean

Radiance engine:

```
SUCCESS  Radiance engine connected to the Lantern network
INFO     Local proxy is listening — SOCKS5: 127.0.0.1:18071  HTTP: 127.0.0.1:18070
INFO     Verifying the data path with a real request through that proxy...
SUCCESS  Data path OK — HTTP 204 from https://www.google.com/generate_204 in 505ms
INFO     Traffic leaves this machine through IP 163.123.193.24
```

Flashlight engine:

```
SUCCESS  Control plane reachable (config fetched successfully)
INFO     Local proxy is listening — SOCKS5: 127.0.0.1:18071  HTTP: 127.0.0.1:18070
SUCCESS  Data path OK — HTTP 204 from https://www.google.com/generate_204 in 803ms
INFO     Traffic leaves this machine through IP 31.171.100.110
```

- **… connected / Control plane reachable** — the connection is up. On its own it says nothing
  about browser traffic.
- **Local proxy is listening** — the ports are open (open ≠ working).
- **Data path OK / NOT working** — the line that matters: the client made a real request through
  its own proxy. On failure, the `Hint:` line right below tells you what to fix.
- **Traffic leaves this machine through IP …** — the public IP that the destination sees.

## Commands

| Command | What it does |
|---|---|
| `start [--engine radiance\|flashlight]` | Starts the local HTTP + SOCKS5 proxy |
| `update [--via upstream\|direct\|auto] [--check] [--json]` | Refreshes the saved server list |
| `servers [--refresh] [--check] [--json]` | Lists servers with country/city; `--check` shows which are reachable |
| `start --server <name>` | Pins the connection to a single server (no automatic failover) |
| `auth login / signup / logout` | Log in to / create a Lantern account |
| `--help`, `--version` | Help and version |

Examples (the global flags go **before** the command; `start` flags go after it):

```shell
./lantern-headless-linux-amd64 --data-path ./data start --engine radiance
./lantern-headless-linux-amd64 --data-path ./data update --via upstream
./lantern-headless-linux-amd64 --data-path ./data servers --check
./lantern-headless-linux-amd64 --data-path ./data start --engine radiance --server sunny-tokyo-02
```

## Main options

| Option | Meaning |
|---|---|
| `--engine <radiance\|flashlight>` | Connection engine (default `flashlight`). Use `radiance` for official v10 parity |
| `--radiance-engine <path>` | Path to the `lantern-radiance` helper (default: next to the CLI, `LANTERN_RADIANCE_ENGINE`, or `$PATH`) |
| `--upstream-proxy` | Your healthy proxy: control-plane bootstrap; required by the flashlight engine to reach blocked servers |
| `--data-path` | Where config and logs live (default `~/.lantern-headless`) |
| `--server <name>` | Pin `start` to one server by name (see `servers`) |
| `--with-proxyless` | Also try direct/smart dialing for control requests (off by default) |
| `--debug` / `--quiet` / `--raw` | Logging |

Full list: `--help`.

## Troubleshooting

- **`could not find the lantern-radiance helper binary`** — download `lantern-radiance`
  (`lantern-radiance.exe`) from Releases and put it next to the CLI, or pass
  `--radiance-engine /path/to/lantern-radiance`.
- **`--engine radiance` exits before it is ready** — re-run with `--debug` to see the engine's own
  logs; the details are also written to the log file in `--data-path`.
- **`update` fails** — direct access uses the Cloudflare alias (`df.getiantem.org`); if that is
  blocked too, set `--upstream-proxy` to a working proxy. `HTTP 401/403` means you should run
  `auth login` first.
- **`bind: address already in use`** — the port is taken; give other ports to
  `--http-proxy-addr` / `--socks-proxy-addr`.
- **SOCKS connects but pages don't open (flashlight engine)** — Lantern's servers are unreachable
  from your network; enable `--upstream-proxy`, or switch to `--engine radiance`, which reaches
  them on its own.
- **A site resolves to `10.x.x.x`** — local DNS is poisoned; the client notices and resolves the
  hostname remotely (log line: `keeping the hostname so it is resolved remotely`).

## Build from source

The CLI needs [Go](https://go.dev/dl/) 1.24 or newer:

```shell
go build -o bin/lantern .
./bin/lantern --help
```

The radiance engine lives in its own module (`radiance-engine/`) and **must be built with Go
1.26.x** (Go 1.27 breaks a sing-box `//go:linkname`). Build it with the helper target:

```shell
make radiance-engine   # writes bin/lantern-radiance
```

On Linux it uses CGO; for a Windows cross-build use `CGO_ENABLED=0 GOOS=windows GOARCH=amd64
make radiance-engine`. Put the resulting helper next to the CLI.

## What this fork changes

- **Radiance engine (`--engine radiance`)**: full parity with the official Lantern v10 client —
  the same Radiance + lantern-box data plane, built with the `novpn` tag so it serves an HTTP/SOCKS
  proxy instead of a TUN device. It connects through modern censorship resistance on its own, so it
  works on filtered networks without an upstream proxy.
- **Cloudflare alias for the control plane**: config/auth requests target `df.getiantem.org`
  instead of `df.iantem.io`, because Google's edge returns `403` to Iranian IPs — this is the
  same alias Lantern's own Radiance client uses for restricted regions.
- **`--upstream-proxy` for the flashlight engine**: opens the (often blocked) TCP connection to
  Lantern's servers and acts as the last-resort data path — so the local ports keep answering,
  and traffic can leave through the Lantern network, even when no server is directly reachable.
- **Poisoned-DNS handling**: a private IP returned by local DNS is never dialed; the hostname is
  kept and resolved remotely by the server.
- **`servers` and `update` commands**: server list with locations, reachability checks, and
  pinning `start` to one server.
- **Honest startup diagnostics**: a real request is made through the proxy before it claims
  anything works.

## License

[Apache License 2.0](./LICENSE) — same as upstream. Lantern is a trademark of its owners; this
project is an independent, unofficial fork.
