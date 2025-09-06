package main

import (
	"context"
	"os"

	"github.com/whoisnian/glb/ansi"
	"github.com/whoisnian/glb/config"
	"github.com/whoisnian/glb/logger"
	"github.com/whoisnian/misc/pkg/serial"
)

var CFG struct {
	Debug  bool   `flag:"d,false,Enable debug output"`
	Device string `flag:"dev,/dev/ttyUSB0,Serial device to use"`
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

	ttyPort, err := serial.Open(CFG.Device, 115200, 8, serial.ParityNone, serial.StopBits1)
	if err != nil {
		LOG.Fatalf(ctx, "failed to open serial port %s: %v", CFG.Device, err)
	}
	defer ttyPort.Close()

	data, err := requestReadID(ctx, ttyPort)
	if err != nil {
		LOG.Fatalf(ctx, "requestReadID failed: %v", err)
	}
	LOG.Infof(ctx, "connected to %X", data[2:10])

	if err = requestWriteConfig(ctx, ttyPort, Precision12bit); err != nil {
		LOG.Fatalf(ctx, "requestWriteConfig failed: %v", err)
	}
	if err = requestConvert(ctx, ttyPort); err != nil {
		LOG.Fatalf(ctx, "requestConvert failed: %v", err)
	}

	data, err = requestReadConfig(ctx, ttyPort)
	if err != nil {
		LOG.Fatalf(ctx, "requestReadConfig failed: %v", err)
	}
	LOG.Infof(ctx, "temperature: %.4f°C", DecodeTemperature(data[2:4], Precision(data[6])))
}
