package main

import (
	"github.com/getlantern/flashlight/v7/common"
)

// applyClientOptions wires the CLI flags into flashlight's control-plane HTTP
// client before anything uses it.
func applyClientOptions() {
	opts := &common.ClientOptions{
		SkipProxyless: !args.WithProxyless,
		SkipDNSTT:     !args.EnableDNSTT,
		UpstreamProxy: args.UpstreamProxy,
		DoHResolver:   args.DoHResolver,
		DoTResolver:   args.DoTResolver,
	}
	common.SetClientOptions(opts)
}
