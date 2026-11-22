# lantern-headless-iran

[English](./README.md) · [فارسی](./README-fa.md) · [Русский](./README-ru.md) · **中文** · [Español](./README-es.md)

> [`getlantern/lantern-headless-client`](https://github.com/getlantern/lantern-headless-client) 的**非官方**分支，与 Lantern 团队无关。

一个**没有图形界面**（headless）的 Lantern 客户端，专为审查严格的网络而做（在伊朗实测过）。
它会打开两个本地端口，你把浏览器指向它们，流量便经由 Lantern 网络发出。

## 两个连接引擎

`start` 可以用两个引擎之一运行；两者都开放相同的本地 HTTP + SOCKS5 端口，因此浏览器配置完全一样。

- **`--engine radiance` —— 官方 v11 引擎（推荐）。** 运行**与官方 Lantern v11 客户端相同的引擎**
  （Radiance + lantern-box，sing-box 1.14）。它用现代传输方式（domain fronting、直连/智能拨号等）
  自己抵达 Lantern 的服务器，因此在大多数受审查的网络上**完全不需要上游代理就能连接** —— 这才是
  真正穿过审查的模式。它需要 CLI 旁边的 `lantern-radiance` 辅助二进制（随 Releases 发布）。
- **`--engine flashlight`（默认）—— 本分支原有的传输方式。** 更轻量，但在封锁了 Lantern 服务器
  IP 的网络里，它需要一个可用的 `--upstream-proxy`。单独使用时，它只能通过 Cloudflare 别名取得
  配置，并经由该上游给你一个合适的出口 IP；它**不会**自己绕过审查。

> 在桌面应用 [MischiefPingu](https://github.com/) 中，radiance 引擎是默认引擎，两个二进制都已随
> 应用打包，所以那里不需要任何参数。

## 下载

二进制由 **GitHub Actions 自动编译**并发布到
[Releases](https://github.com/FossilizedProgrammer/lantern-headless-iran/releases) 页面，**不会从
任何人的电脑手动上传**。每个版本为每个平台发布**一个同时包含两个二进制的压缩包**：

| 文件 | 平台 | 内容 |
|---|---|---|
| `lantern-headless-linux-amd64.tar.gz` | Linux，x86_64 | `lantern`（CLI）+ `lantern-radiance`（v11 引擎） |
| `lantern-headless-windows-amd64.zip` | Windows，64 位 | `lantern.exe`（CLI）+ `lantern-radiance.exe`（v11 引擎） |

解压后两个文件会落在同一个文件夹里，这正是 CLI 期望的位置。**要用 `--engine radiance`，请把
`lantern-radiance`（Windows 上是 `lantern-radiance.exe`）放在 CLI 旁边。** CLI 会在自己旁边（或
`--radiance-engine`，或 `$PATH`）寻找它。`--engine flashlight` 不需要它。

工作方式：每次推送到 `main` 都会为每个平台编译两个二进制（保持构建可用），而每个 `v*` 版本标签
还会额外创建一个包含这些压缩包的 **Release**。

## 快速开始（radiance —— 推荐）

1. 从 **Releases** 下载适合你系统的压缩包并解压（`lantern` 和 `lantern-radiance` 会在同一目录）。
2. 直接运行 —— **不需要任何上游代理**：

**Linux**

```shell
chmod +x lantern lantern-radiance
./lantern --data-path ./data start --engine radiance \
  --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

**Windows（命令提示符）**

```bat
lantern.exe --data-path data start --engine radiance --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

3. 把浏览器指向本地端口：

**Firefox**（推荐 —— 本地 DNS 通常被污染）：
`设置 → 常规 → 网络设置 → 手动配置代理` → SOCKS 主机 `127.0.0.1`，端口 `18071`，类型
**SOCKS v5**，勾选 **“使用 SOCKS5 时代理 DNS 查询”**。

**Chrome/Chromium**

```shell
google-chrome --proxy-server="socks5://127.0.0.1:18071"
```

> 客户端是无头的：它从不自行修改系统设置。如果浏览器没有配置好，流量会直连，不会经过审查。

### 快速开始（flashlight —— 旧方式，需要上游）

如果你更喜欢旧的传输方式，或者缺少 radiance 辅助二进制，可以让 flashlight 引擎走一个可用的本地
代理（Tor、Psiphon 等），端口 `1080`：

**Linux**

```shell
./lantern --data-path ./data --upstream-proxy socks5://127.0.0.1:1080 \
  start --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

**Windows（命令提示符）**

```bat
lantern.exe --data-path data --upstream-proxy socks5://127.0.0.1:1080 start --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

`--upstream-proxy` 支持 `socks5://`、`http://` 和 `https://`。在 flashlight 引擎中它同时承担三件事：
获取配置、打开（常被 IP 封锁的）到 Lantern 服务器的连接，以及作为最后的数据通路。在
`--engine radiance` 下它是可选的，只用于引导控制平面。

## 启动信息是什么意思

radiance 引擎：

```
SUCCESS  Radiance engine connected to the Lantern network
INFO     Local proxy is listening — SOCKS5: 127.0.0.1:18071  HTTP: 127.0.0.1:18070
INFO     Verifying the data path with a real request through that proxy...
SUCCESS  Data path OK — HTTP 204 from https://www.google.com/generate_204 in 505ms
INFO     Traffic leaves this machine through IP 163.123.193.24
```

flashlight 引擎：

```
SUCCESS  Control plane reachable (config fetched successfully)
INFO     Local proxy is listening — SOCKS5: 127.0.0.1:18071  HTTP: 127.0.0.1:18070
SUCCESS  Data path OK — HTTP 204 from https://www.google.com/generate_204 in 803ms
INFO     Traffic leaves this machine through IP 31.171.100.110
```

- **… connected / Control plane reachable** —— 连接已建立。这本身并不说明浏览器流量如何。
- **Local proxy is listening** —— 端口已打开（打开 ≠ 能用）。
- **Data path OK / NOT working** —— 最关键的一行：客户端通过自己的代理发出了真实请求。失败时，
  紧随其下的 `Hint:` 行会告诉你要修什么。
- **Traffic leaves this machine through IP …** —— 目标网站看到的公网 IP。

## 命令

| 命令 | 作用 |
|---|---|
| `start [--engine radiance\|flashlight]` | 启动本地 HTTP + SOCKS5 代理 |
| `update [--via upstream\|direct\|auto] [--check] [--json]` | 刷新已保存的服务器列表 |
| `servers [--engine radiance\|flashlight] [--refresh] [--check] [--json]` | 列出服务器及其国家/城市。列表来自所选引擎；`--check` **经由该引擎**测试可达性（Radiance 会对每个服务器做自己的 URL 测试；flashlight 的 `--check` 是直接探测）。 |
| `start --server <name>` | 把连接固定到单个服务器（不自动切换） |
| `auth login / signup / logout` | 登录 / 注册 / 退出 Lantern 账号 |
| `--help`, `--version` | 帮助与版本 |

示例（全局参数放在命令**之前**，`start` 的参数放在命令之后）：

```shell
./lantern --data-path ./data start --engine radiance
./lantern --data-path ./data update --via upstream
./lantern --data-path ./data servers --check
./lantern --data-path ./data servers --engine radiance --json
./lantern --data-path ./data start --engine radiance --server sunny-tokyo-02
```

## 主要选项

| 选项 | 含义 |
|---|---|
| `--engine <radiance\|flashlight>` | 连接引擎（默认 `flashlight`）。要与官方 v11 一致请用 `radiance` |
| `--radiance-engine <path>` | `lantern-radiance` 辅助二进制的路径（默认：CLI 旁边、`LANTERN_RADIANCE_ENGINE` 或 `$PATH`） |
| `--upstream-proxy` | 你自备的可用代理：控制平面引导；flashlight 引擎需要它来抵达被封锁的服务器 |
| `--data-path` | 配置和日志所在目录（默认 `~/.lantern-headless`） |
| `--server <name>` | 按名称把 `start` 固定到一个服务器（见 `servers`） |
| `--with-proxyless` | 对控制请求也尝试直连/智能拨号（默认关闭） |
| `--debug` / `--quiet` / `--raw` | 日志级别 |

完整列表：`--help`。

## 故障排查

- **`could not find the lantern-radiance helper binary`** —— 从发布压缩包里解出
  `lantern-radiance`（`lantern-radiance.exe`）放到 CLI 旁边，或传入
  `--radiance-engine /path/to/lantern-radiance`。
- **`--engine radiance` 还没就绪就退出** —— 用 `--debug` 重新运行以查看引擎自身的日志；详情也会
  写入 `--data-path` 下的日志文件。
- **`update` 失败** —— 直连走 Cloudflare 别名（`df.getiantem.org`）；如果它也被封锁，请用
  `--upstream-proxy` 指定一个可用代理。`HTTP 401/403` 表示需要先执行 `auth login`。
- **`bind: address already in use`** —— 端口被占用；给 `--http-proxy-addr` /
  `--socks-proxy-addr` 换其他端口。
- **SOCKS 能连上但网页打不开（flashlight 引擎）** —— 你的网络无法抵达 Lantern 的服务器；启用
  `--upstream-proxy`，或改用能自己抵达它们的 `--engine radiance`。
- **网站解析到 `10.x.x.x`** —— 本地 DNS 被污染；客户端会察觉，并在服务器端远程解析该主机名
  （日志行：`keeping the hostname so it is resolved remotely`）。

## 从源码构建

CLI 需要 [Go](https://go.dev/dl/) 1.24 或更新版本：

```shell
go build -o bin/lantern .
./bin/lantern --help
```

radiance 引擎位于独立模块（`radiance-engine/`），**必须用 Go 1.26.x 编译**（Go 1.27 会破坏
sing-box 的一个 `//go:linkname`）。用辅助目标编译它：

```shell
make radiance-engine   # 输出 bin/lantern-radiance
```

在 Linux 上它使用 CGO；交叉编译到 Windows 请用 `CGO_ENABLED=0 GOOS=windows GOARCH=amd64
make radiance-engine`。把生成的辅助二进制放到 CLI 旁边。

想一步构建**全部**（CLI + radiance 辅助二进制）到某个平台，请用脚本：

```shell
./scripts/build-linux.sh     # dist/lantern-headless-linux-amd64.tar.gz   (lantern + lantern-radiance)
./scripts/build-windows.sh   # dist/lantern-headless-windows-amd64.zip    (lantern.exe + lantern-radiance.exe)
```

（Windows 脚本为 CLI 需要 MinGW-w64；它的 radiance 辅助二进制是纯 Go）。同样的目标也可用
`make build-local-linux` / `make build-local-windows`。

## 本分支改了什么

- **Radiance 引擎（`--engine radiance`）**：与官方 Lantern v11 客户端完全一致 —— 相同的
  Radiance + lantern-box 数据平面（sing-box 1.14.2，与 v11 相同），以 `novpn` 标签编译，从而提供
  HTTP/SOCKS 代理而不是 TUN 设备。它借助现代抗审查手段自行连接，因此在受审查的网络里无需上游
  代理也能工作。
- **按服务器显示延迟**（与 v10.0.4/v11 一致）：`servers --check` 会报告每个服务器的应答耗时、标出
  最快的一个，`--json` 里以 `latency_ms` 返回。
- **固定服务器告警**（与 v11 一致）：用 `--server` 固定会关闭自动切换，因此固定的服务器只探测一次，
  无应答时会打印警告。
- **控制平面的 Cloudflare 别名**：配置/认证请求走 `df.getiantem.org` 而不是 `df.iantem.io`，因为
  Google 的边缘节点对伊朗 IP 返回 `403` —— 这与 Lantern 自己的 Radiance 客户端在受限地区使用的
  是同一个别名。
- **flashlight 引擎的 `--upstream-proxy`**：打开（常被封锁的）到 Lantern 服务器的 TCP 连接并充当
  最后的数据通路 —— 因此本地端口始终有响应，即使没有任何服务器可直连，流量也能经由 Lantern 网络
  发出。
- **污染 DNS 处理**：本地 DNS 返回的私有 IP 绝不会被直接拨号；主机名会被保留并由服务器端远程解析。
- **`servers` 与 `update` 命令**：带地理位置的服务器列表、可达性检查，以及把 `start` 固定到某个
  服务器。
- **诚实的启动诊断**：在声称一切可用之前，会先通过代理发出真实请求。

## 许可证

[Apache License 2.0](./LICENSE) —— 与上游相同。Lantern 是其所有者的商标；本项目是独立、非官方的
分支。
