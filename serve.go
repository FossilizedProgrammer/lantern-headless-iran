package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pterm/pterm"

	commonconfig "github.com/getlantern/common/config"
	"github.com/getlantern/flashlight/v7"
	"github.com/getlantern/flashlight/v7/client"
	"github.com/getlantern/flashlight/v7/common"
	"github.com/getlantern/flashlight/v7/config"
	"github.com/getlantern/flashlight/v7/stats"

	"github.com/oldblocker/lantern-headless-iran/shared"
)

var spinner = pterm.DefaultSpinner.WithRemoveWhenDone(true).WithDelay(200 * time.Millisecond)

// lanternProxyReachable records whether at least one of Lantern's own proxy
// servers has answered. When none has, flashlight can still serve traffic
// through its proxyless smart dialer, but that traffic leaves this machine
// directly instead of through the Lantern network.
var lanternProxyReachable atomic.Bool

// newUserConfigData builds the user identity used for Lantern control-plane
// requests (config service, auth, etc.).
func newUserConfigData() common.UserConfig {
	return common.NewUserConfigData("lantern-headless", deviceId, userID, userToken, nil, "en-US")
}

// newLantern initializes flashlight with the options shared by all commands
// that talk to the Lantern network. Extra options customize callbacks such as
// proxy filtering.
func newLantern(flags map[string]interface{}, extra ...flashlight.Option) (*flashlight.Flashlight, error) {
	settings := newUserConfigData()
	return flashlight.New(
		"lantern-headless",
		shared.ApplicationVersion,
		shared.RevisionDate,
		args.DataPath,
		false,
		func() bool { return false },
		func() bool { return false },
		func() bool { return false },
		func() bool { return false },
		flags,
		settings,
		stats.NewTracker(),
		func() bool { return false },
		func() string { return "en-US" },
		func(host string) (string, error) {
			return host, nil
		},
		func(category, action, label string) {
		},
		extra...,
	)
}

func serve(cmd *ServeCmd) {
	var readyOnce, onOneProxy sync.Once
	// ready is closed as soon as a server list is available (fetched from the
	// control plane or loaded from disk), which is what allows the local proxy
	// to start. A working Lantern proxy is *not* required for that: when all of
	// Lantern's own servers are IP-blocked, flashlight still dials through its
	// proxyless smart dialer, and the data path check below says whether that
	// actually works.
	ready := make(chan struct{})
	var readyMsgMu sync.Mutex
	readyMsg := ""
	pterm.Info.Printfln("Starting Lantern... Log file: %s", logFilePath)
	opts := []flashlight.Option{
		flashlight.WithOnProxyConfigs(func(proxies map[string]*commonconfig.ProxyConfig, src config.Source) {
			if len(proxies) == 0 {
				return
			}
			readyOnce.Do(func() {
				readyMsgMu.Lock()
				if src == config.Fetched {
					readyMsg = fmt.Sprintf("Control plane reachable (config fetched: %d servers)", len(proxies))
				} else {
					readyMsg = fmt.Sprintf("Using the cached server list (%d servers); no fresh config yet", len(proxies))
				}
				readyMsgMu.Unlock()
				close(ready)
			})
		}),
		flashlight.WithOnSucceedingProxy(func() {
			// Only record it here; printing while the startup spinner is
			// running would erase the line. The data path check reports it.
			onOneProxy.Do(func() { lanternProxyReachable.Store(true) })
		}),
	}
	if cmd.Server != "" {
		opts = append(opts, pinningOptions(cmd.Server)...)
		pterm.Info.Printfln("Pinning the connection to server %q; all other servers are ignored", cmd.Server)
	}
	fc, err := newLantern(map[string]interface{}{
		"readableconfig": cmd.ReadableConfig,
		"stickyconfig":   cmd.StickyConfig,
	}, opts...)
	if err != nil {
		pterm.Error.Printfln("Failed to initialize Lantern. Error: %s", err)
		return
	}

	fc.Run(cmd.HTTPProxyAddress, cmd.SOCKSProxyAddress, func(cl *client.Client) {
		spin, _ := spinner.Start("Waiting for the config service…")
		defer spin.Stop()
		sa, ok := cl.Socks5Addr(5 * time.Second)
		if !ok {
			pterm.Error.Println("Failed to get proxy address")
			return
		}
		ha, ok := cl.Addr(5 * time.Second)
		if !ok {
			pterm.Error.Println("Failed to get proxy address")
			return
		}
		select {
		case <-ready:
		case <-time.After(30 * time.Second):
			spin.Stop()
			pterm.Error.Println("No server list available — the config service is unreachable.")
			if args.UpstreamProxy == "" {
				pterm.Info.Println("Hint: the config service appears to be blocked on this network. Re-run with --upstream-proxy socks5://127.0.0.1:<port> so the config fetch goes through a working proxy.")
			} else {
				pterm.Info.Printfln("Hint: --upstream-proxy %s is configured but the config service is still unreachable; check that the proxy is alive.", args.UpstreamProxy)
			}
			os.Exit(2)
		}
		spin.Stop()
		readyMsgMu.Lock()
		msg := readyMsg
		readyMsgMu.Unlock()
		if msg != "" {
			pterm.Success.Println(msg)
		}
		pterm.Info.Printfln("Local proxy is listening — SOCKS5: %v  HTTP: %v", sa, ha)
		pterm.Info.Printfln("Verifying the data path with a real request through that proxy...")
		go verifyDataPath(fmt.Sprintf("%v", ha))

	}, func(err error) {
		pterm.Error.Printfln("Failed to start Lantern, error: %s", err)
	})
}

