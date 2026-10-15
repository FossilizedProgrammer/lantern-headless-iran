package common

import (
	"net/http"
	"sync"
	"sync/atomic"

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

	configDir atomic.Value
)

// The config directory on some platforms, such as Android, can only be determined in native code, so we
// need to set it externally.
func SetConfigDir(dir string) {
	configDir.Store(dir)
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
			log.Errorf("Ignoring invalid upstream proxy %q, falling back to a direct connection: %v", opts.UpstreamProxy, err)
			kOptions = append(kOptions, kindling.WithTransport(directTransport{}))
		}
	} else {
		// Without an upstream proxy the control plane is reached directly.
		kOptions = append(kOptions, kindling.WithTransport(directTransport{}))
	}
	k = kindling.NewKindling("flashlight", kOptions...)
	httpClient = k.NewHTTPClient()
	return httpClient
}
