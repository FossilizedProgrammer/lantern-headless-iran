// Command radiance-engine runs the same connection engine as the official
// Lantern v10 client (Radiance + lantern-box) in headless mode: instead of a
// TUN device it exposes a local HTTP/SOCKS proxy built with the "novpn" build
// tag. It is launched by `lantern-headless start --engine radiance` as a child
// process so the headless CLI can keep its flashlight engine untouched while
// offering full beta parity on demand.
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Jigsaw-Code/outline-sdk/transport"
	"github.com/getlantern/kindling"
	"github.com/getlantern/radiance/backend"
	"github.com/getlantern/radiance/config"
	"github.com/getlantern/radiance/events"
	rk "github.com/getlantern/radiance/kindling"
	rfronted "github.com/getlantern/radiance/kindling/fronted"
	rsmart "github.com/getlantern/radiance/kindling/smart"
	rsoar "github.com/getlantern/radiance/kindling/soar"
	"github.com/getlantern/radiance/servers"
	"github.com/getlantern/radiance/vpn"
	singoption "github.com/sagernet/sing-box/option"
	"golang.org/x/net/proxy"
)

var (
	dataPath  = flag.String("data-path", "", "directory to store config, account and logs")
	socksAddr = flag.String("socks-addr", "127.0.0.1:1080", "address for the SOCKS/HTTP mixed inbound")
	httpAddr  = flag.String("http-addr", "", "address for the HTTP proxy (forwarded to the mixed inbound); defaults to the mixed inbound address")
	server    = flag.String("server", "", "pin the connection to a server by tag; empty means auto-select")
	upstream  = flag.String("upstream-proxy", "", "http://, https:// or socks5:// proxy used for control-plane bootstrap")
	proxyless = flag.Bool("proxyless", false, "also try direct/smart dialing for control-plane requests")
	logLevel  = flag.String("log-level", "info", "logging level (trace, debug, info, warn, error)")
	insecure  = flag.Bool("insecure", false, "skip TLS verification for the upstream proxy")

	// listServers runs the engine just long enough to fetch the server list
	// through Radiance's own control plane (honoring --upstream-proxy), prints
	// it on stdout as RADIANCE_SERVERS_JSON <array>, and exits. This is what
	// `lantern-headless servers --engine radiance` and the apps use so the
	// server list always comes from the same engine that serves the traffic.
	listServers = flag.Bool("list-servers", false, "fetch the server list through Radiance, print it as JSON and exit")
	// refreshServers makes --list-servers wait for a fresh fetch from the
	// config service instead of returning the cached list immediately.
	refreshServers = flag.Bool("refresh", false, "with --list-servers, wait for a fresh config fetch before printing")
	// checkServers makes --list-servers also run Radiance's own per-outbound URL
	// tests (its offline pre-warm) and reports each server's reachability. This
	// is the "check availability" action: it tests the exact path the engine
	// would use, instead of a plain TCP probe from the caller.
	checkServers = flag.Bool("check", false, "with --list-servers, run Radiance's own URL tests and include per-server reachability")
)

