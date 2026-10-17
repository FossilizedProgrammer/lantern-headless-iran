package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getlantern/golog"
	"github.com/pterm/pterm"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/getlantern/appdir"
	"github.com/oldblocker/lantern-headless-iran/deviceid"
)

func setupOutput() {
	if args.Debug {
		pterm.EnableDebugMessages()
	}
	if args.Quiet {
		pterm.DisableDebugMessages()
	}
	if args.Raw {
		pterm.DisableColor()
	}
}

func setupLog() *lumberjack.Logger {
	logWriter := &lumberjack.Logger{
		Filename:   logFilePath,
		MaxSize:    10, // megabytes
		MaxBackups: 5,
	}

	golog.SetOutputs(logWriter, logWriter)
	golog.SetPrepender(func(writer io.Writer) {
		_, _ = writer.Write([]byte(fmt.Sprintf("%s: ", time.Now().Format("2006-01-02 15:04:05"))))
	})
	return logWriter
}

func setupDataFolder() {
	if args.DataPath == "" {
		args.DataPath = filepath.Join(appdir.InHomeDir(".lantern-headless"))
	}

	if err := os.Mkdir(args.DataPath, 0755); err != nil && !os.IsExist(err) {
		pterm.Error.Printfln("Unable to create folder to store data, defaulting to current directory. Error: %s", err.Error())
		args.DataPath = "."
	}
	configFilePath = filepath.Join(args.DataPath, "config.toml")
	logFilePath = filepath.Join(args.DataPath, "lantern-headless.log")
}

func main() {
	parseArgs()
	setupOutput()
	setupDataFolder()
	deviceId = deviceid.Get(args.DataPath)
	logWriter := setupLog()
	defer func(logWriter *lumberjack.Logger) {
		_ = logWriter.Close()
	}(logWriter)
	ctx := context.Background()
	applyClientOptions()
	switch {
	case args.Auth != nil:
		authCmd(ctx, args.Auth, args.AuthURL)
	case args.Serve != nil:
		switch strings.ToLower(strings.TrimSpace(args.Serve.Engine)) {
		case "radiance", "beta":
			serveRadiance(ctx, args.Serve)
		default:
			readAuth()
			serve(args.Serve)
		}
	case args.Servers != nil:
		serversCmd(args.Servers)
	case args.Update != nil:
		updateServersCmd(args.Update)
	default:
		printHelp()
	}
}
