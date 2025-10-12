# thermocouple-k-a112
Read temperature from a K-type thermocouple module via UART at 9600 baud.

## example
```sh
# Read once and print temperature
go run ./cmd/thermocouple-k-a112 -dev /dev/ttyUSB0

# Restore factory settings and then read the temperature in an infinite loop
go run ./cmd/thermocouple-k-a112 -dev /dev/ttyUSB0 -r
```

## usage
```
  -help   bool     Show usage message and quit
  -config string   Specify file path of custom configuration json
  -d      bool     Enable debug output [CFG_DEBUG]
  -dev    string   Serial device to use [CFG_DEVICE] (default "/dev/ttyUSB0")
  -r      bool     Restore factory settings (Baudrate9600, WorkModeAuto, DataFormatString) [CFG_RESTORE]
```
