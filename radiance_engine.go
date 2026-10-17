package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/pterm/pterm"
)

// readyPrefix is the line the radiance engine prints once its local proxy is
// up. The format is: RADIANCE_READY socks=<addr> http=<addr>.
const readyPrefix = "RADIANCE_READY "

// serveRadiance runs the official Lantern v10 connection engine (Radiance +
// lantern-box) as a child process and exposes its local proxy. The engine is
// built with the "novpn" tag so it serves HTTP/SOCKS instead of a TUN device.
//
// The engine lives in its own Go module (radiance-engine/) because its
// dependency graph cannot be merged with this module's vendored flashlight
// (conflicting replace directives for sing-box, wazero, water and gvisor).
func serveRadiance(ctx context.Context, cmd *ServeCmd) {
	enginePath, err := locateRadianceEngine()
	if err != nil {
		pterm.Error.Printfln("%s", err)
		pterm.Info.Println("Build it with `make radiance-engine` (needs Go 1.26.x; Go 1.27 breaks a sing-box //go:linkname).")
		return
	}

	if cmd.Server != "" {
		pterm.Info.Printfln("Pinning the connection to server %q; all other servers are ignored", cmd.Server)
	}

	engineArgs := []string{
		"--data-path", args.DataPath,
		"--socks-addr", cmd.SOCKSProxyAddress,
		"--http-addr", cmd.HTTPProxyAddress,
	}
	if cmd.Server != "" {
		engineArgs = append(engineArgs, "--server", cmd.Server)
	}
	if args.UpstreamProxy != "" {
		engineArgs = append(engineArgs, "--upstream-proxy", args.UpstreamProxy)
	}
	if args.WithProxyless {
		engineArgs = append(engineArgs, "--proxyless")
	}
	if args.Insecure {
		engineArgs = append(engineArgs, "--insecure")
	}
	logLevel := "info"
	if args.Debug {
		logLevel = "debug"
	}
	engineArgs = append(engineArgs, "--log-level", logLevel)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	pterm.Info.Printfln("Starting Lantern with the radiance engine (official v10 data plane)... Log file: %s", logFilePath)
	proc := exec.CommandContext(ctx, enginePath, engineArgs...)

	pipe, perr := proc.StdoutPipe()
	if perr != nil {
		pterm.Error.Printfln("Failed to capture engine output: %s", perr)
		return
	}
	proc.Stderr = proc.Stdout

	if serr := proc.Start(); serr != nil {
		pterm.Error.Printfln("Failed to start %s: %s", enginePath, serr)
		return
	}

	ready := make(chan string, 1)
	go drainEngineOutput(pipe, ready)

	exit := make(chan error, 1)
	go func() { exit <- proc.Wait() }()

	select {
	case line := <-ready:
		socks, httpAddr := parseReady(line)
		// The radiance engine always routes through the Lantern network, so the
		// "no Lantern proxy is reachable" hint in verifyDataPath must not fire.
		lanternProxyReachable.Store(true)
		pterm.Success.Println("Radiance engine connected to the Lantern network")
		pterm.Info.Printfln("Local proxy is listening — SOCKS5: %v  HTTP: %v", socks, httpAddr)
		pterm.Info.Println("Verifying the data path with a real request through that proxy...")
		go verifyDataPath(httpAddr)
	case werr := <-exit:
		pterm.Error.Printfln("Radiance engine exited before it was ready: %v", werr)
		pterm.Info.Printfln("Details: %s. Re-run with --debug for the engine's own logs.", logFilePath)
		return
	case <-time.After(180 * time.Second):
		pterm.Error.Println("Radiance engine did not become ready in time.")
		pterm.Info.Printfln("Details: %s. Re-run with --debug for the engine's own logs.", logFilePath)
		_ = proc.Process.Kill()
		return
	}

	select {
	case werr := <-exit:
		if werr != nil && ctx.Err() == nil {
			pterm.Error.Printfln("Radiance engine stopped: %v", werr)
		}
	case <-ctx.Done():
		pterm.Info.Println("Shutting down the radiance engine...")
		_ = proc.Process.Signal(syscall.SIGTERM)
		select {
		case <-exit:
		case <-time.After(15 * time.Second):
			_ = proc.Process.Kill()
		}
	}
}

// drainEngineOutput forwards the engine's logs to the log file and, in debug
// mode, to the terminal, while watching for the readiness line.
func drainEngineOutput(pipe io.Reader, ready chan<- string) {
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, readyPrefix) {
			select {
			case ready <- line:
			default:
			}
			continue
		}
		if lf, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_, _ = fmt.Fprintln(lf, line)
			_ = lf.Close()
		}
		if args.Debug {
			pterm.Debug.Println(line)
		}
	}
}

// parseReady extracts the socks and http addresses from a RADIANCE_READY line.
func parseReady(line string) (socks, httpAddr string) {
	for _, field := range strings.Fields(strings.TrimPrefix(line, readyPrefix)) {
		switch {
		case strings.HasPrefix(field, "socks="):
			socks = strings.TrimPrefix(field, "socks=")
		case strings.HasPrefix(field, "http="):
			httpAddr = strings.TrimPrefix(field, "http=")
		}
	}
	return socks, httpAddr
}

// locateRadianceEngine finds the lantern-radiance helper binary: the
// --radiance-engine flag, then LANTERN_RADIANCE_ENGINE, then a sibling of this
// executable, then $PATH.
func locateRadianceEngine() (string, error) {
	name := "lantern-radiance"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	candidates := make([]string, 0, 3)
	if p := strings.TrimSpace(args.RadianceEngine); p != "" {
		candidates = append(candidates, p)
	}
	if p := strings.TrimSpace(os.Getenv("LANTERN_RADIANCE_ENGINE")); p != "" {
		candidates = append(candidates, p)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), name))
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, nil
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("could not find the %s helper binary (looked next to this executable and in $PATH)", name)
}
