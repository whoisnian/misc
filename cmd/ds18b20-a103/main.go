package main

import (
	"context"

	"github.com/whoisnian/misc/pkg/serial"
)

func main() {
	ctx := context.Background()
	setupConfig(ctx)
	setupLogger(ctx)
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