func main() {
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *dataPath == "" {
		home, _ := os.UserHomeDir()
		*dataPath = filepath.Join(home, ".lantern-headless")
	}
	if err := os.MkdirAll(*dataPath, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "radiance-engine: create data path: %v\n", err)
		os.Exit(1)
	}

	// Resolving/binding the mixed inbound is only relevant when we actually
	// serve traffic; the list-servers mode exits before any tunnel is built.
	var listenAddr string
	if !*listServers {
		la, lerr := resolveAddr(*socksAddr)
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "radiance-engine: resolve --socks-addr: %v\n", lerr)
			os.Exit(1)
		}
		listenAddr = la
		// Radiance's novpn build reads the mixed inbound address from the
		// environment when it assembles the sing-box options at connect time.
		os.Setenv("RADIANCE_SOCKS_ADDRESS", listenAddr)
		os.Setenv("RADIANCE_LOG_LEVEL", *logLevel)
	}

	if *upstream != "" {
		if err := installUpstreamKindling(ctx, *dataPath, *upstream, *insecure); err != nil {
			fmt.Fprintf(os.Stderr, "radiance-engine: upstream kindling setup: %v\n", err)
			os.Exit(1)
		}
	}

	be, err := backend.NewLocalBackend(ctx, backend.Options{
		DataDir:                  *dataPath,
		LogDir:                   *dataPath,
		LogLevel:                 *logLevel,
		PreserveTelemetryConsent: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "radiance-engine: create backend: %v\n", err)
		os.Exit(1)
	}
	defer be.Close()

	if user, uerr := be.UserData(); uerr != nil || user == nil {
		if _, nerr := be.NewUser(ctx); nerr != nil {
			fmt.Fprintf(os.Stderr, "radiance-engine: create user: %v\n", nerr)
		}
	}

	be.Start()

	if *listServers {
		// When asked to check, capture the offline URL-test results Radiance
		// emits (from its own pre-warm) so we can report per-server reachability.
		var col *urlTestCollector
		if *checkServers {
			col = &urlTestCollector{}
			sub := events.SubscribeContext(ctx, col.onEvent)
			defer sub.Unsubscribe()
		}

		list, lerr := waitForServerList(ctx, be, 90*time.Second, *refreshServers)
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "radiance-engine: %v\n", lerr)
			os.Exit(1)
		}
		if col != nil {
			// The offline URL tests run right after the config is applied; wait
			// for their results (bounded) before printing.
			col.wait(ctx, 40*time.Second)
		}
		if perr := printServerList(list, col); perr != nil {
			fmt.Fprintf(os.Stderr, "radiance-engine: encode servers: %v\n", perr)
			os.Exit(1)
		}
		// Cancel the context so any in-flight offline URL tests stop before
		// Close() waits on them; otherwise this one-shot mode can linger.
		stop()
		return
	}

	connectCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	err = be.ConnectVPN(connectCtx, *server)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "radiance-engine: connect: %v\n", err)
		os.Exit(1)
	}

	// The mixed inbound already speaks HTTP, but only listens on one address.
	// Expose the HTTP proxy on its own port with a tiny TCP forwarder when the
	// caller asked for a different one.
	httpListen := resolveHTTPAddr(*httpAddr, listenAddr)
	if httpListen != listenAddr {
		ln, lerr := net.Listen("tcp", httpListen)
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "radiance-engine: listen http: %v\n", lerr)
			os.Exit(1)
		}
		defer ln.Close()
		httpListen = ln.Addr().String()
		go forwardLoop(ln, listenAddr)
	}

	fmt.Printf("RADIANCE_READY socks=%s http=%s\n", listenAddr, httpListen)
	os.Stdout.Sync()

	<-ctx.Done()
	if derr := be.DisconnectVPN(); derr != nil {
		fmt.Fprintf(os.Stderr, "radiance-engine: disconnect: %v\n", derr)
	}
}

// serversJSONPrefix is the marker line that carries the server list; the rest
// of the engine's output is logging, so consumers look for this prefix.
const serversJSONPrefix = "RADIANCE_SERVERS_JSON "

// listedServer mirrors the JSON shape emitted by `lantern-headless servers
// --json`, so both engines produce a list the clients parse identically.
type listedServer struct {
	Name        string `json:"name"`
	Address     string `json:"addr"`
	Protocol    string `json:"protocol"`
	CountryCode string `json:"country_code"`
	Country     string `json:"country"`
	City        string `json:"city"`
	Trusted     bool   `json:"trusted"`
	// Reachable is only set when --check was requested and Radiance's URL tests
	// completed: true when that server's outbound passed its test, false when it
	// did not (or was filtered out).
	Reachable *bool `json:"reachable,omitempty"`
}

// urlTestCollector records the per-tag results of Radiance's offline URL tests.
type urlTestCollector struct {
	mu   sync.Mutex
	seen map[string]bool
	done bool
}

func (c *urlTestCollector) onEvent(evt vpn.URLTestCompleteEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen == nil {
		c.seen = make(map[string]bool, len(evt.Results))
	}
	for tag, delay := range evt.Results {
		c.seen[tag] = delay > 0
	}
	c.done = true
}

