package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getlantern/fronted"
	"github.com/goccy/go-yaml"
	"github.com/pterm/pterm"
)

// ─── front scan ──────────────────────────────────────────────────
//
// The built-in IP scanner finds working domain-fronting endpoints on the CDN
// of the given provider: it walks IP ranges, completes TLS handshakes that
// present the provider's masquerade domains as SNI, and vets the surviving
// IP/SNI pairs end-to-end with the same routine as `front import`. Its output
// feeds `front import` or, with --build, directly becomes the local fronting
// config used by `update --via fronting` and `start`.

// defaultAkamaiRanges is a set of well-known Akamai IPv4 blocks, used when no
// ranges are given. They are far larger than a default scan needs, so
// --max-ips samples a bounded number of IPs from them.
var defaultAkamaiRanges = []string{
	"2.16.0.0/13",
	"23.32.0.0/11",
	"23.64.0.0/14",
	"88.221.0.0/16",
	"92.122.0.0/15",
	"95.100.0.0/15",
	"96.6.0.0/15",
	"104.64.0.0/10",
	"184.24.0.0/13",
	"184.50.0.0/15",
	"184.84.0.0/14",
}

// defaultAkamaiSNIs are Akamai-shared edge hostnames beyond the config's own
// masquerade domain (a248.e.akamai.net). The well-known one is a frequent
// SNI-blocking target — when it gets reset, the same edges still serve and
// front through these.
var defaultAkamaiSNIs = []string{
	"a.akamaized.net",
	"a.akamaihd.net",
}

const defaultScanProvider = "akamai"

const (
	defaultScanWorkers       = 300
	defaultScanTCPTimeoutSec = 2
	defaultScanTLSTimeoutSec = 5
	defaultScanVetTimeoutSec = 15
)

// maxScannableIPs guards against expanding a range so large that enumeration
// alone would exhaust memory and time (an IPv4 /10 already holds 4M IPs).
const maxScannableIPs = 16_000_000

// scanPairsName is where `front scan` writes its pairs when --out is not set.
const scanPairsName = "scan-pairs.json"

// usageError marks a scan pipeline failure caused by bad input (unknown
// provider, no SNIs, ...) as opposed to a runtime/scan failure.
type usageError struct{ err error }

func (u *usageError) Error() string { return u.err.Error() }

// scanOptions is the input to the scan pipeline, shared by the `front scan`
// command and the automatic fallback scan inside `update`.
type scanOptions struct {
	Provider      string
	Ranges        string
	CIDRsFile     string
	SNIs          string
	SNIsFile      string
	Port          int
	Workers       int
	TCPTimeoutSec int
	TLSTimeoutSec int
	TimeoutSec    int
	Repeats       int
	MinOK         int
	MaxIPs        int
	Top           int
}

type scanParams struct {
	port       int
	tcpTimeout time.Duration
	tlsTimeout time.Duration
	vetTimeout time.Duration
	repeats    int
	minOK      int
	pool       *x509.CertPool // CA pool used by the end-to-end fronting vet
	tlsRoots   *x509.CertPool // CA pool used by the scan's TLS handshake probe
	testURL    string
}

type scanResult struct {
	vettedPair
	tlsMs float64
}

type scanStats struct {
	total int64
	tcpOK int64
	tlsOK int64
	vetOK int64
}

