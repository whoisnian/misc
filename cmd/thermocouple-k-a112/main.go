package main

import (
	"bytes"
	"context"
	"os"
	"time"

	"github.com/whoisnian/glb/ansi"
	"github.com/whoisnian/glb/config"
	"github.com/whoisnian/glb/logger"
	"github.com/whoisnian/misc/pkg/serial"
)

var CFG struct {
	Debug   bool   `flag:"d,false,Enable debug output"`
	Device  string `flag:"dev,/dev/ttyUSB0,Serial device to use"`
	Restore bool   `flag:"r,false,Restore factory settings (Baudrate9600, WorkModeAuto, DataFormatString)"`
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

	if CFG.Restore {
		if err = requestRestoreFactory(ctx, ttyPort, false); err != nil {
			LOG.Fatalf(ctx, "requestRestoreFactory failed: %v", err)
		}
		if err = requestReset(ctx, ttyPort, false); err != nil {
			LOG.Fatalf(ctx, "requestReset failed: %v", err)
		}
		LOG.Info(ctx, "restore factory settings and reset successfully")

		time.Sleep(time.Millisecond * 100)
		LOG.Info(ctx, "flush input/output buffers and try to read in a loop (press Ctrl+C to stop)")
		ttyPort.Flush()

		buf := make([]byte, 1024)
		for idx := 0; ; idx++ {
			if n, err := ttyPort.Read(buf); err != nil {
				LOG.Fatalf(ctx, "read failed: %v", err)
			} else {
				LOG.Infof(ctx, "%d read %d bytes: %X (%s)", idx, n, buf[:n], bytes.TrimSpace(buf[:n]))
			}
		}
	} else {
		if err = requestWriteSerialConfig(ctx, ttyPort, SerialBaudrate9600, false); err != nil {
			LOG.Fatalf(ctx, "requestWriteSerialConfig failed: %v", err)
		}
		if err = requestWriteModeConfig(ctx, ttyPort, WorkModeTTL, DataFormatHex, 1, false); err != nil {
			LOG.Fatalf(ctx, "requestWriteModeConfig failed: %v", err)
		}
		if err = requestReset(ctx, ttyPort, false); err != nil {
			LOG.Fatalf(ctx, "requestReset failed: %v", err)
		}
		LOG.Info(ctx, "set serial and mode config successfully")

		time.Sleep(time.Millisecond * 100)
		LOG.Info(ctx, "flush input/output buffers and try to read temperature")
		ttyPort.Flush()

		data, err := requestConvert(ctx, ttyPort, DataFormatString)
		if err != nil {
			LOG.Fatalf(ctx, "requestConvert failed: %v", err)
		}
		LOG.Infof(ctx, "temperature from str: %s°C", bytes.TrimSpace(data))
		data, err = requestRead(ctx, ttyPort, DataFormatHex)
		if err != nil {
			LOG.Fatalf(ctx, "requestRead failed: %v", err)
		}
		LOG.Infof(ctx, "temperature from hex: %.1f°C", float64(int16(uint16(data[8])<<8|uint16(data[9])))/10)
	}
}
