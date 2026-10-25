# reqdump
Echo and record HTTP requests as received, together with TLS and HTTP/2 client fingerprints.

* TLS is terminated by reqdump itself, the raw ClientHello is kept for fingerprinting:
  * `tls_ciphers`, `tls_ext`, `tls_ext_le`, `tls_hello_len`: computed the same way as Cloudflare's `tlsClientCiphersSha1`, `tlsClientExtensionsSha1`, `tlsClientExtensionsSha1Le` and `tlsClientHelloLength`
  * `ja3`, `ja4`
* HTTP/1.x request heads are recorded verbatim (header order and case kept).
* HTTP/2 headers are decoded from the frames in received order (pseudo-headers included), with the Akamai fingerprint (`SETTINGS|WINDOW_UPDATE|PRIORITY|pseudo-header order`).
* `GET /` returns an HTML page with the dump, and its script posts what JavaScript can see (`navigator.webdriver`, UA client hints, WebGL renderer, ...) to `/js`. Other paths return the dump as plain text.
* Every request (and every failed handshake with a valid ClientHello) is appended to a jsonl file. Use `?t=<tag>` to tell test runs apart.

## example
```sh
# local test with a self-signed certificate
go run ./cmd/reqdump -l 127.0.0.1:8443 -http 127.0.0.1:8080
curl -k --http2 'https://localhost:8443/raw?t=curl'

# public server, certificates from Let's Encrypt via tls-alpn-01 on the same port
# (keep the DNS record unproxied, otherwise the CDN terminates TLS instead)
./bin/reqdump -l 0.0.0.0:443 -http 0.0.0.0:80 -acme ua.example.com -o /var/lib/reqdump/reqdump.jsonl
```

## usage
```
  -help       bool     Show usage message and quit
  -config     string   Specify file path of custom configuration json
  -d          bool     Enable debug output [CFG_DEBUG]
  -l          string   HTTPS listen addr [CFG_LISTEN_ADDR] (default "0.0.0.0:443")
  -http       string   Plain HTTP listen addr (e.g. 0.0.0.0:80), disabled if empty [CFG_HTTP_ADDR]
  -cert       string   TLS certificate file [CFG_CERT_FILE]
  -key        string   TLS private key file [CFG_KEY_FILE]
  -acme       string   Get certificates from Let's Encrypt for these hosts (comma-separated) [CFG_ACME_HOSTS]
  -acme-cache string   Directory to cache ACME certificates [CFG_ACME_CACHE] (default "acme-cache")
  -o          string   Append records to this jsonl file [CFG_OUTPUT] (default "reqdump.jsonl")
```
