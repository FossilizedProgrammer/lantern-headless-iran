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
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Jigsaw-Code/outline-sdk/transport"
	"github.com/getlantern/kindling"
	"github.com/getlantern/radiance/backend"
	rk "github.com/getlantern/radiance/kindling"
	rfronted "github.com/getlantern/radiance/kindling/fronted"
	rsmart "github.com/getlantern/radiance/kindling/smart"
	rsoar "github.com/getlantern/radiance/kindling/soar"
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

	listenAddr, err := resolveAddr(*socksAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "radiance-engine: resolve --socks-addr: %v\n", err)
		os.Exit(1)
	}
	// Radiance's novpn build reads the mixed inbound address from the
	// environment when it assembles the sing-box options at connect time.
	os.Setenv("RADIANCE_SOCKS_ADDRESS", listenAddr)
	os.Setenv("RADIANCE_LOG_LEVEL", *logLevel)

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
