# static-fallback
Static file server with suffix-based fallback matching.

## example
```sh
# Serve files from ./workspace, and fallback to ./fallback if not found in workspace.
go run ./cmd/static-fallback -d -l 0.0.0.0:8080 -b ./workspace -f ./fallback -live
```

## usage
```
  -help   bool     Show usage message and quit
  -config string   Specify file path of custom configuration json
  -d      bool     Enable debug output [CFG_DEBUG]
  -l      string   Server listen addr [CFG_LISTEN_ADDR] (default "0.0.0.0:54321")
  -b      string   Base directory to serve files from [CFG_BASE_DIR] (default "base")
  -f      string   Fallback directories to serve files if not found (comma-separated) [CFG_FALLBACKS]
  -live   bool     Enable fallback files live rebuild (at most once per 60s) [CFG_LIVE]
```
