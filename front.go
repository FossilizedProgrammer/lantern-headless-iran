package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getlantern/fronted"
	"github.com/goccy/go-yaml"
	"github.com/pterm/pterm"
)

type frontPair struct {
	IP  string `json:"ip"`
	SNI string `json:"sni"`
}

type frontPairsFile struct {
	Pairs []frontPair `json:"pairs"`
}

type vettedPair struct {
	frontPair
	passed  bool
	avgMs   float64
	okCount int
}

func frontCmd(cmd *FrontCmd) {
	switch {
	case cmd.Import != nil:
		frontImport(cmd.Import)
	case cmd.Scan != nil:
		frontScan(cmd.Scan)
	case cmd.Test != nil:
		frontTest(cmd.Test)
	default:
		printHelp()
		os.Exit(2)
	}
}

// ─── front import ────────────────────────────────────────────────

func frontImport(cmd *FrontImportCmd) {
	pairs, err := loadFrontPairs(cmd)
	if err != nil {
		pterm.Error.Printfln("Failed to load pairs: %s", err)
		os.Exit(1)
	}
	if len(pairs) == 0 {
		pterm.Error.Println("No IP/SNI pairs to test. Use --pairs or --ips/--snis")
		os.Exit(2)
	}
	if cmd.Repeats < 1 {
		cmd.Repeats = 1
	}
	if cmd.MinOK < 1 {
		cmd.MinOK = 1
	}
	if cmd.MinOK > cmd.Repeats {
		cmd.MinOK = cmd.Repeats
	}
	if cmd.Workers < 1 {
		cmd.Workers = 1
	}
	timeout := time.Duration(cmd.TimeoutSec) * time.Second

	base := fronted.EmbeddedConfigBytes()
	if base == nil {
		pterm.Error.Println("Could not read embedded fronting configuration")
		os.Exit(1)
	}
	pool, providers, err := fronted.ProcessConfig(base)
	if err != nil {
		pterm.Error.Printfln("Could not parse embedded fronting configuration: %s", err)
		os.Exit(1)
	}
	provider, ok := providers[cmd.Provider]
	if !ok {
		pterm.Error.Printfln("Unknown provider %q (available: %s)", cmd.Provider, strings.Join(providerNames(providers), ", "))
		os.Exit(2)
	}
	if provider.TestURL == "" {
		pterm.Error.Printfln("Provider %q has no test URL", cmd.Provider)
		os.Exit(1)
	}
	pterm.Info.Printfln("Testing %d pairs against %s (vet: %s)", len(pairs), provider.TestURL, testURLHost(provider.TestURL))

	spinnerText := fmt.Sprintf("Vetting 0/%d...", len(pairs))
	sp, _ := pterm.DefaultSpinner.WithText(spinnerText).Start()
	var (
		wg        sync.WaitGroup
		completed atomic.Int64
	)
	sem := make(chan struct{}, cmd.Workers)
	results := make([]vettedPair, len(pairs))
	for i := range pairs {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer func() {
				<-sem
				wg.Done()
			}()
			results[idx] = vetPair(pairs[idx], pool, provider.TestURL, cmd.Repeats, cmd.MinOK, timeout)
			if n := completed.Add(1); n%50 == 0 {
				sp.UpdateText(fmt.Sprintf("Vetting %d/%d...", n, len(pairs)))
			}
		}(i)
	}
	wg.Wait()
	_ = sp.Stop()

	var passed []vettedPair
	for _, r := range results {
		if r.passed {
			passed = append(passed, r)
		}
	}
	sort.Slice(passed, func(i, j int) bool { return passed[i].avgMs < passed[j].avgMs })
	if cmd.Top > 0 && len(passed) > cmd.Top {
		passed = passed[:cmd.Top]
	}
	pterm.Info.Printfln("Passed %d/%d pairs (repeats=%d, min-ok=%d)", len(passed), len(pairs), cmd.Repeats, cmd.MinOK)
	if len(passed) == 0 {
		pterm.Error.Println("No pair worked — nothing written. Check the scanner output or run from the target network.")
		os.Exit(1)
	}

	outPath := cmd.Out
	if outPath == "" {
		outPath = defaultFrontedConfigPath()
	}
	gz, err := buildFrontedConfig(base, cmd.Provider, passed, cmd.KeepOriginal)
	if err != nil {
		pterm.Error.Printfln("Failed to build fronting config: %s", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outPath, gz, 0644); err != nil {
		pterm.Error.Printfln("Failed to write %s: %s", outPath, err)
		os.Exit(1)
	}
	pterm.Success.Printfln("Wrote %s (%d masquerades for provider %q)", outPath, len(passed), cmd.Provider)
	if len(passed) > 0 {
		pterm.Info.Printfln("Latency: best %.0fms, worst %.0fms", passed[0].avgMs, passed[len(passed)-1].avgMs)
	}
	pterm.Info.Printfln("Run `lantern start` (auto-detects this file) or use --fronted-config %s", outPath)
}

