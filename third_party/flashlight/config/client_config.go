package config

// ClientConfig captures configuration information for a Client
type ClientConfig struct {
	DumpHeaders bool // whether or not to dump headers of requests and responses

	// DNS host-to-ip mappings to bypass DNS when resolving hostnames
	// This only works for direct dials (i.e., domain routing rules that are
	// MustDirect or 'md':
	// https://github.com/getlantern/flashlight/blob/f82d9ab04da841e4a2833783b244948a8daa547e/domainrouting/domainrouting.go#L33
	DNSResolutionMapForDirectDials map[string]string
}

// NewClientConfig creates a new client config with default values.
func NewClientConfig() *ClientConfig {
	return &ClientConfig{}
}
