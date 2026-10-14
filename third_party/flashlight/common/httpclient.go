package common

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getlantern/dnstt"
	"github.com/getlantern/fronted"
	"github.com/getlantern/kindling"

	"github.com/getlantern/flashlight/v7/sentry"
)

var (
	httpClient *http.Client
	mutex      = &sync.Mutex{}

	// These are the domains we will access via kindling.
	domains = []string{
		"api.iantem.io",
		"api.getiantem.org",    // Still used on iOS
		"geo.getiantem.org",    // Still used on iOS
		"config.getiantem.org", // Still used on iOS
		"df.iantem.io",
		"raw.githubusercontent.com",
		"media.githubusercontent.com",
		"objects.githubusercontent.com",
		"replica-r2.lantern.io",
		"replica-search.lantern.io",
		"update.getlantern.org",
		"globalconfig.flashlightproxy.com",
		"dogsdogs.xyz",         // Used in replica
		"service.dogsdogs.xyz", // Used in replica
	}

	configDir   atomic.Value
	dnsttConfig atomic.Value // Holds the DNSTTConfig

	defaultDNSTTConfig = &DNSTTConfig{
		Domain:           "t.iantem.io",
		PublicKey:        "405eb9e22d806e3a0a8e667c6665a321c8a6a35fa680ed814716a66d7ad84977",
		DoHResolver:      "https://cloudflare-dns.com/dns-query",
		DoTResolver:      "",
		UTLSDistribution: "",
	}
)

type DNSTTConfig struct {
	Domain           string `yaml:"domain"`    // DNS tunnel domain, e.g., "t.iantem.io"
	PublicKey        string `yaml:"publicKey"` // DNSTT server public key
	DoHResolver      string `yaml:"dohResolver,omitempty"`
	DoTResolver      string `yaml:"dotResolver,omitempty"`
	UTLSDistribution string `yaml:"utlsDistribution,omitempty"`
}

func init() {
	dnsttConfig.Store(defaultDNSTTConfig)
}

// The config directory on some platforms, such as Android, can only be determined in native code, so we
// need to set it externally.
func SetConfigDir(dir string) {
	configDir.Store(dir)
}

func SetDNSTTConfig(cfg *DNSTTConfig) {
	if cfg != nil {
		dnsttConfig.Store(cfg)
	}
}

func GetHTTPClient() *http.Client {
	mutex.Lock()
	defer mutex.Unlock()
	if httpClient != nil {
		return httpClient
	}

	opts := GetClientOptions()
	var k kindling.Kindling
	ioWriter := log.AsDebugLogger().Writer()
	kOptions := []kindling.Option{
		kindling.WithPanicListener(sentry.PanicListener),
		kindling.WithLogWriter(ioWriter),
	}
	if !opts.SkipProxyless {
		kOptions = append(kOptions, kindling.WithProxyless(domains...))
	}
	if opts.UpstreamProxy != "" {
		if t, err := newUpstreamProxyTransport(opts.UpstreamProxy); err == nil {
			log.Debugf("Adding upstream proxy transport for control-plane requests: %s", opts.UpstreamProxy)
			kOptions = append(kOptions, kindling.WithTransport(t))
		} else {
			log.Errorf("Ignoring invalid upstream proxy %q: %v", opts.UpstreamProxy, err)
		}
	}

	// Create new fronted instance. Fronting must always be available, even if
	// refreshing its configuration is not, so newFronted only returns an error
	// when domain fronting itself cannot be set up.
	f, err := newFronted(ioWriter, sentry.PanicListener)
	if err != nil {
		log.Errorf("Failed to create fronted instance: %v", err)
	} else {
		kOptions = append(kOptions, kindling.WithDomainFronting(f))
	}
	if opts.SkipDNSTT {
		log.Debug("Skipping DNSTT transport (disabled)")
	} else if d, err := newDNSTT(); err != nil {
		log.Errorf("Failed to create DNSTT: %v", err)
	} else {
		kOptions = append(kOptions, kindling.WithDNSTunnel(d))
	}
	k = kindling.NewKindling("flashlight", kOptions...)
	httpClient = k.NewHTTPClient()
	return httpClient
}