// runScanPipeline resolves the provider, SNIs and ranges, scans the IPs and
// returns the passing pairs sorted by measured latency (Top applied when > 0).
// It contains everything the scanner does except command-specific output.
func runScanPipeline(opts *scanOptions) ([]scanResult, scanStats, error) {
	if opts.Repeats < 1 {
		opts.Repeats = 1
	}
	if opts.MinOK < 1 {
		opts.MinOK = 1
	}
	if opts.MinOK > opts.Repeats {
		opts.MinOK = opts.Repeats
	}
	if opts.Workers < 1 {
		opts.Workers = defaultScanWorkers
	}
	if opts.Port < 1 || opts.Port > 65535 {
		opts.Port = 443
	}
	if opts.TCPTimeoutSec < 1 {
		opts.TCPTimeoutSec = defaultScanTCPTimeoutSec
	}
	if opts.TLSTimeoutSec < 1 {
		opts.TLSTimeoutSec = defaultScanTLSTimeoutSec
	}
	if opts.TimeoutSec < 1 {
		opts.TimeoutSec = defaultScanVetTimeoutSec
	}
	if opts.MaxIPs < 0 {
		opts.MaxIPs = 0
	}
	if opts.Top < 0 {
		opts.Top = 0
	}
	providerID := opts.Provider
	if providerID == "" {
		providerID = defaultScanProvider
	}

	base := fronted.EmbeddedConfigBytes()
	if base == nil {
		return nil, scanStats{}, errors.New("could not read embedded fronting configuration")
	}
	pool, providers, err := fronted.ProcessConfig(base)
	if err != nil {
		return nil, scanStats{}, fmt.Errorf("could not parse embedded fronting configuration: %w", err)
	}
	provider, ok := providers[providerID]
	if !ok {
		return nil, scanStats{}, &usageError{fmt.Errorf("unknown provider %q (available: %s)", providerID, strings.Join(providerNames(providers), ", "))}
	}
	if provider.TestURL == "" {
		return nil, scanStats{}, &usageError{fmt.Errorf("provider %q has no test URL", providerID)}
	}
	tlsRoots, err := scanRootCAs(base)
	if err != nil {
		return nil, scanStats{}, fmt.Errorf("could not build CA pool for scanning: %w", err)
	}

	snis, err := resolveScanSNIs(opts, providerID, provider)
	if err != nil {
		return nil, scanStats{}, &usageError{err}
	}
	nets, err := resolveScanRanges(opts)
	if err != nil {
		return nil, scanStats{}, &usageError{err}
	}
	ips, err := expandRanges(nets, opts.MaxIPs)
	if err != nil {
		return nil, scanStats{}, &usageError{err}
	}

	cfg := &scanParams{
		port:       opts.Port,
		tcpTimeout: time.Duration(opts.TCPTimeoutSec) * time.Second,
		tlsTimeout: time.Duration(opts.TLSTimeoutSec) * time.Second,
		vetTimeout: time.Duration(opts.TimeoutSec) * time.Second,
		repeats:    opts.Repeats,
		minOK:      opts.MinOK,
		pool:       pool,
		tlsRoots:   tlsRoots,
		testURL:    provider.TestURL,
	}
	pterm.Info.Printfln("Scanning %d IPs × %d SNI(s) on port %d against %s (workers: %d)",
		len(ips), len(snis), opts.Port, testURLHost(provider.TestURL), opts.Workers)

	results, stats := runScan(ips, snis, cfg, opts.Workers)
	return applyTop(results, opts.Top), stats, nil
}

// applyTop keeps at most the top N fastest pairs (0 or less keeps all).
func applyTop(results []scanResult, top int) []scanResult {
	if top > 0 && len(results) > top {
		return results[:top]
	}
	return results
}

func frontScan(cmd *FrontScanCmd) {
	results, stats, err := runScanPipeline(&scanOptions{
		Provider:      cmd.Provider,
		Ranges:        cmd.Ranges,
		CIDRsFile:     cmd.CIDRsFile,
		SNIs:          cmd.SNIs,
		SNIsFile:      cmd.SNIsFile,
		Port:          cmd.Port,
		Workers:       cmd.Workers,
		TCPTimeoutSec: cmd.TCPTimeoutSec,
		TLSTimeoutSec: cmd.TLSTimeoutSec,
		TimeoutSec:    cmd.TimeoutSec,
		Repeats:       cmd.Repeats,
		MinOK:         cmd.MinOK,
		MaxIPs:        cmd.MaxIPs,
		Top:           cmd.Top,
	})
	if err != nil {
		pterm.Error.Printfln("%s", err)
		var ue *usageError
		if errors.As(err, &ue) {
			os.Exit(2)
		}
		os.Exit(1)
	}

	pterm.Info.Printfln("Scanned %d IPs: %d had TCP %d open, %d completed a TLS handshake, %d passed the fronting vet",
		stats.total, stats.tcpOK, cmd.Port, stats.tlsOK, stats.vetOK)
	if len(results) == 0 {
		pterm.Error.Println("No working IP/SNI pairs found — nothing written.")
		pterm.Info.Println("Hints: try other --ranges/--cidrs or --sni, raise --max-ips, or run from the target network.")
		os.Exit(1)
	}

	table := pterm.TableData{{"IP", "SNI", "TLS ms", "Fronted ms"}}
	shown := results
	if len(shown) > 50 {
		shown = shown[:50]
	}
	for _, r := range shown {
		table = append(table, []string{r.IP, r.SNI, fmt.Sprintf("%.0f", r.tlsMs), fmt.Sprintf("%.0f", r.avgMs)})
	}
	_ = pterm.DefaultTable.WithHasHeader().WithData(table).Render()
	if len(results) > len(shown) {
		pterm.Info.Printfln("... and %d more (all of them are in the pairs file)", len(results)-len(shown))
	}

	outPath := cmd.Out
	if outPath == "" {
		outPath = filepath.Join(args.DataPath, scanPairsName)
	}
	if err := writePairsFile(outPath, results); err != nil {
		pterm.Error.Printfln("Failed to write %s: %s", outPath, err)
		os.Exit(1)
	}
	pterm.Success.Printfln("Wrote %d pairs to %s", len(results), outPath)

	if cmd.Build {
		cfgPath := cmd.OutConfig
		if cfgPath == "" {
			cfgPath = defaultFrontedConfigPath()
		}
		gz, err := buildFrontedConfig(fronted.EmbeddedConfigBytes(), cmd.Provider, toVettedPairs(results), cmd.KeepOriginal)
		if err != nil {
			pterm.Error.Printfln("Failed to build fronting config: %s", err)
			os.Exit(1)
		}
		if err := os.WriteFile(cfgPath, gz, 0644); err != nil {
			pterm.Error.Printfln("Failed to write %s: %s", cfgPath, err)
			os.Exit(1)
		}
		pterm.Success.Printfln("Wrote %s (%d masquerades for provider %q)", cfgPath, len(results), cmd.Provider)
		pterm.Info.Printfln("Run `lantern update --via fronting` or `lantern start` — the config is auto-detected at %s", cfgPath)
	} else {
		pterm.Info.Printfln("Run `lantern front import --pairs %s` to build the fronting config, or re-run the scan with --build", outPath)
	}
}

