package main

import (
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/pterm/pterm"

	"github.com/oldblocker/lantern-headless-iran/shared"
)

type ServeCmd struct {
	HTTPProxyAddress  string `arg:"--http-proxy-addr" default:"127.0.0.1:0" help:"Address to bind to and use for HTTP the proxy. Defaults to a random port on localhost"`
	SOCKSProxyAddress string `arg:"--socks-proxy-addr" default:"127.0.0.1:0" help:"Address to bind to and use for SOCKS the proxy. Defaults to a random port on localhost"`
	StickyConfig      bool   `arg:"--sticky-config" help:"Whether to use a sticky config"`
	ReadableConfig    bool   `arg:"--readable-config" help:"Whether to use a readable config"`
	Server            string `arg:"--server" help:"Pin the connection to a single server by (case-insensitive) name and ignore all other servers. See the servers command for the list of names"`
	Engine            string `arg:"--engine" default:"flashlight" help:"Connection engine: flashlight (this client's own transport, supports --upstream-proxy) or radiance (full parity with official Lantern v10/beta using Radiance + lantern-box)"`
}

type AuthCmd struct {
	Login  *LoginCmd  `arg:"subcommand:login" help:"Login to Lantern"`
	Signup *SignupCmd `arg:"subcommand:signup" help:"Signup for Lantern"`
	Logout *LogoutCmd `arg:"subcommand:logout" help:"Logout of Lantern"`
}

type LoginCmd struct {
	Email    string `arg:"--email,env:EMAIL" help:"Email address"`
	Password string `arg:"--password,env:PASSWORD" help:"Password"`
}

type SignupCmd struct {
	Email    string `arg:"--email,env:EMAIL" help:"Email address"`
	Password string `arg:"--password,env:PASSWORD" help:"Password"`
}

type LogoutCmd struct {
}

type UpdateCmd struct {
	Via        string `arg:"--via" default:"auto" help:"How to reach the config service: upstream, direct, or auto (tries upstream, then direct)"`
	TimeoutSec int    `arg:"--timeout" default:"45" help:"Timeout per attempt"`
	JSON       bool   `arg:"--json" help:"Print the fetched server list as JSON"`
	Check      bool   `arg:"--check" help:"Probe each fetched server (TCP 443) and show whether it is directly reachable or likely IP-blocked"`
	ConfigURL  string `arg:"--config-url" help:"Override the config service URL (default: https://df.getiantem.org/api/v1/config)"`
}

type ServersCmd struct {
	Refresh    bool `arg:"--refresh" help:"Fetch a fresh server list from Lantern's config service instead of using the cached one"`
	JSON       bool `arg:"--json" help:"Print the list as JSON"`
	Check      bool `arg:"--check" help:"Probe each server (TCP 443) and show whether it is directly reachable or likely IP-blocked"`
	TimeoutSec int  `arg:"--timeout" default:"45" help:"How long to wait for a fresh list when --refresh is set"`
}

type Args struct {
	// sub commands
	Auth    *AuthCmd    `arg:"subcommand:auth" help:"Authenticate with Lantern"`
	Serve   *ServeCmd   `arg:"subcommand:start" help:"Start Lantern"`
	Servers *ServersCmd `arg:"subcommand:servers" help:"List Lantern servers and their geographic locations"`
	Update  *UpdateCmd  `arg:"subcommand:update" help:"Update the cached server list (user.conf) via upstream proxy or direct"`

	// common options
	DataPath string `arg:"--data-path" help:"Path to store data (config, logs, etc)"`
	Debug    bool   `arg:"--debug" help:"Enable debug logging"`
	Quiet    bool   `arg:"--quiet" help:"Disable logging to stdout"`
	Raw      bool   `arg:"--raw" help:"Output logs without any formatting"`
	AuthURL  string `arg:"--auth-url" help:"Override URL of the authentication server"`
	Insecure bool   `arg:"--insecure" help:"Whether to skip TLS verification"`

	// control-plane options
	UpstreamProxy string `arg:"--upstream-proxy" help:"HTTP/HTTPS/SOCKS5 proxy used to fetch Lantern config, to reach Lantern's own (possibly IP-blocked) proxy servers, and as a last-resort data path when no Lantern proxy is reachable, e.g. socks5://127.0.0.1:1080"`
	WithProxyless bool   `arg:"--with-proxyless" help:"Also try direct/smart dialing for control-plane requests (off by default)"`

	// radiance engine
	RadianceEngine string `arg:"--radiance-engine" help:"Path to the lantern-radiance helper binary used by --engine radiance (default: a sibling of this executable, LANTERN_RADIANCE_ENGINE, or $PATH)"`
}

func (Args) Version() string {
	return fmt.Sprintf("lantern-headless-iran v%s [Build Date: %s Revision Date: %s]", shared.ApplicationVersion, shared.BuildDate, shared.RevisionDate)
}

var args Args
var userID int64
var userToken string
var deviceId string
var configFilePath, logFilePath string
var argParser *arg.Parser

func parseArgs() {
	argParser = arg.MustParse(&args)
}

func printHelp() {
	argParser.WriteHelp(pterm.DefaultLogger.Writer)
}
