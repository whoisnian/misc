# mockcas
Mock CAS server and client for testing [CAS Protocol 3.0](https://apereo.github.io/cas/7.3.x/protocol/CAS-Protocol-Specification.html).

## example
Start service and visit http://192.168.1.2:9090/app/login in browser.  
Login with default username `casuser` and password `Mellon`.  
```sh
go run ./cmd/mockcas
# 2026-01-02 00:09:12 [I] using cas server url prefix:  http://192.168.1.2:9090/cas
# 2026-01-02 00:09:12 [I] using cas client service url: http://192.168.1.2:9090/app/validate
# 2026-01-02 00:09:12 [I] service httpd started: http://0.0.0.0:9090
```

## usage
```
  -help   bool     Show usage message and quit
  -config string   Specify file path of custom configuration json
  -d      bool     Enable debug output [CFG_DEBUG]
  -l      string   Server listen addr [CFG_LISTEN_ADDR] (default "0.0.0.0:9090")
  -s      string   URL prefix of the CAS server, auto detected if empty [CFG_SERVER_URL_PREFIX]
  -c      string   Service URL of the CAS client application, auto detected if empty [CFG_CLIENT_SERVICE_URL]
```
