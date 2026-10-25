package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"

	"golang.org/x/net/http2/hpack"
)

const (
	h2Preface   = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"
	maxTapBytes = 1 << 20
)

// Header is a header field as received: original order and (for HTTP/1.x) original case.
type Header [2]string

// tapConn sees the plaintext stream before net/http parses it, so that header order and case are kept.
// HTTP/1.x request heads are kept verbatim; HTTP/2 frames are decoded with a mirrored HPACK decoder.
type tapConn struct {
	net.Conn
	info *connInfo
}

func (c *tapConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.info.feed(b[:n])
	}
	return n, err
}

// ConnectionState lets x/net/http2 verify the TLS connection and fill Request.TLS.
func (c *tapConn) ConnectionState() tls.ConnectionState {
	if tc, ok := c.Conn.(*tls.Conn); ok {
		return tc.ConnectionState()
	}
	return tls.ConnectionState{}
}

type h2Block struct {
	stream  uint32
	fields  []hpack.HeaderField
	claimed bool
}

// connInfo is shared by all requests on one connection.
type connInfo struct {
	Remote string
	TLS    *tlsInfo
	h2     bool

	mu  sync.Mutex
	buf []byte // h1: plaintext not yet matched to a request; h2: incomplete frame bytes

	// HTTP/2 only
	preface   bool
	dec       *hpack.Decoder
	fragment  []byte
	fragSid   uint32
	blocks    []*h2Block
	settings  string
	window    string
	priority  []string
	sawHeader bool
}

type tlsInfo struct {
	Version    string       `json:"version,omitempty"`
	Cipher     string       `json:"cipher,omitempty"`
	ALPN       string       `json:"alpn,omitempty"`
	ServerName string       `json:"server_name,omitempty"`
	Hello      *Fingerprint `json:"hello"`
}

type connKey struct{}

func withConnInfo(ctx context.Context, info *connInfo) context.Context {
	return context.WithValue(ctx, connKey{}, info)
}

func getConnInfo(ctx context.Context) *connInfo {
	info, _ := ctx.Value(connKey{}).(*connInfo)
	return info
}

func (ci *connInfo) feed(b []byte) {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	ci.buf = append(ci.buf, b...)
	if !ci.h2 {
		if len(ci.buf) > maxTapBytes { // a large body we do not need verbatim
			ci.buf = append(ci.buf[:0], ci.buf[len(ci.buf)-64<<10:]...)
		}
		return
	}
	if !ci.preface {
		if len(ci.buf) < len(h2Preface) {
			return
		}
		ci.buf = ci.buf[len(h2Preface):]
		ci.preface = true
	}
	for len(ci.buf) >= 9 {
		size := int(ci.buf[0])<<16 | int(ci.buf[1])<<8 | int(ci.buf[2])
		if len(ci.buf) < 9+size {
			break
		}
		ci.frame(ci.buf[3], ci.buf[4], binary.BigEndian.Uint32(ci.buf[5:9])&0x7fffffff, ci.buf[9:9+size])
		ci.buf = ci.buf[9+size:]
	}
	if len(ci.buf) == 0 {
		ci.buf = nil
	}
}

