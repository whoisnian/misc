# ch341-oled
Test an SSD1306 or SH1106 I2C OLED module connected through a CH341 USB to I2C adapter.

Mainline linux has no i2c adapter driver for the CH341, and its interface is claimed by `spi-ch341`,
so this tool talks to the chip directly over usbfs: it detaches the kernel driver from the interface
while running, and `-reconnect` hands the device back on exit.

Writing to `/dev/bus/usb/*/*` needs root. To run it as a normal user instead:
```sh
echo 'SUBSYSTEM=="usb", ATTR{idVendor}=="1a86", ATTR{idProduct}=="5512", MODE="0660", GROUP="uucp"' | sudo tee /etc/udev/rules.d/99-ch341-i2c.rules
sudo udevadm control --reload && sudo udevadm trigger
```

Tested panels:

| panel     | resolution | chip    | options                         |
| --------- | ---------- | ------- | ------------------------------- |
| 0.91 inch | 128x32     | SSD1306 | (defaults)                      |
| 0.96 inch | 128x64     | SSD1306 | `-size 128x64`                  |
| 1.3 inch  | 128x64     | SH1106  | `-size 128x64 -chip sh1106`     |

The `border` pattern doubles as a diagnostic: a frame flush against all four edges means the multiplex
ratio matches the panel, and diagonals crossing at the exact center mean the COM pin config and the
column offset are right. A frame shifted by two columns usually means an SH1106 driven as an SSD1306.

## example
```sh
# scan the i2c bus, the display usually acks at 0x3c
sudo go run ./cmd/ch341-oled -scan

# 0.91 inch SSD1306 128x32
sudo go run ./cmd/ch341-oled -p text

# 0.96 inch SSD1306 128x64
sudo go run ./cmd/ch341-oled -size 128x64 -p border

# 1.3 inch SH1106 128x64
sudo go run ./cmd/ch341-oled -size 128x64 -chip sh1106 -p sweep

# turn the display off
sudo go run ./cmd/ch341-oled -p off
```

## usage
```
  -help       bool     Show usage message and quit
  -config     string   Specify file path of custom configuration json
  -d          bool     Enable debug output [CFG_DEBUG]
  -scan       bool     Scan the i2c bus and quit [CFG_SCAN]
  -addr       uint     I2C address of the display [CFG_ADDR] (default 60)
  -chip       string   Driver chip: 'ssd1306' or 'sh1106' (1.3 inch panels are usually sh1106) [CFG_CHIP] (default "ssd1306")
  -size       string   Display resolution like 128x32 or 128x64 [CFG_SIZE] (default "128x32")
  -col-offset int      Column offset in GDDRAM, -1 means auto (ssd1306: 0, sh1106: 2) [CFG_COL_OFFSET] (default -1)
  -speed      int      I2C speed: 0=20kHz 1=100kHz 2=400kHz 3=750kHz [CFG_SPEED] (default 1)
  -p          string   Test pattern: all|checker|border|text|sweep|off [CFG_PATTERN] (default "text")
  -t          float64  Duration in seconds of the 'sweep' pattern [CFG_DURATION] (default 6)
  -reconnect  bool     Rebind the kernel driver on exit [CFG_RECONNECT]
```