func newFronted(logWriter io.Writer, panicListener func(string)) (fronted.Fronted, error) {
	opts := GetClientOptions()

	configURL := opts.FrontedConfigURL
	if configURL == "" && opts.FrontedConfigPath == "" {
		// Default refresh URL, only used when no local fronting config is given.
		configURL = "https://raw.githubusercontent.com/getlantern/fronted/refs/heads/main/fronted.yaml.gz"
	}

	// The HTTP client is only used to refresh the fronting configuration, never
	// for actual fronting traffic, so any failure here falls back to a plain
	// client instead of disabling domain fronting altogether.
	httpClient := controlPlaneHTTPClient(logWriter, opts)
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	var cacheFile string
	configDirValue := configDir.Load()
	if configDirValue != nil {
		cacheFile = filepath.Join(configDirValue.(string), "fronted_cache.json")
	} else {
		cacheFile = filepath.Join(os.TempDir(), "fronted_cache.json")
	}
	frontedOpts := []fronted.Option{
		fronted.WithPanicListener(panicListener),
		fronted.WithCacheFile(cacheFile),
		fronted.WithHTTPClient(httpClient),
	}
	if configURL != "" {
		frontedOpts = append(frontedOpts, fronted.WithConfigURL(configURL))
	}
	if opts.FrontedConfigPath != "" {
		log.Debugf("Loading local fronting configuration from %s", opts.FrontedConfigPath)
		frontedOpts = append(frontedOpts, fronted.WithLocalConfig(opts.FrontedConfigPath))
	}
	return fronted.NewFronted(frontedOpts...), nil
}

// controlPlaneHTTPClient returns the HTTP client used to refresh the fronting
// configuration, or nil if no better-than-direct option is available.
func controlPlaneHTTPClient(logWriter io.Writer, opts *ClientOptions) *http.Client {
	if opts.UpstreamProxy != "" {
		if rt, err := newUpstreamProxyRoundTripper(opts.UpstreamProxy); err == nil {
			return &http.Client{Transport: rt, Timeout: 30 * time.Second}
		} else {
			log.Errorf("Ignoring invalid upstream proxy %q: %v", opts.UpstreamProxy, err)
		}
	}
	if opts.SkipProxyless {
		// Building the smart dialer runs DNS/TLS probes that are slow and
		// usually blocked in censored networks, so skip it entirely.
		return nil
	}
	// Best effort: download the fronted config using the smart dialer, as
	// upstream does.
	u, err := url.Parse("https://raw.githubusercontent.com/getlantern/fronted/refs/heads/main/fronted.yaml.gz")
	if err != nil {
		return nil
	}
	trans, err := kindling.NewSmartHTTPTransport(logWriter, u.Host)
	if err != nil {
		log.Debugf("Could not create smart transport for fronting config updates, falling back to direct: %v", err)
		return nil
	}
	return &http.Client{Transport: trans, Timeout: 30 * time.Second}
}

func (c *DNSTTConfig) Validate() error {
	if c.PublicKey == "" {
		return fmt.Errorf("publicKey is required")
	}
	if c.Domain == "" {
		return fmt.Errorf("domain is required")
	}
	if c.DoHResolver == "" && c.DoTResolver == "" {
		return fmt.Errorf("at least one of DoHResolver or DoTResolver must be specified")
	}
	return nil
}

func newDNSTT() (dnstt.DNSTT, error) {
	cfg := dnsttConfig.Load().(*DNSTTConfig)
	opts := GetClientOptions()
	if opts.DoHResolver != "" || opts.DoTResolver != "" {
		overridden := *cfg
		if opts.DoHResolver != "" {
			overridden.DoHResolver = opts.DoHResolver
		}
		if opts.DoTResolver != "" {
			overridden.DoTResolver = opts.DoTResolver
			if opts.DoHResolver == "" {
				// newDNSTT gives DoH precedence, so clear it when only DoT was requested.
				overridden.DoHResolver = ""
			}
		}
		cfg = &overridden
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid DNSTT configuration: %w", err)
	}

	options := []dnstt.Option{
		dnstt.WithPublicKey(cfg.PublicKey),
		dnstt.WithTunnelDomain(cfg.Domain),
	}
	switch {
	case cfg.DoHResolver != "":
		options = append(options, dnstt.WithDoH(cfg.DoHResolver))
	case cfg.DoTResolver != "":
		options = append(options, dnstt.WithDoT(cfg.DoTResolver))
	}
	if cfg.UTLSDistribution != "" {
		options = append(options, dnstt.WithUTLSDistribution(cfg.UTLSDistribution))
	}
	return dnstt.NewDNSTT(options...)
}
