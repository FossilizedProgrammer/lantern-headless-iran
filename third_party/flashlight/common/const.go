package common

import (
	"github.com/getlantern/golog"
)

const (
	// UserConfigURL is the URL for fetching the per user proxy config.
	//
	// This is the Cloudflare-fronted alias of df.iantem.io. Google's edge
	// answers 403 to requests from Iranian IPs for df.iantem.io, so a plain
	// direct fetch from Iran can never reach the config service there. The
	// same origin behind Cloudflare is reachable; Lantern's own Radiance
	// client uses exactly this alias (df.iantem.io -> df.getiantem.org) for
	// restricted regions.
	UserConfigURL = "https://df.getiantem.org/api/v1/config"

	// UpdateServerURL is the URL of the update server. Different applications
	// hit the server on separate paths "/update/<AppName>".
	UpdateServerURL = "https://update.getlantern.org/"
)

var (
	EnvironmentDevelopment = "development"
	EnvironmentProduction  = "production"
)

var (
	// GlobalURL URL for fetching the global config.
	GlobalURL = "https://globalconfig.flashlightproxy.com/global.yaml.gz"

	ProAPIHost = "api.getiantem.org"

	log = golog.LoggerFor("flashlight.common")

	// Set by the linker using -ldflags in the project's Makefile.
	// Defaults to 'production' so as not to mistakingly push development work
	// to a production environment
	Environment = "production"
)
