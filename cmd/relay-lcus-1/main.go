package main

import (
	"context"
	"os"
	"strings"

	"github.com/whoisnian/glb/ansi"
	"github.com/whoisnian/glb/config"
	"github.com/whoisnian/glb/logger"
	"github.com/whoisnian/misc/pkg/serial"
)

var CFG struct {
	Debug  bool   `flag:"d,false,Enable debug output"`
	Device string `flag:"dev,/dev/ttyUSB0,Serial device to use"`
	State  string `flag:"s,,Set relay state to 'on' or 'off'"`
}

var LOG *logger.Logger

func setupConfigAndLogger(_ context.Context) {
	_, err := config.FromCommandLine(&CFG)
	if err != nil {
		panic(err)
	}
	level := logger.LevelInfo
	if CFG.Debug {
		level = logger.LevelDebug
	}
	LOG = logger.New(logger.NewNanoHandler(os.Stderr, logger.Options{
		Level:     level,
		Colorful:  ansi.IsSupported(os.Stderr.Fd()),
		AddSource: CFG.Debug,
	}))
}

func main() {
	ctx := context.Background()
	setupConfigAndLogger(ctx)
	LOG.Debugf(ctx, "use config: %+v", CFG)

	ttyPort, err := serial.Open(CFG.Device, 9600, 8, serial.ParityNone, serial.StopBits1)
	if err != nil {
		LOG.Fatalf(ctx, "failed to open serial port %s: %v", CFG.Device, err)
	}
	defer ttyPort.Close()

	switch strings.ToLower(CFG.State) {
	case "on":
		if err := requestTurnOn(ctx, ttyPort); err != nil {
			LOG.Fatalf(ctx, "requestTurnOn failed: %v", err)
		}
	case "off":
		if err := requestTurnOff(ctx, ttyPort); err != nil {
			LOG.Fatalf(ctx, "requestTurnOff failed: %v", err)
		}
	default:
		LOG.Fatalf(ctx, "invalid state %q, must be 'on' or 'off'", CFG.State)
	}
}