func loadFrontPairs(cmd *FrontImportCmd) ([]frontPair, error) {
	seen := make(map[string]bool)
	var pairs []frontPair
	add := func(ip, sni string) {
		ip = strings.TrimSpace(ip)
		sni = strings.TrimSpace(sni)
		if ip == "" || sni == "" {
			return
		}
		key := ip + "|" + sni
		if seen[key] {
			return
		}
		seen[key] = true
		pairs = append(pairs, frontPair{IP: ip, SNI: sni})
	}

	if cmd.Pairs != "" {
		data, err := os.ReadFile(cmd.Pairs)
		if err != nil {
			return nil, err
		}
		var pf frontPairsFile
		if err := json.Unmarshal(data, &pf); err != nil {
			// Also accept a bare array of pairs.
			var arr []frontPair
			if err2 := json.Unmarshal(data, &arr); err2 != nil {
				return nil, fmt.Errorf("invalid JSON in %s: %w", cmd.Pairs, err)
			}
			pf.Pairs = arr
		}
		for _, p := range pf.Pairs {
			add(p.IP, p.SNI)
		}
	}

	if cmd.IPs != "" || cmd.SNIs != "" {
		if cmd.IPs == "" || cmd.SNIs == "" {
			return nil, fmt.Errorf("--ips and --snis must be used together")
		}
		ips, err := readLines(cmd.IPs)
		if err != nil {
			return nil, err
		}
		snis, err := readLines(cmd.SNIs)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			for _, sni := range snis {
				add(ip, sni)
			}
		}
	}
	return pairs, nil
}

func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

func vetPair(p frontPair, pool *x509.CertPool, testURL string, repeats, minOK int, timeout time.Duration) vettedPair {
	m := &fronted.Masquerade{Domain: p.SNI, IpAddress: p.IP}
	res := vettedPair{frontPair: p}
	var totalMs float64
	for i := 0; i < repeats; i++ {
		ok, ms := vetOnce(m, pool, testURL, timeout)
		if ok {
			res.okCount++
			totalMs += ms
		}
		if res.okCount >= minOK {
			break
		}
	}
	res.passed = res.okCount >= minOK
	if res.okCount > 0 {
		res.avgMs = totalMs / float64(res.okCount)
	}
	return res
}

// vetOnce runs a single vetting attempt with an overall timeout.
func vetOnce(m *fronted.Masquerade, pool *x509.CertPool, testURL string, timeout time.Duration) (bool, float64) {
	type result struct{ ok bool }
	ch := make(chan result, 1)
	start := time.Now()
	go func() {
		ch <- result{fronted.Vet(m, pool, testURL)}
	}()
	select {
	case r := <-ch:
		return r.ok, float64(time.Since(start).Microseconds()) / 1000
	case <-time.After(timeout):
		return false, float64(timeout.Milliseconds())
	}
}

