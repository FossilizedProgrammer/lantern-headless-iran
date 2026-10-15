package common

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/getlantern/kindling"
)

// ClientOptions controls how the control-plane HTTP client (kindling) is built.
// SetClientOptions must be called before the first call to GetHTTPClient.
type ClientOptions struct {
	// SkipProxyless disables the smart/direct dialer transport (DNS based, slow
	// and usually blocked in censored networks).
	SkipProxyless bool
	// UpstreamProxy is an optional HTTP/HTTPS/SOCKS5 proxy used to reach
	// control-plane endpoints (config fetch). When it is not set, the control
	// plane is reached directly.
	UpstreamProxy string
}

var clientOptions atomic.Value

func init() {
	clientOptions.Store(&ClientOptions{})
}

// SetClientOptions sets the options used when building the control-plane HTTP
// client. Passing nil is a no-op.
func SetClientOptions(o *ClientOptions) {
	if o == nil {
		return
	}
	clientOptions.Store(o)
}

// GetClientOptions returns the currently configured client options.
func GetClientOptions() *ClientOptions {
	return clientOptions.Load().(*ClientOptions)
}

// newUpstreamProxyRoundTripper builds an http.RoundTripper that reaches
// everything through the given HTTP/HTTPS/SOCKS5 proxy.
func newUpstreamProxyRoundTripper(rawurl string) (http.RoundTripper, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream proxy URL: %w", err)
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("unsupported upstream proxy scheme %q (use http, https or socks5)", u.Scheme)
	}
	return &http.Transport{
		Proxy:               http.ProxyURL(u),
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     45 * time.Second,
		ForceAttemptHTTP2:   true,
	}, nil
}

// newUpstreamProxyTransport wraps the proxy round tripper as a kindling
// transport so it participates in the control-plane transport race.
func newUpstreamProxyTransport(rawurl string) (kindling.Transport, error) {
	rt, err := newUpstreamProxyRoundTripper(rawurl)
	if err != nil {
		return nil, err
	}
	return &upstreamTransport{rt: rt}, nil
}

type upstreamTransport struct {
	rt http.RoundTripper
}

func (u *upstreamTransport) NewRoundTripper(_ context.Context, _ string) (http.RoundTripper, error) {
	return u.rt, nil
}

func (u *upstreamTransport) MaxLength() int     { return 0 }
func (u *upstreamTransport) IsStreamable() bool { return true }
func (u *upstreamTransport) Name() string       { return "upstream-proxy" }

// directTransport reaches the control plane over a plain direct TCP
// connection, without any proxy or tunnel.
type directTransport struct{}

func (directTransport) NewRoundTripper(_ context.Context, _ string) (http.RoundTripper, error) {
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     45 * time.Second,
		ForceAttemptHTTP2:   true,
	}, nil
}

func (directTransport) MaxLength() int     { return 0 }
func (directTransport) IsStreamable() bool { return true }
func (directTransport) Name() string       { return "direct" }
