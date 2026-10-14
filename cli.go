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

type FrontCmd struct {
	Import *FrontImportCmd `arg:"subcommand:import" help:"Validate scanned IP/SNI pairs and build a local fronting config (fronted-local.yaml.gz)"`
	Scan   *FrontScanCmd   `arg:"subcommand:scan" help:"Scan IP ranges for working fronting endpoints (IP/SNI pairs) with the built-in IP scanner"`
	Test   *FrontTestCmd   `arg:"subcommand:test" help:"Verify domain fronting end-to-end against Lantern's config endpoints"`
}

type FrontImportCmd struct {
	Pairs        string `arg:"--pairs" help:"JSON file with {\"pairs\":[{\"ip\":\"...\",\"sni\":\"...\"}]}"`
	IPs          string `arg:"--ips" help:"File with one IP per line (combine with --snis)"`
	SNIs         string `arg:"--snis" help:"File with one SNI per line (combine with --ips)"`
	Out          string `arg:"--out" help:"Output path (default: <data-path>/fronted-local.yaml.gz)"`
	Provider     string `arg:"--provider" default:"akamai" help:"Fronting provider whose masquerades are replaced"`
	Workers      int    `arg:"--workers" default:"64" help:"Number of parallel vetting workers"`
	Repeats      int    `arg:"--repeats" default:"3" help:"Vetting attempts per pair"`
	MinOK        int    `arg:"--min-ok" default:"2" help:"Successful attempts required for a pair to pass"`
	TimeoutSec   int    `arg:"--timeout" default:"15" help:"Per-attempt timeout in seconds"`
	Top          int    `arg:"--top" default:"200" help:"Keep at most N fastest pairs (0 = all)"`
	KeepOriginal bool   `arg:"--keep-original" help:"Keep the original masquerades of the provider as well"`
}

type FrontTestCmd struct {
	Config     string `arg:"--config" help:"fronted.yaml.gz to test (default: <data-path>/fronted-local.yaml.gz, falls back to embedded)"`
	TimeoutSec int    `arg:"--timeout" default:"30" help:"Timeout per request in seconds"`
}

type FrontScanCmd struct {
	Ranges        string `arg:"--ranges" help:"Comma-separated CIDR ranges or single IPs to scan, e.g. 23.32.0.0/11,104.64.0.0/10 (default: built-in Akamai ranges)"`
	CIDRsFile     string `arg:"--cidrs" help:"File with one CIDR range or IP per line"`
	SNIs          string `arg:"--sni" help:"Comma-separated SNIs to try on every IP (default: the provider's masquerade domains)"`
	SNIsFile      string `arg:"--snis" help:"File with one SNI per line"`
	Provider      string `arg:"--provider" default:"akamai" help:"Fronting provider whose masquerades and test URL are used"`
	Port          int    `arg:"--port" default:"443" help:"TCP port to scan"`
	Workers       int    `arg:"--workers" default:"300" help:"Number of parallel scan workers"`
	TCPTimeoutSec int    `arg:"--tcp-timeout" default:"2" help:"TCP connect timeout in seconds"`
	TLSTimeoutSec int    `arg:"--tls-timeout" default:"5" help:"TLS handshake timeout in seconds"`
	TimeoutSec    int    `arg:"--timeout" default:"15" help:"Per-vet timeout in seconds"`
	Repeats       int    `arg:"--repeats" default:"1" help:"Vetting attempts per pair"`
	MinOK         int    `arg:"--min-ok" default:"1" help:"Successful attempts required for a pair to pass"`
	MaxIPs        int    `arg:"--max-ips" default:"60000" help:"Scan at most N randomly sampled IPs (0 = all; may be very slow on big ranges)"`
	Top           int    `arg:"--top" default:"0" help:"Keep at most N fastest pairs (0 = all)"`
	Out           string `arg:"--out" help:"Where to write the passing pairs as JSON (default: <data-path>/scan-pairs.json)"`
	OutConfig     string `arg:"--out-config" help:"Where --build writes the fronting config (default: <data-path>/fronted-local.yaml.gz)"`
	Build         bool   `arg:"--build" help:"Also build the local fronting config from the passing pairs"`
	KeepOriginal  bool   `arg:"--keep-original" help:"When building, keep the original masquerades of the provider as well"`
}

type UpdateCmd struct {
	Via        string `arg:"--via" default:"auto" help:"How to reach the config service: fronting, upstream, direct, or auto (tries fronting, then upstream, then direct)"`
	TimeoutSec int    `arg:"--timeout" default:"45" help:"Timeout per attempt"`
	JSON       bool   `arg:"--json" help:"Print the fetched server list as JSON"`
	Check      bool   `arg:"--check" help:"Probe each fetched server (TCP 443) and show whether it is directly reachable or likely IP-blocked"`
	ConfigURL  string `arg:"--config-url" help:"Override the config service URL (default: https://df.iantem.io/api/v1/config)"`
	Rescan     bool   `arg:"--rescan" help:"Scan for fresh fronting endpoints first, even if the current fronting config still works"`
	NoScan     bool   `arg:"--no-scan" help:"Never run the automatic fallback scan when fronting fails"`
	ScanIPs    int    `arg:"--scan-ips" default:"15000" help:"IPs sampled by the fronting scan used by --rescan and the automatic fallback"`
	ScanTop    int    `arg:"--scan-top" default:"20" help:"Pairs the fronting scan keeps in the rebuilt fronting config"`
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
	Front   *FrontCmd   `arg:"subcommand:front" help:"Domain fronting tools (front scan / front import / front test)"`
	Servers *ServersCmd `arg:"subcommand:servers" help:"List Lantern servers and their geographic locations"`
	Update  *UpdateCmd  `arg:"subcommand:update" help:"Update the cached server list (user.conf) via fronting, upstream proxy, or direct"`

	// common options
	DataPath string `arg:"--data-path" help:"Path to store data (config, logs, etc)"`
	Debug    bool   `arg:"--debug" help:"Enable debug logging"`
	Quiet    bool   `arg:"--quiet" help:"Disable logging to stdout"`
	Raw      bool   `arg:"--raw" help:"Output logs without any formatting"`
	AuthURL  string `arg:"--auth-url" help:"Override URL of the authentication server"`
	Insecure bool   `arg:"--insecure" help:"Whether to skip TLS verification"`

	// control-plane / fronting options
	UpstreamProxy    string `arg:"--upstream-proxy" help:"HTTP/HTTPS/SOCKS5 proxy used to fetch Lantern config, to reach Lantern's own (possibly IP-blocked) proxy servers, and as a last-resort data path when no Lantern proxy is reachable, e.g. socks5://127.0.0.1:1080"`
	FrontedConfig    string `arg:"--fronted-config" help:"Path to a local fronted.yaml.gz (default: auto-detect <data-path>/fronted-local.yaml.gz)"`
	FrontedConfigURL string `arg:"--fronted-config-url" help:"Override URL used to refresh the fronting config"`
	WithProxyless    bool   `arg:"--with-proxyless" help:"Also try direct/smart dialing for control-plane requests (off by default)"`
	EnableDNSTT      bool   `arg:"--enable-dnstt" help:"Enable the DNS tunnel transport (requires a reachable DoH/DoT resolver)"`
	DoHResolver      string `arg:"--doh-resolver" help:"DoH resolver URL for the DNS tunnel"`
	DoTResolver      string `arg:"--dot-resolver" help:"DoT resolver address for the DNS tunnel"`
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