// buildFrontedConfig starts from the embedded configuration, replaces the
// masquerades of the given provider with the validated pairs and drops the
// (dead) CloudFront masquerades.
func buildFrontedConfig(base []byte, providerID string, passed []vettedPair, keepOriginal bool) ([]byte, error) {
	raw, err := gunzip(base)
	if err != nil {
		return nil, fmt.Errorf("decompress base config: %w", err)
	}
	var root map[string]interface{}
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parse base config: %w", err)
	}
	providers, _ := root["providers"].(map[string]interface{})
	if providers == nil {
		return nil, fmt.Errorf("base config has no providers section")
	}

	if target, ok := providers[providerID].(map[string]interface{}); ok {
		var masqs []interface{}
		if keepOriginal {
			if orig, ok := target["masquerades"].([]interface{}); ok {
				masqs = append(masqs, orig...)
			}
		}
		for _, p := range passed {
			masqs = append(masqs, map[string]interface{}{
				"domain":         p.SNI,
				"ipaddress":      p.IP,
				"sni":            "",
				"verifyhostname": nil,
			})
		}
		target["masquerades"] = masqs
	}

	// CloudFront no longer allows domain fronting (421 Misdirected Request),
	// so its masquerades would only waste vetting time.
	if cf, ok := providers["cloudfront"].(map[string]interface{}); ok {
		cf["masquerades"] = []interface{}{}
	}

	// The configuration's trusted CAs predate DigiCert's root rotation, so
	// Akamai's current chain (DigiCert Global Root G3) would fail to verify
	// on systems without a system trust store.
	ensureTrustedCA(root, "DigiCert Global Root G3", digicertGlobalRootG3PEM)

	out, err := yaml.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	return gzipBytes(out)
}

// ensureTrustedCA appends a root certificate to the config's trustedcas list
// when it is not already present.
func ensureTrustedCA(root map[string]interface{}, commonName, pemCert string) {
	cas, _ := root["trustedcas"].([]interface{})
	for _, entry := range cas {
		if m, ok := entry.(map[string]interface{}); ok && m["commonname"] == commonName {
			return
		}
	}
	root["trustedcas"] = append(cas, map[string]interface{}{
		"commonname": commonName,
		"cert":       pemCert,
	})
}

const digicertGlobalRootG3PEM = `-----BEGIN CERTIFICATE-----
MIICPzCCAcWgAwIBAgIQBVVWvPJepDU1w6QP1atFcjAKBggqhkjOPQQDAzBhMQsw
CQYDVQQGEwJVUzEVMBMGA1UEChMMRGlnaUNlcnQgSW5jMRkwFwYDVQQLExB3d3cu
ZGlnaWNlcnQuY29tMSAwHgYDVQQDExdEaWdpQ2VydCBHbG9iYWwgUm9vdCBHMzAe
Fw0xMzA4MDExMjAwMDBaFw0zODAxMTUxMjAwMDBaMGExCzAJBgNVBAYTAlVTMRUw
EwYDVQQKEwxEaWdpQ2VydCBJbmMxGTAXBgNVBAsTEHd3dy5kaWdpY2VydC5jb20x
IDAeBgNVBAMTF0RpZ2lDZXJ0IEdsb2JhbCBSb290IEczMHYwEAYHKoZIzj0CAQYF
K4EEACIDYgAE3afZu4q4C/sLfyHS8L6+c/MzXRq8NOrexpu80JX28MzQC7phW1FG
fp4tn+6OYwwX7Adw9c+ELkCDnOg/QW07rdOkFFk2eJ0DQ+4QE2xy3q6Ip6FrtUPO
Z9wj/wMco+I+o0IwQDAPBgNVHRMBAf8EBTADAQH/MA4GA1UdDwEB/wQEAwIBhjAd
BgNVHQ4EFgQUs9tIpPmhxdiuNkHMEWNpYim8S8YwCgYIKoZIzj0EAwMDaAAwZQIx
AK288mw/EkrRLTnDCgmXc/SINoyIJ7vmiI1Qhadj+Z4y3maTD/HMsQmP3Wyr+mt/
oAIwOWZbwmSNuJ5Q3KjVSaLtx9zRSX8XAbjIho9OjIgrqJqpisXRAL34VOKa5Vt8
sycX
-----END CERTIFICATE-----
`

// ─── front test ──────────────────────────────────────────────────

