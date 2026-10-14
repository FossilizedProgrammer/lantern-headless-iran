package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"google.golang.org/protobuf/proto"

	commonconfig "github.com/getlantern/common/config"
	"github.com/getlantern/flashlight/v7/apipb"
	"github.com/getlantern/flashlight/v7/common"

	"github.com/oldblocker/lantern-headless-iran/shared"
)

// Transport methods for `update --via`.
const (
	viaAuto     = "auto"
	viaUpstream = "upstream"
	viaDirect   = "direct"
)

// maxConfigResponseBytes bounds how much of the config response is read.
const maxConfigResponseBytes = 64 << 20

func updateServersCmd(cmd *UpdateCmd) {
	via := strings.ToLower(strings.TrimSpace(cmd.Via))
	if via == viaUpstream && args.UpstreamProxy == "" {
		pterm.Error.Println("--via upstream requires --upstream-proxy (e.g. socks5://127.0.0.1:1080)")
		os.Exit(2)
	}
	plan, err := updatePlan(via)
	if err != nil {
		pterm.Error.Printfln("%s", err)
		os.Exit(2)
	}

	timeout := time.Duration(cmd.TimeoutSec) * time.Second
	cfgURL := cmd.ConfigURL
	if cfgURL == "" {
		cfgURL = common.UserConfigURL
	}

	readAuth()

	// flashight.New normally initializes the version factors that the config
	// service validates (X-Lantern-App-Version and friends); update runs
	// without flashlight, so do it here to avoid "bad client factors" 400s.
	common.InitVersion(shared.ApplicationVersion)

	uc := newUserConfigData()

	pterm.Info.Printfln("Updating the server list from %s (timeout per attempt: %s)", cfgURL, timeout)

	out, lastErr := tryPlan(plan, cfgURL, uc, timeout)
	if !out.ok {
		pterm.Error.Printfln("Could not update the server list: %s", lastErr)
		pterm.Info.Println("Hint: pass --upstream-proxy socks5://127.0.0.1:<port-of-a-working-proxy> " +
			"to fetch through a proxy that already works on your network.")
		os.Exit(1)
	}

	if out.unchanged {
		printUpdated(cmd, nil, out.via, true)
		return
	}
	proxies := legacyProxyMap(out.conf.GetProxy().GetProxies())
	if serr := saveUserConfig(out.conf); serr != nil {
		pterm.Error.Printfln("Fetched %d servers but failed to save the list: %s", len(proxies), serr)
		os.Exit(1)
	}
	printUpdated(cmd, proxies, out.via, false)
}

// updateOutcome is the result of trying a plan of transports.
type updateOutcome struct {
	conf      *apipb.ConfigResponse
	unchanged bool
	via       string
	ok        bool
}

// tryPlan runs the transports in order and stops at the first success.
func tryPlan(plan []string, cfgURL string, uc common.UserConfig, timeout time.Duration) (updateOutcome, error) {
	var lastErr error
	for _, method := range plan {
		pterm.Info.Printfln("Trying via %s...", method)
		conf, unchanged, err := fetchServerConfig(method, cfgURL, uc, timeout)
		if err != nil {
			lastErr = err
			pterm.Warning.Printfln("Via %s failed: %s", method, err)
			continue
		}
		if unchanged {
			return updateOutcome{unchanged: true, via: method, ok: true}, nil
		}
		if len(legacyProxyMap(conf.GetProxy().GetProxies())) == 0 {
			lastErr = errors.New("the config service returned no servers; keeping the cached list")
			pterm.Warning.Println(lastErr.Error())
			continue
		}
		return updateOutcome{conf: conf, via: method, ok: true}, nil
	}
	return updateOutcome{}, lastErr
}

// updatePlan turns --via into an ordered list of transports to try.
func updatePlan(via string) ([]string, error) {
	switch via {
	case "", viaAuto:
		plan := []string{}
		if args.UpstreamProxy != "" {
			plan = append(plan, viaUpstream)
		}
		return append(plan, viaDirect), nil
	case viaUpstream, viaDirect:
		return []string{via}, nil
	default:
		return nil, fmt.Errorf("unknown method %q (use upstream, direct or auto)", via)
	}
}

