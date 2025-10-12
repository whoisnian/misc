# ds18b20-a103
Read temperature from a DS18B20-compatible sensor via UART at 115200 baud.

## example
```sh
# Read once and print temperature
go run ./cmd/ds18b20-a103 -dev /dev/ttyUSB0
```

## usage
```
  -help   bool     Show usage message and quit
  -config string   Specify file path of custom configuration json
  -d      bool     Enable debug output [CFG_DEBUG]
  -dev    string   Serial device to use [CFG_DEVICE] (default "/dev/ttyUSB0")
```