func frontTest(cmd *FrontTestCmd) {
	cfgPath := cmd.Config
	if cfgPath == "" {
		if _, err := os.Stat(defaultFrontedConfigPath()); err == nil {
			cfgPath = defaultFrontedConfigPath()
		}
	}
	opts := []fronted.Option{
		fronted.WithCacheFile(frontTestCachePath()),
		fronted.WithHTTPClient(&http.Client{Timeout: 20 * time.Second}),
	}
	if cfgPath != "" {
		opts = append(opts, fronted.WithLocalConfig(cfgPath))
		pterm.Info.Printfln("Fronting config: %s", cfgPath)
	} else {
		pterm.Info.Println("Fronting config: embedded (run `lantern front import` to add scanned masquerades)")
	}
	f := fronted.NewFronted(opts...)
	defer f.Close()

	timeout := time.Duration(cmd.TimeoutSec) * time.Second
	exitCode := 0

	// 1. Global (cloud) config via fronting.
	pterm.Println()
	pterm.DefaultHeader.Println("Test 1: GET globalconfig.flashlightproxy.com/global.yaml.gz")
	status, latency, body, errStr := frontGet(f, "globalconfig.flashlightproxy.com", "https://globalconfig.flashlightproxy.com/global.yaml.gz", timeout)
	gzipMagic := len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b
	switch {
	case status == 200 && gzipMagic:
		pterm.Success.Printfln("PASS — HTTP 200, %d bytes, %.0fms", len(body), latency)
	case status > 0:
		pterm.Warning.Printfln("UNEXPECTED — HTTP %d, %d bytes, %.0fms", status, len(body), latency)
		exitCode = 1
	default:
		pterm.Error.Printfln("FAILED — %s", errStr)
		exitCode = 1
	}

	// 2. Proxy config endpoint (df.iantem.io) via fronting. Any HTTP response
	// from Lantern's origin proves the front works; transport errors do not.
	pterm.Println()
	pterm.DefaultHeader.Println("Test 2: POST df.iantem.io/api/v1/config")
	pStatus, pLatency, _, pDetail, pBody := frontPostJSON(f, "df.iantem.io", "https://df.iantem.io/api/v1/config", timeout)
	originReached := pStatus == 200 || strings.Contains(pBody, "proto") || strings.Contains(pBody, "invalid request body") ||
		strings.Contains(pBody, "bad client factors")
	switch {
	case originReached:
		pterm.Success.Printfln("PASS — Lantern origin reachable (HTTP %d, %.0fms)", pStatus, pLatency)
	case pStatus > 0:
		pterm.Warning.Printfln("REACHABLE BUT UNEXPECTED — HTTP %d, %.0fms, body: %s", pStatus, pLatency, truncate(pBody, 80))
		exitCode = 1
	default:
		pterm.Error.Printfln("FAILED — %s", pDetail)
		exitCode = 1
	}

	pterm.Println()
	if exitCode == 0 {
		pterm.Success.Println("Domain fronting is working")
	} else {
		pterm.Error.Println("Domain fronting is not working — run `lantern front import` with fresh scanner results")
	}
	os.Exit(exitCode)
}

func frontGet(f fronted.Fronted, addr, url string, timeout time.Duration) (int, float64, []byte, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	rt, err := f.NewConnectedRoundTripper(ctx, addr+":443")
	if err != nil {
		return 0, 0, nil, err.Error()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, nil, err.Error()
	}
	start := time.Now()
	resp, err := rt.RoundTrip(req)
	latency := float64(time.Since(start).Milliseconds())
	if err != nil {
		return 0, latency, nil, err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, latency, body, firstLine(resp.Status)
}

func frontPostJSON(f fronted.Fronted, addr, url string, timeout time.Duration) (int, float64, int, string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	rt, err := f.NewConnectedRoundTripper(ctx, addr+":443")
	if err != nil {
		return 0, 0, 0, err.Error(), ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(`{"probe":"lantern-front-test"}`))
	if err != nil {
		return 0, 0, 0, err.Error(), ""
	}
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := rt.RoundTrip(req)
	latency := float64(time.Since(start).Milliseconds())
	if err != nil {
		return 0, latency, 0, err.Error(), ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, latency, len(body), firstLine(resp.Status), string(body)
}

// ─── helpers ─────────────────────────────────────────────────────

func providerNames(providers map[string]*fronted.Provider) []string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func testURLHost(testURL string) string {
	if i := strings.Index(testURL, "://"); i >= 0 {
		rest := testURL[i+3:]
		if j := strings.IndexAny(rest, "/:"); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	return testURL
}

func gunzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func gzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func frontTestCachePath() string {
	return filepath.Join(os.TempDir(), "lantern-front-test-cache.json")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
