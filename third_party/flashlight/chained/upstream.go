package chained

import (
	"context"
	"net"

	"github.com/getlantern/netx"

	"github.com/getlantern/flashlight/v7/common"
)

// dialProxyServer opens the raw TCP connection to a Lantern proxy server.
//
// When an upstream proxy is configured (--upstream-proxy), the connection to
// the proxy server itself is opened through it. This is the difference between
// "the proxy server's IP is blocked on this network" and "the proxy does not
// work": only the short-lived connection to the proxy server travels through
// the upstream, while user traffic still runs through Lantern's own transport
// (tlsmasq/https/...) on top of it and exits from the Lantern network.
//
// If the upstream cannot reach the proxy server we fall back to dialing
// directly, so configuring an upstream never makes a working direct path
// worse.
func dialProxyServer(ctx context.Context, network, addr string) (net.Conn, error) {
	if up := common.UpstreamDialer(); up != nil {
		conn, err := up(ctx, network, addr)
		if err == nil {
			log.Debugf("Dialed proxy server %s through upstream proxy", addr)
			return conn, nil
		}
		log.Debugf("Upstream proxy could not reach proxy server %s, dialing directly: %v", addr, err)
	}
	return netx.DialContext(ctx, network, addr)
}
