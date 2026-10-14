package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pterm/pterm"

	"github.com/getlantern/flashlight/v7"
	"github.com/getlantern/flashlight/v7/client"
	"github.com/getlantern/flashlight/v7/common"
	"github.com/getlantern/flashlight/v7/stats"

	"github.com/oldblocker/lantern-headless-iran/shared"
)

var spinner = pterm.DefaultSpinner.WithRemoveWhenDone(true).WithDelay(200 * time.Millisecond)

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
	var onOneProxy sync.Once
	proxyReady := make(chan struct{})
	pterm.Info.Printfln("Starting Lantern... Log file: %s", logFilePath)
	opts := []flashlight.Option{
		flashlight.WithOnSucceedingProxy(func() {
			onOneProxy.Do(func() {
				// This only reports the control plane (fetching Lantern's config via
				// the upstream proxy or directly). It says nothing about the data path
				// that browser traffic uses, which is verified separately below.
				pterm.Success.Printfln("Control plane reachable (config fetched successfully)")
				proxyReady <- struct{}{}
			})
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
		spin, _ := spinner.Start("Waiting for proxy to be ready")
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
		case <-proxyReady:
			break
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
