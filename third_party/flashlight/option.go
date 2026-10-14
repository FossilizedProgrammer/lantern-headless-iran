package flashlight

import (
	commonconfig "github.com/getlantern/common/config"

	"github.com/getlantern/flashlight/v7/config"
	"github.com/getlantern/flashlight/v7/dialer"
)

type Option func(*Flashlight)

// WithOnSucceedingProxy sets the onSucceedingProxy callback
func WithOnSucceedingProxy(onSucceedingProxy func()) Option {
	return func(client *Flashlight) {
		client.callbacks.onSucceedingProxy = onSucceedingProxy
	}
}

// WithOnConfig sets the callback that is called when a config is successfully fetched
func WithOnConfig(onConfigUpdate func(*config.Global, config.Source)) Option {
	return func(client *Flashlight) {
		client.callbacks.onConfigUpdate = onConfigUpdate
	}
}

// WithOnDialError sets the callback that is called when an error occurs dialing a proxy. It includes the error itself and whether or not we
// have any successful dialers
func WithOnDialError(onDialError func(error, bool)) Option {
	return func(client *Flashlight) {
		client.callbacks.onDialError = onDialError
	}
}

// WithInit sets the callback that is called when flashlight is ready and has a config or needs to be initialized
func WithInit(onInit func()) Option {
	return func(client *Flashlight) {
		client.callbacks.onInit = onInit
	}
}

// WithOnProxies sets the callback when new proxies are received
func WithOnProxies(onProxiesUpdate func([]dialer.ProxyDialer, config.Source)) Option {
	return func(client *Flashlight) {
		client.callbacks.onProxiesUpdate = onProxiesUpdate
	}
}

// WithProxyFilter sets a filter that is applied to the map of chained proxies
// before it is dispatched to any proxy listener (the client's dialers, the
// bypass service, ...). It can be used to pin the client to a single server.
// The filter must not modify the given map; it returns the map to use instead
// (a nil or empty result means "no proxies", i.e. the client will have no
// chained servers).
func WithProxyFilter(filter func(proxies map[string]*commonconfig.ProxyConfig) map[string]*commonconfig.ProxyConfig) Option {
	return func(client *Flashlight) {
		client.proxyFilter = filter
	}
}

// WithOnProxyConfigs sets a callback that is invoked with the raw proxy
// configurations (name -> config, including location) whenever they change,
// before any filtering is applied. Unlike WithOnProxies, it does not wait for
// dialers to be built, so it also reports servers whose transport is
// unsupported on this platform.
func WithOnProxyConfigs(onProxyConfigs func(proxies map[string]*commonconfig.ProxyConfig, src config.Source)) Option {
	return func(client *Flashlight) {
		client.callbacks.onProxyConfigs = onProxyConfigs
	}
}