// frame records what the Akamai HTTP/2 fingerprint needs and decodes header blocks in order.
func (ci *connInfo) frame(typ, flags byte, sid uint32, p []byte) {
	switch typ {
	case 0x4: // SETTINGS
		if flags&0x1 != 0 || ci.settings != "" {
			return
		}
		var s []string
		for i := 0; i+6 <= len(p); i += 6 {
			s = append(s, fmt.Sprintf("%d:%d", binary.BigEndian.Uint16(p[i:]), binary.BigEndian.Uint32(p[i+2:])))
		}
		ci.settings = strings.Join(s, ";")
	case 0x8: // WINDOW_UPDATE
		if sid == 0 && ci.window == "" && len(p) >= 4 {
			ci.window = fmt.Sprint(binary.BigEndian.Uint32(p) & 0x7fffffff)
		}
	case 0x2: // PRIORITY
		if !ci.sawHeader && len(p) >= 5 {
			ci.priority = append(ci.priority, priorityString(sid, p))
		}
	case 0x1: // HEADERS
		if flags&0x8 != 0 && len(p) > 0 { // PADDED
			pad := int(p[0])
			p = p[1:]
			if pad > len(p) {
				return
			}
			p = p[:len(p)-pad]
		}
		if flags&0x20 != 0 && len(p) >= 5 { // PRIORITY, not part of the Akamai fingerprint (only PRIORITY frames are)
			p = p[5:]
		}
		ci.sawHeader = true
		ci.fragSid, ci.fragment = sid, append([]byte(nil), p...)
		if flags&0x4 != 0 {
			ci.decode()
		}
	case 0x9: // CONTINUATION
		ci.fragment = append(ci.fragment, p...)
		if flags&0x4 != 0 {
			ci.decode()
		}
	}
}

func priorityString(sid uint32, p []byte) string {
	dep := binary.BigEndian.Uint32(p)
	excl := dep >> 31
	return fmt.Sprintf("%d:%d:%d:%d", sid, excl, dep&0x7fffffff, int(p[4])+1)
}

func (ci *connInfo) decode() {
	if ci.dec == nil {
		ci.dec = hpack.NewDecoder(4096, nil)
	}
	fields, err := ci.dec.DecodeFull(ci.fragment)
	ci.fragment = nil
	if err != nil {
		LOG.Warnf(context.Background(), "hpack decode from %s failed: %v", ci.Remote, err)
		return
	}
	ci.blocks = append(ci.blocks, &h2Block{stream: ci.fragSid, fields: fields})
	if len(ci.blocks) > 256 { // trailers and other blocks nobody claims
		ci.blocks = ci.blocks[len(ci.blocks)-128:]
	}
}

// takeH1 returns the verbatim head of the HTTP/1.x request whose request line is "method uri HTTP/...".
func (ci *connInfo) takeH1(method, uri string) (string, []Header) {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	line := []byte(method + " " + uri + " HTTP/")
	i := bytes.Index(ci.buf, line)
	if i < 0 {
		return "", nil
	}
	j := bytes.Index(ci.buf[i:], []byte("\r\n\r\n"))
	if j < 0 {
		return "", nil
	}
	head := string(ci.buf[i : i+j])
	ci.buf = ci.buf[i+j+4:]

	lines := strings.Split(head, "\r\n")
	headers := make([]Header, 0, len(lines)-1)
	for _, l := range lines[1:] {
		k, v, _ := strings.Cut(l, ":")
		headers = append(headers, Header{k, strings.TrimLeft(v, " \t")})
	}
	return head, headers
}

// takeH2 returns the first unclaimed header block whose :method and :path match, with the h2 fingerprint.
func (ci *connInfo) takeH2(method, path string) (headers []Header, akamai string) {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	for _, b := range ci.blocks {
		if b.claimed || pseudo(b.fields, ":method") != method || pseudo(b.fields, ":path") != path {
			continue
		}
		b.claimed = true
		var order []string
		for _, f := range b.fields {
			headers = append(headers, Header{f.Name, f.Value})
			if strings.HasPrefix(f.Name, ":") {
				order = append(order, f.Name[1:2])
			}
		}
		prio := "0"
		if len(ci.priority) > 0 {
			prio = strings.Join(ci.priority, ",")
		}
		window := ci.window
		if window == "" {
			window = "00"
		}
		return headers, ci.settings + "|" + window + "|" + prio + "|" + strings.Join(order, ",")
	}
	return nil, ""
}

func pseudo(fields []hpack.HeaderField, name string) string {
	for _, f := range fields {
		if f.Name == name {
			return f.Value
		}
	}
	return ""
}
