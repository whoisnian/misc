# term
A terminal utility for testing local shells, SSH servers/clients, and a browser-based HTTP terminal.

## example
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

## usage
```
  -help   bool     Show usage message and quit
  -config string   Specify file path of custom configuration json
  -d      bool     Enable debug output [CFG_DEBUG]
  -s      string   Specify shell for local/sshd/httpd mode [CFG_SHELL]
  -dir    string   Specify working directory for local/sshd/httpd mode [CFG_WORKING_DIR] (default "~")
  -local  bool     Run as local terminal [CFG_LOCAL]
  -sshd   string   Run as ssh server (e.g. user:pass@127.0.0.1:2222) [CFG_SSH_SERVER]
  -ssh    string   Run as ssh client (e.g. user:pass@127.0.0.1:2222) [CFG_SSH_CLIENT]
  -httpd  string   Run as http server (e.g. user:pass@127.0.0.1:8080) [CFG_HTTP_SERVER]
  -http   string   Run as http client (e.g. user:pass@127.0.0.1:8080) [CFG_HTTP_CLIENT]
```
