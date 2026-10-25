package main

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
)

const maxHelloSize = 64 << 10

// helloConn records the raw bytes of the first TLS handshake message (ClientHello) read from the connection.
type helloConn struct {
	net.Conn
	mu   sync.Mutex
	raw  []byte
	done bool
}

func (c *helloConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.mu.Lock()
		if !c.done {
			c.raw = append(c.raw, b[:n]...)
			if _, err := helloMessage(c.raw); !errors.Is(err, errShortHello) || len(c.raw) > maxHelloSize {
				c.done = true
			}
		}
		c.mu.Unlock()
	}
	return n, err
}

func (c *helloConn) Hello() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.raw
}

var errShortHello = errors.New("short client hello")

// helloMessage reassembles the ClientHello handshake message (4-byte header included) from the TLS records in raw.
func helloMessage(raw []byte) ([]byte, error) {
	var msg []byte
	for {
		if len(raw) < 5 {
			return nil, errShortHello
		}
		if raw[0] != 0x16 {
			return nil, fmt.Errorf("not a tls handshake record: 0x%02x", raw[0])
		}
		n := int(binary.BigEndian.Uint16(raw[3:5]))
		if len(raw) < 5+n {
			return nil, errShortHello
		}
		msg = append(msg, raw[5:5+n]...)
		raw = raw[5+n:]
		if len(msg) >= 4 {
			if msg[0] != 0x01 {
				return nil, fmt.Errorf("not a client hello: 0x%02x", msg[0])
			}
			if size := 4 + (int(msg[1])<<16 | int(msg[2])<<8 | int(msg[3])); len(msg) >= size {
				return msg[:size], nil
			}
		}
	}
}

// Fingerprint holds the ClientHello fingerprints. TLSCiphers, TLSExt, TLSExtLE and TLSHelloLen are computed
// the same way as Cloudflare's tlsClientCiphersSha1, tlsClientExtensionsSha1, tlsClientExtensionsSha1Le
// and tlsClientHelloLength, so they can be compared with Workers logs directly.
type Fingerprint struct {
	TLSCiphers  string   `json:"tls_ciphers"`
	TLSExt      string   `json:"tls_ext"`
	TLSExtLE    string   `json:"tls_ext_le"`
	TLSHelloLen int      `json:"tls_hello_len"`
	JA3         string   `json:"ja3"`
	JA3Hash     string   `json:"ja3_hash"`
	JA4         string   `json:"ja4"`
	SNI         string   `json:"sni,omitempty"`
	ALPN        []string `json:"alpn,omitempty"`
	Ciphers     []string `json:"ciphers"`
	Extensions  []string `json:"extensions"`
}

type helloReader struct{ b []byte }

func (r *helloReader) u8() int {
	if len(r.b) < 1 {
		r.b = nil
		return 0
	}
	v := int(r.b[0])
	r.b = r.b[1:]
	return v
}

func (r *helloReader) u16() int {
	if len(r.b) < 2 {
		r.b = nil
		return 0
	}
	v := int(binary.BigEndian.Uint16(r.b))
	r.b = r.b[2:]
	return v
}