// resolveScanRanges turns --ranges/--cidrs into nets, falling back to the
// built-in Akamai ranges when nothing is given for the default provider.
func resolveScanRanges(opts *scanOptions) ([]*net.IPNet, error) {
	if opts.Ranges == "" && opts.CIDRsFile == "" {
		if opts.Provider != defaultScanProvider {
			return nil, fmt.Errorf("no ranges given: pass --ranges or --cidrs (built-in ranges are only bundled for provider %q)", defaultScanProvider)
		}
		return parseRanges(strings.Join(defaultAkamaiRanges, ","), "")
	}
	return parseRanges(opts.Ranges, opts.CIDRsFile)
}

// resolveScanSNIs collects the SNIs to try on every IP: --sni/--snis if given,
// otherwise the provider's masquerade domains (plus, for the default Akamai
// provider, the alternate shared edge hostnames).
func resolveScanSNIs(opts *scanOptions, providerID string, provider *fronted.Provider) ([]string, error) {
	var parts []string
	if opts.SNIs != "" {
		parts = append(parts, strings.Split(opts.SNIs, ",")...)
	}
	if opts.SNIsFile != "" {
		extra, err := readLines(opts.SNIsFile)
		if err != nil {
			return nil, err
		}
		parts = append(parts, extra...)
	}
	if len(parts) == 0 {
		for _, m := range provider.Masquerades {
			if d := strings.TrimSpace(m.Domain); d != "" {
				parts = append(parts, d)
			}
		}
		if providerID == defaultScanProvider {
			parts = append(parts, defaultAkamaiSNIs...)
		}
	}
	var (
		out  []string
		seen = map[string]bool{}
	)
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		if strings.HasPrefix(s, "*") {
			pterm.Warning.Printfln("Skipping wildcard SNI %q (a concrete hostname is required)", s)
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, errors.New("no SNIs to scan; pass --sni or --snis")
	}
	return out, nil
}

// parseRanges parses comma-separated CIDR ranges / single IPs plus an optional
// file with one range per line. IPv6 entries are skipped (the scanner is
// IPv4-only).
func parseRanges(spec, filePath string) ([]*net.IPNet, error) {
	var lines []string
	if strings.TrimSpace(spec) != "" {
		lines = append(lines, strings.Split(spec, ",")...)
	}
	if filePath != "" {
		extra, err := readLines(filePath)
		if err != nil {
			return nil, err
		}
		lines = append(lines, extra...)
	}
	var (
		nets []*net.IPNet
		seen = map[string]bool{}
	)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "/") {
			line += "/32"
		}
		_, n, err := net.ParseCIDR(line)
		if err != nil {
			return nil, fmt.Errorf("invalid range %q: %w", line, err)
		}
		if n.IP.To4() == nil {
			pterm.Warning.Printfln("Skipping %s: the scanner only scans IPv4", line)
			continue
		}
		if seen[n.String()] {
			continue
		}
		seen[n.String()] = true
		nets = append(nets, n)
	}
	return nets, nil
}

