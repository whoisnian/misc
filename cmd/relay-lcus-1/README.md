# relay-lcus-1
Control a single-channel LCUS relay over UART at 9600 baud.

## example
```sh
# Turn the relay ON
go run ./cmd/relay-lcus-1 -dev /dev/ttyUSB0 -s on

# Turn the relay OFF
go run ./cmd/relay-lcus-1 -dev /dev/ttyUSB0 -s off
```

## usage
```
  -help   bool     Show usage message and quit
  -config string   Specify file path of custom configuration json
  -d      bool     Enable debug output [CFG_DEBUG]
  -dev    string   Serial device to use [CFG_DEVICE] (default "/dev/ttyUSB0")
  -s      string   Set relay state to 'on' or 'off' [CFG_STATE]
```
