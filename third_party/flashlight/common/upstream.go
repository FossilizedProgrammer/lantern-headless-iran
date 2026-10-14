package common

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// DialFunc opens a connection to addr on the given network, optionally through
// an upstream proxy.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// UpstreamDialer returns a function that opens raw TCP connections through the
// proxy configured in ClientOptions.UpstreamProxy, or nil when no upstream
// proxy is configured or its scheme cannot carry raw TCP.
//
// It is used for two different things:
//
//  1. Opening the connection to Lantern's own proxy servers. In censored
//     networks those servers are often IP-blocked, while the upstream proxy
//     (Tor, Psiphon, a Cloudflare relay, ...) can still reach them. Only the
//     short-lived connection to the proxy server travels through the upstream;
//     user traffic then runs through Lantern's own transport on top of it.
//  2. As a last-resort data path for browser traffic when no Lantern proxy is
//     reachable at all.
//
// The upstream dialer never replaces a working direct connection by itself:
// callers are expected to fall back to it only when the direct path fails, or
// (case 1) when the upstream is explicitly configured.
func UpstreamDialer() DialFunc {
	raw := GetClientOptions().UpstreamProxy
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		log.Debugf("Ignoring invalid upstream proxy URL %q: %v", raw, err)
		return nil
	}
	switch u.Scheme {
	case "socks5", "socks5h":
		return socksUpstreamDialer(u)
	case "http":
		return httpUpstreamDialer(u, false)
	case "https":
		return httpUpstreamDialer(u, true)
	default:
		log.Debugf("Upstream proxy scheme %q cannot carry raw TCP, ignoring it for dialing", u.Scheme)
		return nil
	}
}

// hostPort returns the host:port of the proxy URL, filling in the default port
// for the given scheme when the URL does not specify one.
func hostPort(u *url.URL, defaultPort int) string {
	host := u.Host
	if host == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(host); err != nil {
		// No port (or a bare IPv6 address).
		host = net.JoinHostPort(u.Hostname(), strconv.Itoa(defaultPort))
	}
	return host
}

// socksUpstreamDialer dials through a SOCKS5 proxy. Hostnames are passed to the
// proxy unresolved (remote DNS), which matters on networks with poisoned DNS.
func socksUpstreamDialer(u *url.URL) DialFunc {
	addr := hostPort(u, 1080)
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if addr == "" {
			return nil, fmt.Errorf("upstream SOCKS5 proxy has no address")
		}
		var auth *proxy.Auth
		if u.User != nil {
			password, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: password}
		}
		d, err := proxy.SOCKS5("tcp", addr, auth, &net.Dialer{})
		if err != nil {
			return nil, fmt.Errorf("creating SOCKS5 dialer: %w", err)
		}
		if cd, ok := d.(proxy.ContextDialer); ok {
			return cd.DialContext(ctx, network, address)
		}
		return dialWithContext(ctx, d, network, address)
	}
}

// httpUpstreamDialer dials through an HTTP proxy using CONNECT.
func httpUpstreamDialer(u *url.URL, overTLS bool) DialFunc {
	addr := hostPort(u, map[bool]int{false: 80, true: 443}[overTLS])
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if addr == "" {
			return nil, fmt.Errorf("upstream HTTP proxy has no address")
		}
		if network != "tcp" {
			return nil, fmt.Errorf("upstream HTTP proxy only supports tcp, not %s", network)
		}
		var d net.Dialer
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		if deadline, ok := ctx.Deadline(); ok {
			_ = conn.SetDeadline(deadline)
		}
		if overTLS {
			tlsConn := tls.Client(conn, &tls.Config{
				ServerName: u.Hostname(),
				MinVersion: tls.VersionTLS12,
			})
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("handshaking with HTTPS upstream proxy: %w", err)
			}
			conn = tlsConn
		}

		var b strings.Builder
		fmt.Fprintf(&b, "CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Connection: keep-alive\r\n", address, address)
		if u.User != nil {
			password, _ := u.User.Password()
			cred := base64.StdEncoding.EncodeToString([]byte(u.User.Username() + ":" + password))
			fmt.Fprintf(&b, "Proxy-Authorization: Basic %s\r\n", cred)
		}
		b.WriteString("\r\n")
		if _, err := io.WriteString(conn, b.String()); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("sending CONNECT to upstream proxy: %w", err)
		}

		br := bufio.NewReader(conn)
		statusLine, err := br.ReadString('\n')
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("reading CONNECT response from upstream proxy: %w", err)
		}
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("reading CONNECT response from upstream proxy: %w", err)
			}
			if line == "\r\n" || line == "\n" {
				break
			}
		}
		// Clear the deadline we set for the handshake; the tunnel is now open.
		_ = conn.SetDeadline(time.Time{})

		fields := strings.SplitN(strings.TrimSpace(statusLine), " ", 3)
		code := 0
		if len(fields) > 1 {
			code, _ = strconv.Atoi(fields[1])
		}
		if code != http.StatusOK {
			_ = conn.Close()
			return nil, fmt.Errorf("upstream proxy refused CONNECT to %s: %s", address, strings.TrimSpace(statusLine))
		}
		return &bufferedConn{Conn: conn, reader: br}, nil
	}
}

// dialWithContext runs a context-less dialer while honoring ctx cancellation.
func dialWithContext(ctx context.Context, d proxy.Dialer, network, addr string) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := d.Dial(network, addr)
		ch <- result{conn, err}
	}()
	select {
	case r := <-ch:
		return r.conn, r.err
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.conn != nil {
				_ = r.conn.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

// bufferedConn preserves bytes the bufio.Reader already consumed while parsing
// the CONNECT response.
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}
