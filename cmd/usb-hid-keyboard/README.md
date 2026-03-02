# usb-hid-keyboard
USB HID Keyboard emulator with ch9329 or kcom3 serial device.

## example
```sh
# Test mode without sending keycodes
go run ./cmd/usb-hid-keyboard -t

# Send keycodes to the specified device with kcom3 encoder
go run ./cmd/usb-hid-keyboard -dev /dev/ttyUSB0 -enc kcom3
```

## key combinations
| Key Combination      | Description               |
| -------------------- | ------------------------- |
| (`Ctrl+K`, `Q`)      | Exit the program          |
| (`Ctrl+K`, `K`)      | Trigger `Ctrl+K`          |
| (`Ctrl+K`, `T`)      | Trigger `Ctrl+Alt+T`      |
| (`Ctrl+K`, `F1`)     | Trigger `Ctrl+Alt+F1`     |
| (`Ctrl+K`, `F2`)     | Trigger `Ctrl+Alt+F2`     |
| (`Ctrl+K`, `F3`)     | Trigger `Ctrl+Alt+F3`     |
| (`Ctrl+K`, `F4`)     | Trigger `Ctrl+Alt+F4`     |
| (`Ctrl+K`, `F5`)     | Trigger `Ctrl+Alt+F5`     |
| (`Ctrl+K`, `F6`)     | Trigger `Ctrl+Alt+F6`     |
| (`Ctrl+K`, `F7`)     | Trigger `Ctrl+Alt+F7`     |
| (`Ctrl+K`, `F8`)     | Trigger `Ctrl+Alt+F8`     |
| (`Ctrl+K`, `F9`)     | Trigger `Ctrl+Alt+F9`     |
| (`Ctrl+K`, `F10`)    | Trigger `Ctrl+Alt+F10`    |
| (`Ctrl+K`, `F11`)    | Trigger `Ctrl+Alt+F11`    |
| (`Ctrl+K`, `F12`)    | Trigger `Ctrl+Alt+F12`    |
| (`Ctrl+K`, `Delete`) | Trigger `Ctrl+Alt+Delete` |

## usage
```
  -help   bool     Show usage message and quit
  -config string   Specify file path of custom configuration json
  -d      bool     Enable debug output [CFG_DEBUG]
  -t      bool     Run in test mode without sending keycodes [CFG_TEST]
  -dev    string   Serial device to use [CFG_DEVICE] (default "/dev/ttyUSB0")
  -enc    string   Encoder for keycodes, ch9329 or kcom3 [CFG_ENCODER] (default "ch9329")
```