// dataPathTargets are small, highly available endpoints that answer with 204.
// They are requested through the local proxy, exactly like a browser would.
var dataPathTargets = []string{
	"https://www.google.com/generate_204",
	"https://cp.cloudflare.com/generate_204",
}

// filteredTargets are sites that are usually blocked on censored networks.
// They are probed only to tell the user whether the local proxy opens blocked
// sites or only the ones the network already reaches.
var filteredTargets = []string{
	"https://x.com",
	"https://www.youtube.com",
}

// verifyDataPath performs a real request through the local proxy and reports
// whether browser traffic can actually leave this machine.
//
// The startup messages only guarantee that the ports are bound and that the
// config was fetched; without this check the user is told
// "ready" even when every server of the Lantern network is unreachable and
// every browser request is about to time out.
func verifyDataPath(httpAddr string) {
	const attempts = 3
	var lastErr error
	for i := 1; i <= attempts; i++ {
		for _, target := range dataPathTargets {
			start := time.Now()
			status, err := probeThroughProxy(httpAddr, target, 12*time.Second)
			if err == nil {
				pterm.Success.Printfln("Data path OK — HTTP %d from %s in %v", status, target, time.Since(start).Round(time.Millisecond))
				if egress, ipErr := egressIP(httpAddr, 8*time.Second); ipErr == nil && egress != "" {
					pterm.Info.Printfln("Traffic leaves this machine through IP %s", egress)
				}
				if !lanternProxyReachable.Load() {
					reportFilteredSites(httpAddr)
				}
				return
			}
			lastErr = err
		}
		if i < attempts {
			time.Sleep(5 * time.Second)
		}
	}

	pterm.Error.Printfln("Data path is NOT working: the ports above accept connections, but all %d test requests through the local proxy failed (last error: %v).",
		attempts*len(dataPathTargets), lastErr)
	pterm.Error.Printfln("Browser requests will fail or hang until this is fixed. Details: %s", logFilePath)
	if args.UpstreamProxy == "" {
		pterm.Info.Println("Hint: Lantern's own proxy servers look unreachable from this network (their IPs are usually blocked). Re-run with --upstream-proxy socks5://127.0.0.1:<port-of-a-working-proxy>; the connections to Lantern's servers then travel through that proxy while browser traffic still exits from the Lantern network.")
	} else {
		pterm.Info.Printfln("Hint: --upstream-proxy %s is configured but did not help either; check that this upstream proxy is alive and can reach the Lantern servers.", args.UpstreamProxy)
	}
}

// reportFilteredSites probes sites that censored networks usually block and
// tells the user plainly whether the local proxy opens them. Without this,
// "Data path OK" is misleading: google.com/generate_204 is often reachable
// even on a heavily filtered network, so a green line does not mean a
// browser will open blocked pages.
func reportFilteredSites(httpAddr string) {
	for _, target := range filteredTargets {
		if _, err := probeThroughProxy(httpAddr, target, 15*time.Second); err == nil {
			pterm.Success.Printfln("Blocked-site check: %s opened through the local proxy", target)
			return
		}
	}
	pterm.Warning.Println("The local proxy cannot open blocked sites (x.com and youtube.com both failed), because no Lantern proxy is reachable and the direct smart dialer cannot bypass the filter. Use --upstream-proxy <socks5://127.0.0.1:port> for that.")
}

// proxyClient builds an HTTP client that reaches the network through the local
// proxy at httpAddr.
func proxyClient(httpAddr string, timeout time.Duration) (*http.Client, func(), error) {
	proxyURL, err := url.Parse("http://" + httpAddr)
	if err != nil {
		return nil, nil, err
	}
	transport := &http.Transport{
		Proxy:             http.ProxyURL(proxyURL),
		DisableKeepAlives: true,
	}
	return &http.Client{Timeout: timeout, Transport: transport}, transport.CloseIdleConnections, nil
}

// probeThroughProxy requests target through the local proxy and returns the
// status code. Only 204 counts as success: a block page or a captive portal
// answering 200/30x would otherwise be reported as a working data path.
func probeThroughProxy(httpAddr, target string, timeout time.Duration) (int, error) {
	client, cleanup, err := proxyClient(httpAddr, timeout)
	if err != nil {
		return 0, err
	}
	defer cleanup()
	resp, err := client.Get(target)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusNoContent {
		return resp.StatusCode, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, target)
	}
	return resp.StatusCode, nil
}

// egressIP reports the public IP seen by the destination, which tells the user
// whether traffic really leaves through a proxy instead of directly.
func egressIP(httpAddr string, timeout time.Duration) (string, error) {
	client, cleanup, err := proxyClient(httpAddr, timeout)
	if err != nil {
		return "", err
	}
	defer cleanup()
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
