package main

import (
	"os"
	"path/filepath"

	"github.com/pterm/pterm"

	"github.com/getlantern/flashlight/v7/common"
)

// defaultFrontedConfigName is where `front import` writes its output and where
// the client looks for a local fronting config.
const defaultFrontedConfigName = "fronted-local.yaml.gz"

// defaultFrontedConfigPath returns the default location of the local fronting
// configuration inside the data folder.
func defaultFrontedConfigPath() string {
	return filepath.Join(args.DataPath, defaultFrontedConfigName)
}

// resolvedFrontedConfigPath returns the fronting config to use: the explicit
// --fronted-config value, or the auto-detected file in the data folder ("" if
// neither exists).
func resolvedFrontedConfigPath() string {
	if args.FrontedConfig != "" {
		return args.FrontedConfig
	}
	candidate := defaultFrontedConfigPath()
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

// applyClientOptions wires the CLI flags into flashlight's control-plane HTTP
// client before anything uses it.
func applyClientOptions() {
	opts := &common.ClientOptions{
		SkipProxyless:    !args.WithProxyless,
		SkipDNSTT:        !args.EnableDNSTT,
		UpstreamProxy:    args.UpstreamProxy,
		FrontedConfigURL: args.FrontedConfigURL,
		DoHResolver:      args.DoHResolver,
		DoTResolver:      args.DoTResolver,
	}
	opts.FrontedConfigPath = resolvedFrontedConfigPath()
	if opts.FrontedConfigPath != "" {
		pterm.Debug.Printfln("Using local fronting config: %s", opts.FrontedConfigPath)
	}
	common.SetClientOptions(opts)
}