// wait blocks until the URL tests have produced a result or the timeout elapses.
func (c *urlTestCollector) wait(ctx context.Context, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		c.mu.Lock()
		done := c.done
		c.mu.Unlock()
		if done || time.Now().After(deadline) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// lookup reports the reachability of a server tag, or nil when no check ran.
func (c *urlTestCollector) lookup(tag string) *bool {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.done {
		return nil
	}
	v := c.seen[tag]
	return &v
}

// waitForServerList polls the backend until Radiance has loaded at least one
// server from its cached config or a fresh fetch, or the timeout elapses. When
// requireFresh is set it also waits for a NewConfigEvent (a fresh fetch) before
// returning, falling back to whatever is cached if the timeout is hit.
func waitForServerList(ctx context.Context, be *backend.LocalBackend, timeout time.Duration, requireFresh bool) ([]*servers.Server, error) {
	gotFresh := !requireFresh
	if requireFresh {
		fresh := make(chan struct{}, 1)
		sub := events.SubscribeContext(ctx, func(config.NewConfigEvent) {
			select {
			case fresh <- struct{}{}:
			default:
			}
		})
		defer sub.Unsubscribe()
		deadline := time.Now().Add(timeout)
		for {
			list := be.AllServers()
			if len(list) > 0 && gotFresh {
				return list, nil
			}
			if time.Now().After(deadline) {
				if len(list) > 0 {
					return list, nil
				}
				return nil, fmt.Errorf("timed out waiting for the server list; check the account/config or --upstream-proxy")
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-fresh:
				gotFresh = true
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	deadline := time.Now().Add(timeout)
	for {
		if list := be.AllServers(); len(list) > 0 {
			return list, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for the server list; check the account/config or --upstream-proxy")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// printServerList converts Radiance's servers into the client-facing JSON shape
// and writes it on a single marker line, so it can be parsed even when the
// engine also logs to stdout.
func printServerList(list []*servers.Server, checks *urlTestCollector) error {
	out := make([]listedServer, 0, len(list))
	for _, s := range list {
		if s == nil || s.Tag == "" {
			continue
		}
		addr, proto := serverEndpoint(s)
		out = append(out, listedServer{
			Name:        s.Tag,
			Address:     addr,
			Protocol:    proto,
			CountryCode: s.Location.CountryCode,
			Country:     s.Location.Country,
			City:        s.Location.City,
			Trusted:     s.IsLantern,
			Reachable:   checks.lookup(s.Tag),
		})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	fmt.Printf("%s%s\n", serversJSONPrefix, b)
	os.Stdout.Sync()
	return nil
}

// serverEndpoint best-effort extracts "server:port" and the outbound type from
// the server's typed sing-box options. The Outbound/Endpoint wrapper hides the
// concrete options behind `json:"-"`, so we unwrap it before marshaling.
func serverEndpoint(s *servers.Server) (addr, proto string) {
	proto = s.Type
	inner := s.Options
	switch o := s.Options.(type) {
	case singoption.Outbound:
		if o.Type != "" {
			proto = o.Type
		}
		inner = o.Options
	case singoption.Endpoint:
		if o.Type != "" {
			proto = o.Type
		}
		inner = o.Options
	}
	raw, err := json.Marshal(inner)
	if err != nil {
		return "", proto
	}
	var m map[string]any
	if jerr := json.Unmarshal(raw, &m); jerr != nil {
		return "", proto
	}
	host, _ := m["server"].(string)
	if host == "" {
		return "", proto
	}
	if port, ok := m["server_port"].(float64); ok && port > 0 {
		return net.JoinHostPort(host, strconv.Itoa(int(port))), proto
	}
	return host, proto
}

// resolveAddr turns host:0 into host:<free-port> so callers learn the concrete
// address Radiance will bind (Radiance does not report the bound port back).
func resolveAddr(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	if port != "0" {
		return addr, nil
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return "", err
	}
	defer ln.Close()
	return ln.Addr().String(), nil
}

// resolveHTTPAddr picks the HTTP proxy address: the explicit --http-addr, or
// the mixed inbound address when none was given.
func resolveHTTPAddr(addr, fallback string) string {
	if strings.TrimSpace(addr) != "" {
		return addr
	}
	return fallback
}

// installUpstreamKindling builds a custom kindling client that routes its
// control-plane dials through --upstream-proxy and installs it before Radiance
// initializes its own. It mirrors radiance/kindling.NewKindling but swaps the
// stream dialer for one that dials through the upstream proxy.
func installUpstreamKindling(ctx context.Context, dataDir, proxyURL string, insecure bool) error {
	sd, err := upstreamStreamDialer(proxyURL, insecure)
	if err != nil {
		return err
	}
	logw := io.Writer(os.Stderr)
	opts := []kindling.Option{
		kindling.WithLogWriter(logw),
		kindling.WithStreamDialer(sd),
		kindling.WithSmartDialerConfig(rsmart.DialerConfig),
	}
	if f, ferr := rfronted.NewFronted(ctx, filepath.Join(dataDir, "fronted_cache.json"), logw); ferr != nil {
		fmt.Fprintf(os.Stderr, "radiance-engine: fronted unavailable, continuing without it: %v\n", ferr)
	} else if f != nil {
		opts = append(opts, kindling.WithDomainFronting(f))
	}
	opts = append(opts,
		kindling.WithProxyless("df.getiantem.org", "api.getiantem.org"),
		kindling.WithProxylessAliases(map[string]string{"df.iantem.io": "df.getiantem.org"}),
	)
	if rsoar.Configured() {
		if sc, serr := rsoar.New(dataDir); serr != nil {
			fmt.Fprintf(os.Stderr, "radiance-engine: DNS tunnel unavailable: %v\n", serr)
		} else {
			opts = append(opts, kindling.WithDNSTunnel(sc))
		}
	}
	k, err := kindling.NewKindling("radiance", opts...)
	if err != nil {
		return err
	}
	rk.SetKindling(&rk.Client{Kindling: k})
	return nil
}

// upstreamStreamDialer returns an Outline-SDK stream dialer that dials every
// target through the configured upstream proxy.
func upstreamStreamDialer(rawURL string, insecure bool) (transport.StreamDialer, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid --upstream-proxy %q: %w", rawURL, err)
	}
	var dial func(ctx context.Context, addr string) (net.Conn, error)
	switch strings.ToLower(u.Scheme) {
	case "socks5", "socks5h":
		var auth *proxy.Auth
		if u.User != nil {
			pw, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: pw}
		}
		d, derr := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
		if derr != nil {
			return nil, derr
		}
		cd, ok := d.(proxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("socks5 dialer does not support contexts")
		}
		dial = func(ctx context.Context, addr string) (net.Conn, error) {
			return cd.DialContext(ctx, "tcp", addr)
		}
	case "http", "https":
		dial = func(ctx context.Context, addr string) (net.Conn, error) {
			return httpConnectDial(ctx, u, addr, insecure)
		}
	default:
		return nil, fmt.Errorf("unsupported --upstream-proxy scheme %q (use http, https or socks5)", u.Scheme)
	}
	return transport.FuncStreamDialer(func(ctx context.Context, addr string) (transport.StreamConn, error) {
		conn, cerr := dial(ctx, addr)
		if cerr != nil {
			return nil, cerr
		}
		return streamConn{Conn: conn}, nil
	}), nil
}

// streamConn adapts a net.Conn to transport.StreamConn. Conns without a
// half-close implementation fall back to a full close, which is safe for the
// control-plane probes kindling runs over this dialer.
type streamConn struct{ net.Conn }

func (c streamConn) CloseRead() error {
	if hc, ok := c.Conn.(interface{ CloseRead() error }); ok {
		return hc.CloseRead()
	}
	return nil
}

func (c streamConn) CloseWrite() error {
	if hc, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return hc.CloseWrite()
	}
	return nil
}

// httpConnectDial opens a CONNECT tunnel to addr through an HTTP(S) proxy.
func httpConnectDial(ctx context.Context, proxyURL *url.URL, addr string, insecure bool) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", proxyURL.Host)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(proxyURL.Scheme, "https") {
		host, _, _ := net.SplitHostPort(proxyURL.Host)
		tconn := tls.Client(conn, &tls.Config{ServerName: host, InsecureSkipVerify: insecure})
		if herr := tconn.HandshakeContext(ctx); herr != nil {
			_ = conn.Close()
			return nil, herr
		}
		conn = tconn
	}
	var b strings.Builder
	fmt.Fprintf(&b, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", addr, addr)
	if proxyURL.User != nil {
		pw, _ := proxyURL.User.Password()
		cred := base64.StdEncoding.EncodeToString([]byte(proxyURL.User.Username() + ":" + pw))
		fmt.Fprintf(&b, "Proxy-Authorization: Basic %s\r\n", cred)
	}
	b.WriteString("\r\n")
	if _, werr := conn.Write([]byte(b.String())); werr != nil {
		_ = conn.Close()
		return nil, werr
	}
	resp, rerr := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if rerr != nil {
		_ = conn.Close()
		return nil, rerr
	}
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("upstream proxy CONNECT %s failed: %s", addr, resp.Status)
	}
	return conn, nil
}

// forwardLoop accepts connections on ln and pipes them to target. Because
// sing-box's mixed inbound speaks both SOCKS and HTTP on one port, raw TCP
// forwarding is enough to expose an HTTP-only entry point.
func forwardLoop(ln net.Listener, target string) {
	for {
		client, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer client.Close()
			upstream, derr := net.Dial("tcp", target)
			if derr != nil {
				return
			}
			defer upstream.Close()
			var wg sync.WaitGroup
			wg.Add(2)
			go func() { defer wg.Done(); _, _ = io.Copy(upstream, client); closeWrite(upstream) }()
			go func() { defer wg.Done(); _, _ = io.Copy(client, upstream); closeWrite(client) }()
			wg.Wait()
		}()
	}
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = c.Close()
}
