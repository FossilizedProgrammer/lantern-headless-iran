package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pterm/pterm"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	commonconfig "github.com/getlantern/common/config"
	"github.com/getlantern/flashlight/v7"
	"github.com/getlantern/flashlight/v7/apipb"
	"github.com/getlantern/flashlight/v7/config"
)

// savedUserConfigName is the file the config service persists in the data
// folder; it contains the last fetched server list.
const savedUserConfigName = "user.conf"

// probeTimeout is how long each reachability probe waits.
const probeTimeout = 3 * time.Second

type serverInfo struct {
	Name        string `json:"name"`
	Address     string `json:"addr"`
	Protocol    string `json:"protocol"`
	CountryCode string `json:"country_code"`
	Country     string `json:"country"`
	City        string `json:"city"`
	Trusted     bool   `json:"trusted"`
	// Reachable is only set when a reachability check was requested: TCP 443
	// answered (true) or timed out (false, i.e. likely IP-blocked).
	Reachable *bool `json:"reachable,omitempty"`
}

func serversCmd(cmd *ServersCmd) {
	var proxies map[string]*commonconfig.ProxyConfig
	if cmd.Refresh {
		timeout := time.Duration(cmd.TimeoutSec) * time.Second
		pterm.Info.Printfln("Fetching server list from Lantern's config service (timeout: %s)...", timeout)
		fresh, stale, err := fetchServers(timeout)
		if err != nil {
			pterm.Error.Printfln("Fetching a fresh list failed: %s", err)
		}
		if stale {
			pterm.Warning.Println("Could not fetch a fresh list in time; showing the cached one")
		}
		proxies = fresh
	} else {
		saved, err := readSavedProxies()
		if err != nil {
			pterm.Error.Printfln("Failed to read cached server list: %s", err)
			os.Exit(1)
		}
		proxies = saved
	}

	if len(proxies) == 0 {
		pterm.Error.Println("No server list available yet.")
		pterm.Info.Println("Run `servers --refresh` to fetch one now, or run `start` once — the fetched list is cached in <data-path>/user.conf.")
		os.Exit(1)
	}

	infos := toServerInfos(proxies)
	if cmd.Check {
		infos = withReachability(infos, proxies, probeTimeout)
	}
	if cmd.JSON {
		b, err := json.MarshalIndent(infos, "", "  ")
		if err != nil {
			pterm.Error.Printfln("Failed to encode JSON: %s", err)
			os.Exit(1)
		}
		fmt.Println(string(b))
		return
	}
	printServers(infos)
}

// fetchServers runs the Lantern client just long enough to fetch a fresh
// server list through the control plane (upstream proxy or direct — same as
// `start`). It reports the first fetched list, falling back to
// the cached one (stale = true) when the fetch does not complete in time.
func fetchServers(timeout time.Duration) (proxies map[string]*commonconfig.ProxyConfig, stale bool, err error) {
	type proxyUpdate struct {
		proxies map[string]*commonconfig.ProxyConfig
		src     config.Source
	}
	updates := make(chan proxyUpdate, 8)
	readAuth()
	fc, ferr := newLantern(map[string]interface{}{}, flashlight.WithOnProxyConfigs(func(p map[string]*commonconfig.ProxyConfig, src config.Source) {
		select {
		case updates <- proxyUpdate{p, src}:
		default:
		}
	}))
	if ferr != nil {
		return nil, false, ferr
	}
	defer fc.Stop()

	go fc.Run("127.0.0.1:0", "127.0.0.1:0", nil, nil)

	var cached map[string]*commonconfig.ProxyConfig
	if saved, serr := readSavedProxies(); serr == nil {
		cached = saved
	}

	deadline := time.After(timeout)
	for {
		select {
		case u := <-updates:
			if u.src == config.Fetched {
				return u.proxies, false, nil
			}
		case <-deadline:
			if len(cached) > 0 {
				return cached, true, nil
			}
			return nil, false, errors.New("timed out waiting for the config service; try again or check --upstream-proxy setup")
		}
	}
}

// readSavedProxies loads the server list that a previous run persisted in
// <data-path>/user.conf. The file may be in protobuf or JSON format depending
// on the --readable-config/--sticky-config flags used by `start`.
func readSavedProxies() (map[string]*commonconfig.ProxyConfig, error) {
	path := filepath.Join(args.DataPath, savedUserConfigName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	conf := &apipb.ConfigResponse{}
	if uerr := proto.Unmarshal(data, conf); uerr != nil {
		conf = &apipb.ConfigResponse{}
		if jerr := protojson.Unmarshal(data, conf); jerr != nil {
			return nil, fmt.Errorf("unable to parse %s as protobuf (%v) or JSON (%v)", path, uerr, jerr)
		}
	}
	return legacyProxyMap(conf.GetProxy().GetProxies()), nil
}

// legacyProxyMap converts the wire-format proxy configs into the legacy
// commonconfig.ProxyConfig map used internally by flashlight.
func legacyProxyMap(proxies []*apipb.ProxyConnectConfig) map[string]*commonconfig.ProxyConfig {
	out := make(map[string]*commonconfig.ProxyConfig, len(proxies))
	for _, p := range proxies {
		if p == nil || p.GetName() == "" {
			continue
		}
		lc, err := apipb.ProxyToLegacyConfig(p)
		if err != nil {
			continue
		}
		out[p.GetName()] = lc
	}
	return out
}

// displayProtocol reports the transport used by a proxy, mirroring how
// chained.extractParams defaults an empty PluggableTransport.
func displayProtocol(cfg *commonconfig.ProxyConfig) string {
	if cfg.PluggableTransport != "" {
		return cfg.PluggableTransport
	}
	if cfg.Cert == "" {
		return "http"
	}
	return "https"
}

func toServerInfos(proxies map[string]*commonconfig.ProxyConfig) []serverInfo {
	infos := make([]serverInfo, 0, len(proxies))
	for name, cfg := range proxies {
		info := serverInfo{
			Name:     name,
			Address:  cfg.Addr,
			Protocol: displayProtocol(cfg),
			Trusted:  cfg.Trusted,
		}
		if cfg.Location != nil {
			info.City = cfg.Location.City
			info.Country = cfg.Location.Country
			info.CountryCode = cfg.Location.CountryCode
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].CountryCode != infos[j].CountryCode {
			return infos[i].CountryCode < infos[j].CountryCode
		}
		return infos[i].Name < infos[j].Name
	})
	return infos
}

