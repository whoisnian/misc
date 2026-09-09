//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/whoisnian/glb/ansi"
	"github.com/whoisnian/glb/config"
	"github.com/whoisnian/glb/logger"
)

var CFG struct {
	Debug     bool    `flag:"d,false,Enable debug output"`
	Scan      bool    `flag:"scan,false,Scan the i2c bus and quit"`
	Addr      uint    `flag:"addr,0x3c,I2C address of the display"`
	Chip      string  `flag:"chip,ssd1306,Driver chip: 'ssd1306' or 'sh1106' (1.3 inch panels are usually sh1106)"`
	Size      string  `flag:"size,128x32,Display resolution like 128x32 or 128x64"`
	ColOffset int     `flag:"col-offset,-1,Column offset in GDDRAM, -1 means auto (ssd1306: 0, sh1106: 2)"`
	Speed     int     `flag:"speed,1,I2C speed: 0=20kHz 1=100kHz 2=400kHz 3=750kHz"`
	Pattern   string  `flag:"p,text,Test pattern: all|checker|border|text|sweep|off"`
	Duration  float64 `flag:"t,6,Duration in seconds of the 'sweep' pattern"`
	Reconnect bool    `flag:"reconnect,false,Rebind the kernel driver on exit"`
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

	devPath, sysName, err := FindDevice(CH341VendorID, CH341ProductID)
	if err != nil {
		LOG.Fatalf(ctx, "failed to find CH341 (%04x:%04x): %v", CH341VendorID, CH341ProductID, err)
	}
	LOG.Infof(ctx, "found CH341 at %s (%s)", devPath, sysName)

	// usbfs 节点默认只有 root 可写，非 root 运行需要额外的 udev 规则。
	dev, err := OpenDevice(ctx, devPath, ch341Interface)
	if err != nil {
		LOG.Fatalf(ctx, "failed to open %s: %v", devPath, err)
	}
	defer dev.Close(ctx, CFG.Reconnect)

	bus, err := NewCH341(ctx, dev, CFG.Speed)
	if err != nil {
		LOG.Fatalf(ctx, "failed to init CH341: %v", err)
	}

	found := bus.I2CScan()
	if len(found) == 0 {
		LOG.Fatalf(ctx, "no device acked on the i2c bus, check SDA/SCL/VCC/GND wiring and pull-up resistors")
	}
	list := make([]string, len(found))
	for i, a := range found {
		list[i] = fmt.Sprintf("0x%02x", a)
	}
	LOG.Infof(ctx, "i2c scan result: %s", strings.Join(list, ", "))
	if CFG.Scan {
		return
	}

	addr := uint8(CFG.Addr)
	if !slices.Contains(found, addr) {
		LOG.Warnf(ctx, "address 0x%02x did not ack, falling back to 0x%02x", addr, found[0])
		addr = found[0]
	}

	width, height, err := ParseSize(CFG.Size)
	if err != nil {
		LOG.Fatalf(ctx, "invalid size: %v", err)
	}
	d, err := NewDisplay(bus, addr, strings.ToLower(CFG.Chip), width, height, CFG.ColOffset)
	if err != nil {
		LOG.Fatalf(ctx, "failed to create display: %v", err)
	}
	if err = d.Init(ctx); err != nil {
		LOG.Fatalf(ctx, "failed to init display: %v", err)
	}
	if err = Render(d, strings.ToLower(CFG.Pattern), time.Duration(CFG.Duration*float64(time.Second))); err != nil {
		LOG.Fatalf(ctx, "failed to render pattern: %v", err)
	}
	LOG.Infof(ctx, "rendered pattern %q on %s %dx%d at 0x%02x (col offset %d)",
		CFG.Pattern, d.Chip(), d.Width(), d.Height(), d.Addr(), d.ColOff())
}