// expandRanges enumerates the nets into individual IPs, deduplicating them.
// With maxIPs > 0 a uniform random sample of that size is returned instead,
// so huge ranges stay cheap to scan.
func expandRanges(nets []*net.IPNet, maxIPs int) ([]net.IP, error) {
	if len(nets) == 0 {
		return nil, errors.New("no ranges to scan")
	}
	var ips []net.IP
	if maxIPs > 0 {
		reservoir := make([]net.IP, 0, maxIPs)
		var seen int64
		err := streamIPs(nets, maxScannableIPs, func(ip net.IP) error {
			seen++
			if len(reservoir) < maxIPs {
				reservoir = append(reservoir, ip)
			} else if j := rand.Int63n(seen); j < int64(maxIPs) {
				reservoir[j] = ip
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if seen == 0 {
			return nil, errors.New("no IPs to scan")
		}
		ips = reservoir
	} else {
		err := streamIPs(nets, maxScannableIPs, func(ip net.IP) error {
			ips = append(ips, ip)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if len(ips) == 0 {
		return nil, errors.New("no IPs to scan")
	}
	return dedupeIPs(ips), nil
}

// streamIPs walks every net and emits each contained IP, aborting with an
// error once more than hardCap IPs would be emitted.
func streamIPs(nets []*net.IPNet, hardCap int64, emit func(net.IP) error) error {
	var emitted int64
	for _, n := range nets {
		base := n.IP.To4()
		if base == nil {
			continue
		}
		ip := append(net.IP(nil), base.Mask(n.Mask)...)
		for n.Contains(ip) {
			if emitted >= hardCap {
				return fmt.Errorf("ranges expand to more than %d IPs; pass narrower --ranges/--cidrs or use --max-ips to sample", hardCap)
			}
			if err := emit(append(net.IP(nil), ip...)); err != nil {
				return err
			}
			emitted++
			if !incIP(ip) {
				break
			}
		}
	}
	return nil
}

// incIP increments ip in place, reporting false when it wraps around zero.
func incIP(ip net.IP) bool {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			return true
		}
	}
	return false
}

// dedupeIPs removes duplicates (sorting first) and shuffles the result so a
// capped scan explores the ranges evenly.
func dedupeIPs(ips []net.IP) []net.IP {
	sort.Slice(ips, func(i, j int) bool { return bytes.Compare(ips[i], ips[j]) < 0 })
	out := ips[:0]
	for i, ip := range ips {
		if i > 0 && bytes.Equal(ips[i-1], ip) {
			continue
		}
		out = append(out, ip)
	}
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// scanRootCAs builds the CA pool for the scan's TLS probe from the fronting
// config's trusted CAs, plus DigiCert Global Root G3 (which postdates the
// embedded config but signs Akamai's current chain).
func scanRootCAs(base []byte) (*x509.CertPool, error) {
	raw, err := gunzip(base)
	if err != nil {
		return nil, fmt.Errorf("decompress base config: %w", err)
	}
	var root map[string]interface{}
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parse base config: %w", err)
	}
	pool := x509.NewCertPool()
	added := 0
	cas, _ := root["trustedcas"].([]interface{})
	for _, entry := range cas {
		m, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		if pemCert, _ := m["cert"].(string); pemCert != "" && pool.AppendCertsFromPEM([]byte(pemCert)) {
			added++
		}
	}
	if added == 0 {
		return nil, errors.New("fronting config has no usable trusted CAs")
	}
	pool.AppendCertsFromPEM([]byte(digicertGlobalRootG3PEM))
	return pool, nil
}

// verifyScanCert checks the presented chain against roots, falling back to the
// system trust store when that fails (minimal containers ship no system CAs,
// while the fronting config's CAs may lag behind the CDN's root rotation).
func verifyScanCert(rawCerts [][]byte, host string, roots *x509.CertPool) error {
	if len(rawCerts) == 0 {
		return errors.New("no certificates presented")
	}
	certs := make([]*x509.Certificate, len(rawCerts))
	for i, raw := range rawCerts {
		c, err := x509.ParseCertificate(raw)
		if err != nil {
			return fmt.Errorf("invalid certificate: %w", err)
		}
		certs[i] = c
	}
	intermediates := x509.NewCertPool()
	for _, c := range certs[1:] {
		intermediates.AddCert(c)
	}
	opts := x509.VerifyOptions{DNSName: host, Intermediates: intermediates}
	if roots != nil {
		opts.Roots = roots
		if _, err := certs[0].Verify(opts); err == nil {
			return nil
		}
		opts.Roots = nil
	}
	_, err := certs[0].Verify(opts)
	return err
}

// runScan probes the IPs concurrently: a cheap TCP connect prunes dead IPs,
// a TLS handshake per (IP, SNI) filters non-CDN addresses, and survivors are
// vetted end-to-end with the same routine as `front import`.
func runScan(ips []net.IP, snis []string, cfg *scanParams, workers int) ([]scanResult, scanStats) {
	sp, _ := pterm.DefaultSpinner.WithText("Scanning...").Start()
	var (
		wg                        sync.WaitGroup
		mu                        sync.Mutex
		results                   []scanResult
		done, tcpOK, tlsOK, vetOK atomic.Int64
	)
	total := int64(len(ips))
	sem := make(chan struct{}, workers)
	for _, ip := range ips {
		wg.Add(1)
		sem <- struct{}{}
		go func(ip net.IP) {
			defer func() {
				<-sem
				wg.Done()
			}()
			res, tcp, tl := scanIP(ip, snis, cfg)
			n := done.Add(1)
			if tcp {
				tcpOK.Add(1)
			}
			if tl {
				tlsOK.Add(1)
			}
			if res != nil {
				vetOK.Add(1)
				mu.Lock()
				results = append(results, *res)
				mu.Unlock()
			}
			if n%25 == 0 || n == total {
				sp.UpdateText(fmt.Sprintf("Scanning %d/%d — tcp ok: %d, tls ok: %d, fronted: %d",
					n, total, tcpOK.Load(), tlsOK.Load(), vetOK.Load()))
			}
		}(ip)
	}
	wg.Wait()
	_ = sp.Stop()

	sort.Slice(results, func(i, j int) bool { return results[i].avgMs < results[j].avgMs })
	stats := scanStats{total: total, tcpOK: tcpOK.Load(), tlsOK: tlsOK.Load(), vetOK: vetOK.Load()}
	return results, stats
}

// scanIP probes a single IP: TCP connect, then a TLS handshake with each SNI
// until one succeeds, then the end-to-end fronting vet for the winning pair.
func scanIP(ip net.IP, snis []string, cfg *scanParams) (*scanResult, bool, bool) {
	addr := net.JoinHostPort(ip.String(), strconv.Itoa(cfg.port))
	conn, err := net.DialTimeout("tcp", addr, cfg.tcpTimeout)
	if err != nil {
		return nil, false, false
	}
	_ = conn.Close()

	for _, sni := range snis {
		ms, err := tlsProbe(ip, cfg.port, sni, cfg)
		if err != nil {
			continue
		}
		vp := vetPair(frontPair{IP: ip.String(), SNI: sni}, cfg.pool, cfg.testURL, cfg.repeats, cfg.minOK, cfg.vetTimeout)
		if vp.passed {
			return &scanResult{vettedPair: vp, tlsMs: ms}, true, true
		}
		// The handshake worked but fronting through this pair does not —
		// try the next SNI on this IP.
	}
	return nil, true, false
}

// tlsProbe performs a full TLS handshake with sni as SNI and verifies the
// presented chain with verifyScanCert.
func tlsProbe(ip net.IP, port int, sni string, cfg *scanParams) (float64, error) {
	dialer := &net.Dialer{Timeout: cfg.tlsTimeout}
	start := time.Now()
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(port)), &tls.Config{
		ServerName:         sni,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // verification happens in VerifyPeerCertificate below
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return verifyScanCert(rawCerts, sni, cfg.tlsRoots)
		},
	})
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return float64(time.Since(start).Microseconds()) / 1000, nil
}

func toVettedPairs(results []scanResult) []vettedPair {
	out := make([]vettedPair, len(results))
	for i, r := range results {
		out[i] = r.vettedPair
	}
	return out
}

func writePairsFile(path string, results []scanResult) error {
	pf := frontPairsFile{Pairs: make([]frontPair, len(results))}
	for i, r := range results {
		pf.Pairs[i] = r.frontPair
	}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
