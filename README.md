# misc
A collection of small Go utilities.

## tools
* [**ksplit**](cmd/ksplit): Split Kustomize build output.
* [**term**](cmd/term): A versatile terminal utility.
* [**mockcas**](cmd/mockcas): Mock CAS server and client for testing CAS Protocol 3.0.
* [**ds18b20-a103**](cmd/ds18b20-a103): Read temperature from a DS18B20 sensor.
* [**thermocouple-k-a112**](cmd/thermocouple-k-a112): Read temperature from a K-type thermocouple module.
* [**relay-lcus-1**](cmd/relay-lcus-1): Control a single-channel LCUS relay.

## build and run
```sh
# build all tools
go build -o ./bin/ ./cmd/...

# build specific tool (e.g., ksplit)
go build -o ./bin/ ./cmd/ksplit

# run specific tool (e.g., ksplit)
./bin/ksplit -help
```
