# misc
A collection of small Go utilities.

## tools
* [**ksplit**](#ksplit): Split Kustomize build output.
* [**term**](#term): A versatile terminal utility.
* [**ds18b20-a103**](#ds18b20-a103): Read temperature from a DS18B20 sensor.
* [**thermocouple-k-a112**](#thermocouple-k-a112): Read temperature from a K-type thermocouple module.
* [**relay-lcus-1**](#relay-lcus-1): Control a single-channel LCUS relay.

---
### ksplit
Split a Kustomize build output into individual, sorted resource files.
```sh
# Input from stdin
kustomize build overlays/prod | go run ./cmd/ksplit -o ./output

# Input from file
go run ./cmd/ksplit -i ./final.yaml -o ./output

# Group by subdirectories per kind
go run ./cmd/ksplit -i ./final.yaml -o ./output -sub
```

---
### term
A terminal utility for testing local shells, SSH servers/clients, and a browser-based HTTP terminal.
```sh
# Run a local PTY shell in the current terminal
go run ./cmd/term -local -s bash -dir ~

# Run as SSH server with password auth (accept from normal openssh client)
go run ./cmd/term -sshd admin:password@127.0.0.1:2222 -s bash -dir ~

# Run as SSH client with password auth (connect to normal openssh server)
go run ./cmd/term -ssh admin:password@192.168.1.1:22

# Run as HTTP server with optional basic auth (visit http://127.0.0.1:8080/web in browser)
go run ./cmd/term -httpd admin:password@127.0.0.1:8080 -s bash -dir ~

# Run as HTTP client with optional basic auth (connect to existing server without browser)
go run ./cmd/term -http admin:password@127.0.0.1:8080
```

---
### ds18b20-a103
Read temperature from a DS18B20-compatible sensor via UART at 115200 baud.
```sh
# Read once and print temperature
go run ./cmd/ds18b20-a103 -dev /dev/ttyUSB0
```

---
### thermocouple-k-a112
Read temperature from a K-type thermocouple module via UART at 9600 baud.
```sh
# Read once and print temperature
go run ./cmd/thermocouple-k-a112 -dev /dev/ttyUSB0

# Restore factory settings and then read the temperature in an infinite loop
go run ./cmd/thermocouple-k-a112 -dev /dev/ttyUSB0 -r
```

---
### relay-lcus-1
Control a single-channel LCUS relay over UART at 9600 baud.
```sh
# Turn the relay ON
go run ./cmd/relay-lcus-1 -dev /dev/ttyUSB0 -s on

# Turn the relay OFF
go run ./cmd/relay-lcus-1 -dev /dev/ttyUSB0 -s off
```
