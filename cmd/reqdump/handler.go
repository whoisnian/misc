package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const maxBodySize = 64 << 10

// Record is one line of the jsonl output.
type Record struct {
	Time     string   `json:"t"`
	Remote   string   `json:"remote"`
	Proto    string   `json:"proto,omitempty"`
	Method   string   `json:"method,omitempty"`
	URI      string   `json:"uri,omitempty"`
	Tag      string   `json:"tag,omitempty"`
	Head     string   `json:"head,omitempty"` // HTTP/1.x request head, verbatim
	Headers  []Header `json:"headers,omitempty"`
	H2       string   `json:"h2,omitempty"` // Akamai HTTP/2 fingerprint: SETTINGS|WINDOW_UPDATE|PRIORITY|pseudo-header order
	Body     string   `json:"body,omitempty"`
	BodyB64  bool     `json:"body_b64,omitempty"`
	BodySize int64    `json:"body_size,omitempty"`
	TLS      *tlsInfo `json:"tls,omitempty"`
	Error    string   `json:"error,omitempty"`
}

type recorder struct {
	mu sync.Mutex
	f  *os.File
}

func newRecorder(path string) (*recorder, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &recorder{f: f}, nil
}

func (rc *recorder) Write(r *Record) {
	b, err := json.Marshal(r)
	if err != nil {
		LOG.Errorf(context.Background(), "json.Marshal failed: %v", err)
		return
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if _, err = rc.f.Write(append(b, '\n')); err != nil {
		LOG.Errorf(context.Background(), "write record failed: %v", err)
	}
}

var RECORDER *recorder

func dumpHandler(w http.ResponseWriter, r *http.Request) {
	rec := &Record{
		Time:   time.Now().Format(time.RFC3339Nano),
		Remote: r.RemoteAddr,
		Proto:  r.Proto,
		Method: r.Method,
		URI:    r.RequestURI,
		Tag:    r.URL.Query().Get("t"),
	}
	info := getConnInfo(r.Context())
	if info != nil {
		rec.TLS = info.TLS
		if info.h2 {
			rec.Headers, rec.H2 = info.takeH2(r.Method, r.RequestURI)
		} else {
			rec.Head, rec.Headers = info.takeH1(r.Method, r.RequestURI)
		}
	}

	body, _ := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	rest, _ := io.Copy(io.Discard, r.Body)
	rec.BodySize = int64(len(body)) + rest
	if utf8.Valid(body) {
		rec.Body = string(body)
	} else {
		rec.Body, rec.BodyB64 = base64.StdEncoding.EncodeToString(body), true
	}
	RECORDER.Write(rec)
	LOG.Infof(r.Context(), "%s %s %s %s tag=%q ua=%q", r.RemoteAddr, r.Proto, r.Method, r.RequestURI, rec.Tag, r.UserAgent())

	w.Header().Set("Cache-Control", "no-store")
	switch {
	case r.URL.Path == "/robots.txt":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "User-agent: *\nAllow: /\n")
	case r.URL.Path == "/favicon.ico":
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/" && r.Method == http.MethodGet:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, indexHTML, html.EscapeString(dumpText(rec)))
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, dumpText(rec))
	}
}

func dumpText(rec *Record) string {
	var sb strings.Builder
	if rec.Head != "" {
		sb.WriteString(rec.Head + "\n")
	} else {
		fmt.Fprintf(&sb, "%s %s %s\n", rec.Method, rec.URI, rec.Proto)
		for _, h := range rec.Headers {
			fmt.Fprintf(&sb, "%s: %s\n", h[0], h[1])
		}
	}
	if rec.Body != "" {
		sb.WriteString("\n" + rec.Body + "\n")
	}
	sb.WriteString("\n----\n")
	fmt.Fprintf(&sb, "remote: %s\n", rec.Remote)
	if t := rec.TLS; t != nil {
		fmt.Fprintf(&sb, "tls: %s %s alpn=%q sni=%q\n", t.Version, t.Cipher, t.ALPN, t.ServerName)
		if fp := t.Hello; fp != nil {
			fmt.Fprintf(&sb, "tls_ciphers: %s\ntls_ext: %s\ntls_ext_le: %s\ntls_hello_len: %d\n", fp.TLSCiphers, fp.TLSExt, fp.TLSExtLE, fp.TLSHelloLen)
			fmt.Fprintf(&sb, "ja3: %s\nja4: %s\n", fp.JA3Hash, fp.JA4)
		}
	}
	if rec.H2 != "" {
		fmt.Fprintf(&sb, "h2: %s\n", rec.H2)
	}
	return sb.String()
}

// indexHTML shows the dump, then posts what JavaScript can see to /js, so browser-based agents show up twice.
const indexHTML = `<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>reqdump</title></head>
<body>
<h3>request</h3>
<pre id="req">%s</pre>
<h3>javascript</h3>
<pre id="js">(no javascript)</pre>
<script>
(async () => {
  const d = {
    ua: navigator.userAgent, webdriver: navigator.webdriver, languages: navigator.languages, platform: navigator.platform,
    cores: navigator.hardwareConcurrency, memory: navigator.deviceMemory, plugins: navigator.plugins.length,
    screen: [screen.width, screen.height, screen.colorDepth, devicePixelRatio], viewport: [innerWidth, innerHeight],
    tz: Intl.DateTimeFormat().resolvedOptions().timeZone,
  };
  try {
    if (navigator.userAgentData) d.uach = await navigator.userAgentData.getHighEntropyValues(
      ['architecture', 'bitness', 'model', 'platformVersion', 'fullVersionList']);
  } catch (e) { d.uach_error = String(e); }
  try {
    const gl = document.createElement('canvas').getContext('webgl');
    const ext = gl.getExtension('WEBGL_debug_renderer_info');
    d.gpu = [gl.getParameter(ext.UNMASKED_VENDOR_WEBGL), gl.getParameter(ext.UNMASKED_RENDERER_WEBGL)];
  } catch (e) { d.gpu_error = String(e); }
  document.getElementById('js').textContent = JSON.stringify(d, null, 2);
  const r = await fetch('/js' + location.search, {method: 'POST', headers: {'content-type': 'application/json'}, body: JSON.stringify(d)});
  document.getElementById('js').textContent = await r.text();
})();
</script>
</body></html>
`