func printServers(infos []serverInfo) {
	checked := false
	for _, info := range infos {
		if info.Reachable != nil {
			checked = true
			break
		}
	}
	headers := []string{"Name", "Address", "Protocol", "Country", "City"}
	if checked {
		headers = append(headers, "Reachable")
	}
	counts := map[string]int{}
	open := 0
	table := pterm.TableData{headers}
	for _, info := range infos {
		code := info.CountryCode
		if code == "" {
			code = "?"
		}
		counts[code]++
		row := []string{info.Name, info.Address, info.Protocol, strings.TrimSpace(code + " " + info.Country), info.City}
		if checked {
			if info.Reachable != nil && *info.Reachable {
				row = append(row, "open")
				open++
			} else {
				row = append(row, "unreachable")
			}
		}
		table = append(table, row)
	}
	_ = pterm.DefaultTable.WithHasHeader().WithData(table).Render()

	if checked {
		pterm.Info.Printfln("%d of %d servers directly reachable (TCP 443)", open, len(infos))
		if open < len(infos) {
			pterm.Info.Println("Unreachable servers are likely IP-blocked; they may still work through --upstream-proxy.")
		}
	}

	codes := make([]string, 0, len(counts))
	for code := range counts {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	parts := make([]string, 0, len(codes))
	for _, code := range codes {
		parts = append(parts, fmt.Sprintf("%s: %d", code, counts[code]))
	}
	pterm.Info.Printfln("%d servers in %d countries (%s)", len(infos), len(codes), strings.Join(parts, ", "))
}

// probeServers TCP-probes every server address in parallel and reports, per
// address, whether port 443 answered. A timeout is the signature of an
// IP-blocked server; an open port says nothing about the handshake layer,
// which only the real client dialer can verify.
func probeServers(proxies map[string]*commonconfig.ProxyConfig, timeout time.Duration) map[string]bool {
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		out = map[string]bool{}
	)
	sem := make(chan struct{}, 50)
	for _, cfg := range proxies {
		if cfg == nil || cfg.Addr == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(addr string) {
			defer func() {
				<-sem
				wg.Done()
			}()
			conn, err := net.DialTimeout("tcp", addr, timeout)
			if err == nil {
				_ = conn.Close()
			}
			mu.Lock()
			out[addr] = err == nil
			mu.Unlock()
		}(cfg.Addr)
	}
	wg.Wait()
	return out
}

// withReachability attaches probe results to the server infos.
func withReachability(infos []serverInfo, proxies map[string]*commonconfig.ProxyConfig, timeout time.Duration) []serverInfo {
	probe := probeServers(proxies, timeout)
	for i := range infos {
		if reachable, ok := probe[infos[i].Address]; ok {
			infos[i].Reachable = &reachable
		}
	}
	return infos
}

// pinningOptions builds flashlight options that restrict the client to a
// single server from the config, matched case-insensitively by name. All other
// servers are removed before any dialers are created, so the balancer has no
// choice but to use the pinned one.
func pinningOptions(server string) []flashlight.Option {
	var onceFound, onceMissing sync.Once
	return []flashlight.Option{
		flashlight.WithProxyFilter(serverFilter(strings.ToLower(strings.TrimSpace(server)))),
		flashlight.WithOnProxyConfigs(func(proxies map[string]*commonconfig.ProxyConfig, _ config.Source) {
			for name, cfg := range proxies {
				if strings.ToLower(name) != strings.ToLower(strings.TrimSpace(server)) {
					continue
				}
				onceFound.Do(func() {
					country, city := "", ""
					if cfg.Location != nil {
						country, city = cfg.Location.Country, cfg.Location.City
					}
					at := strings.Join(nonEmpty([]string{city, country}), ", ")
					if at == "" {
						pterm.Success.Printfln("Using pinned server %q", name)
					} else {
						pterm.Success.Printfln("Using pinned server %q (%s)", name, at)
					}
				})
				return
			}
			onceMissing.Do(func() {
				names := make([]string, 0, len(proxies))
				for name := range proxies {
					names = append(names, name)
				}
				sort.Strings(names)
				pterm.Warning.Printfln("Server %q is not in the current server list (available: %s). Waiting for a config that contains it...", server, strings.Join(names, ", "))
			})
		}),
	}
}

// serverFilter returns a proxy filter that keeps only the server whose name
// matches wanted, case-insensitively. It is the mechanism behind --server.
func serverFilter(wanted string) func(map[string]*commonconfig.ProxyConfig) map[string]*commonconfig.ProxyConfig {
	wanted = strings.ToLower(strings.TrimSpace(wanted))
	return func(proxies map[string]*commonconfig.ProxyConfig) map[string]*commonconfig.ProxyConfig {
		for name, cfg := range proxies {
			if strings.ToLower(name) == wanted {
				return map[string]*commonconfig.ProxyConfig{name: cfg}
			}
		}
		return map[string]*commonconfig.ProxyConfig{}
	}
}

func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}