// fetchServerConfig asks Lantern's config service for a fresh server list over
// the given transport. It mirrors what flashlight's config service does on
// startup: a POST with a protobuf ConfigRequest that returns a protobuf
// ConfigResponse (204 means "no changes").
func fetchServerConfig(via, cfgURL string, uc common.UserConfig, timeout time.Duration) (*apipb.ConfigResponse, bool, error) {
	var client *http.Client
	switch via {
	case viaDirect:
		client = directClient(timeout)
	case viaUpstream:
		c, err := upstreamClient(args.UpstreamProxy, timeout)
		if err != nil {
			return nil, false, err
		}
		client = c
	default:
		return nil, false, fmt.Errorf("unknown transport %q", via)
	}

	req, err := buildConfigRequest(cfgURL, uc, savedProxyNames())
	if err != nil {
		return nil, false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil, true, nil
	case http.StatusOK:
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxConfigResponseBytes))
		if err != nil {
			return nil, false, err
		}
		conf := &apipb.ConfigResponse{}
		if uerr := proto.Unmarshal(body, conf); uerr != nil {
			return nil, false, fmt.Errorf("the config service answered, but not with a Lantern config: %w", uerr)
		}
		return conf, false, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, false, fmt.Errorf("HTTP %d from the config service — run `lantern auth login` first or check your credentials", resp.StatusCode)
	default:
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, false, fmt.Errorf("HTTP %d from the config service: %s", resp.StatusCode, truncate(string(snippet), 120))
	}
}

// buildConfigRequest constructs the POST that fetches a fresh server list,
// identical to the one flashlight's config service sends.
func buildConfigRequest(cfgURL string, uc common.UserConfig, proxyNames []string) (*http.Request, error) {
	confReq := &apipb.ConfigRequest{
		ClientInfo: &apipb.ConfigRequest_ClientInfo{
			FlashlightVersion: common.LibraryVersion,
			ClientVersion:     shared.ApplicationVersion,
			UserId:            strconv.FormatInt(uc.GetUserID(), 10),
			ProToken:          uc.GetToken(),
		},
		Proxy: &apipb.ConfigRequest_Proxy{
			Names: proxyNames,
		},
	}
	buf, err := proto.Marshal(confReq)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal config request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, cfgURL, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	common.AddCommonHeaders(uc, req)
	req.Header.Set("Content-Type", "application/x-protobuf")
	// Prevents intermediate proxies from caching the content.
	req.Header.Set("Cache-Control", "no-cache")
	req.Close = true
	return req, nil
}

// savedProxyNames returns the names of the currently cached servers, so the
// config service knows what the client already has (same as flashlight does).
func savedProxyNames() []string {
	saved, err := readSavedProxies()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(saved))
	for name := range saved {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// directClient talks to the config service directly, with no circumvention.
func directClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: args.Insecure}, // opted in via --insecure
		},
	}
}

// upstreamClient talks to the config service through --upstream-proxy
// (http://, https:// or socks5://), like the auth client does.
func upstreamClient(proxy string, timeout time.Duration) (*http.Client, error) {
	u, err := url.Parse(proxy)
	if err != nil {
		return nil, fmt.Errorf("invalid --upstream-proxy %q: %w", proxy, err)
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("unsupported --upstream-proxy scheme %q (use http, https or socks5)", u.Scheme)
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(u),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: args.Insecure}, // opted in via --insecure
		},
	}, nil
}

// saveUserConfig persists the fetched server list to <data-path>/user.conf in
// the same protobuf format flashlight uses, so `start` and `servers` pick it
// up as the cached list.
func saveUserConfig(conf *apipb.ConfigResponse) error {
	data, err := proto.Marshal(conf)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(args.DataPath, ".user.conf-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName) // no-op after a successful rename
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0600); err != nil {
		return err
	}
	return os.Rename(tmpName, userConfigPath())
}

func userConfigPath() string {
	return filepath.Join(args.DataPath, savedUserConfigName)
}

// truncate shortens s to at most max runes, appending an ellipsis when cut.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// printUpdated reports the outcome of an update, as a table or JSON.
func printUpdated(cmd *UpdateCmd, proxies map[string]*commonconfig.ProxyConfig, method string, unchanged bool) {
	infos := toServerInfos(proxies)
	if cmd.Check && len(infos) > 0 {
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
	if unchanged {
		pterm.Success.Printfln("Server list is already up to date (checked via %s)", method)
	} else {
		pterm.Success.Printfln("Server list updated via %s and saved to %s", method, userConfigPath())
	}
	printServers(infos)
}