func (r *helloReader) bytes(n int) []byte {
	if len(r.b) < n {
		r.b = nil
		return nil
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *helloReader) u16s(b []byte) (out []uint16) {
	for i := 0; i+1 < len(b); i += 2 {
		out = append(out, binary.BigEndian.Uint16(b[i:]))
	}
	return out
}

func isGrease(v uint16) bool { return v&0x0f0f == 0x0a0a && v>>8 == v&0xff }

func ParseClientHello(raw []byte) (*Fingerprint, error) {
	msg, err := helloMessage(raw)
	if err != nil {
		return nil, err
	}
	r := &helloReader{b: msg[4:]}
	version := r.u16()
	r.bytes(32)     // random
	r.bytes(r.u8()) // session id
	cipherBytes := r.bytes(r.u16())
	r.bytes(r.u8()) // compression methods
	extBytes := r.bytes(r.u16())
	if r.b == nil {
		return nil, errors.New("truncated client hello")
	}

	ciphers := r.u16s(cipherBytes)
	var extTypes, groups, sigAlgs, versions []uint16
	var pointFormats []byte
	fp := &Fingerprint{TLSHelloLen: len(msg)}
	er := &helloReader{b: extBytes}
	for len(er.b) >= 4 {
		typ := uint16(er.u16())
		data := er.bytes(er.u16())
		extTypes = append(extTypes, typ)
		d := &helloReader{b: data}
		switch typ {
		case 0: // server_name
			d.u16()
			if d.u8() == 0 {
				fp.SNI = string(d.bytes(d.u16()))
			}
		case 10: // supported_groups
			groups = d.u16s(d.bytes(d.u16()))
		case 11: // ec_point_formats
			pointFormats = d.bytes(d.u8())
		case 13: // signature_algorithms
			sigAlgs = d.u16s(d.bytes(d.u16()))
		case 16: // application_layer_protocol_negotiation
			l := &helloReader{b: d.bytes(d.u16())}
			for len(l.b) > 0 {
				fp.ALPN = append(fp.ALPN, string(l.bytes(l.u8())))
			}
		case 43: // supported_versions
			versions = d.u16s(d.bytes(d.u8()))
		}
	}

	be := make([]byte, 0, 2*len(extTypes))
	le := make([]byte, 0, 2*len(extTypes))
	for _, t := range extTypes {
		be = binary.BigEndian.AppendUint16(be, t)
		le = binary.LittleEndian.AppendUint16(le, t)
	}
	fp.TLSCiphers = b64sha1(cipherBytes)
	fp.TLSExt = b64sha1(be)
	fp.TLSExtLE = b64sha1(le)
	fp.Ciphers = hexList(ciphers, false)
	fp.Extensions = hexList(extTypes, false)

	// JA3: version,ciphers,extensions,groups,point formats (decimal, GREASE removed)
	pf := make([]uint16, len(pointFormats))
	for i, v := range pointFormats {
		pf[i] = uint16(v)
	}
	fp.JA3 = strings.Join([]string{strconv.Itoa(version), decList(ciphers), decList(extTypes), decList(groups), decList(pf)}, ",")
	sum := md5.Sum([]byte(fp.JA3))
	fp.JA3Hash = hex.EncodeToString(sum[:])

	fp.JA4 = ja4(version, ciphers, extTypes, versions, sigAlgs, fp.SNI != "", fp.ALPN)
	return fp, nil
}

// ja4 follows https://github.com/FoxIO-LLC/ja4/blob/main/technical_details/JA4.md (TCP only).
func ja4(version int, ciphers, exts, versions, sigAlgs []uint16, hasSNI bool, alpn []string) string {
	ver := uint16(version)
	for _, v := range versions {
		if !isGrease(v) && v > ver {
			ver = v
		}
	}
	verStr := map[uint16]string{0x0304: "13", 0x0303: "12", 0x0302: "11", 0x0301: "10", 0x0300: "s3"}[ver]
	if verStr == "" {
		verStr = "00"
	}
	sni := "i"
	if hasSNI {
		sni = "d"
	}
	alpnStr := "00"
	if len(alpn) > 0 && alpn[0] != "" {
		first, last := alpn[0][0], alpn[0][len(alpn[0])-1]
		if isAlnum(first) && isAlnum(last) {
			alpnStr = string([]byte{first, last})
		} else {
			h := hex.EncodeToString([]byte(alpn[0]))
			alpnStr = string([]byte{h[0], h[len(h)-1]})
		}
	}
	cs := hexList(ciphers, true)
	es := hexList(exts, true)
	a := fmt.Sprintf("t%s%s%02d%02d%s", verStr, sni, min(len(cs), 99), min(len(es), 99), alpnStr)

	slices.Sort(cs)
	b := "000000000000"
	if len(cs) > 0 {
		b = sha256Prefix(strings.Join(cs, ","))
	}
	es = slices.DeleteFunc(es, func(s string) bool { return s == "0000" || s == "0010" })
	slices.Sort(es)
	c := "000000000000"
	if len(es) > 0 {
		in := strings.Join(es, ",")
		if sa := hexList(sigAlgs, false); len(sa) > 0 {
			in += "_" + strings.Join(sa, ",")
		}
		c = sha256Prefix(in)
	}
	return a + "_" + b + "_" + c
}

func isAlnum(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

func b64sha1(b []byte) string {
	sum := sha1.Sum(b)
	return base64.StdEncoding.EncodeToString(sum[:])
}

func sha256Prefix(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}

func hexList(vs []uint16, skipGrease bool) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		if !skipGrease || !isGrease(v) {
			out = append(out, fmt.Sprintf("%04x", v))
		}
	}
	return out
}

func decList(vs []uint16) string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		if !isGrease(v) {
			out = append(out, strconv.Itoa(int(v)))
		}
	}
	return strings.Join(out, "-")
}
