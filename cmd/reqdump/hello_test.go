package main

import (
	"crypto/tls"
	"encoding/binary"
	"net"
	"slices"
	"testing"
)

// Compare with what crypto/tls itself parsed from the same ClientHello.
func TestParseClientHello(t *testing.T) {
	cli, srv := net.Pipe()
	defer cli.Close()
	hc := &helloConn{Conn: srv}
	infoCh := make(chan *tls.ClientHelloInfo, 1)
	go func() {
		tc := tls.Server(hc, &tls.Config{GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
			infoCh <- info
			return nil, net.ErrClosed // stop here, the ClientHello is all we need
		}})
		tc.Handshake()
		tc.Close()
	}()
	go tls.Client(cli, &tls.Config{ServerName: "example.com", NextProtos: []string{"h2", "http/1.1"}}).Handshake()
	info := <-infoCh

	fp, err := ParseClientHello(hc.Hello())
	if err != nil {
		t.Fatal(err)
	}
	var ciphers, exts []byte
	for _, c := range info.CipherSuites {
		ciphers = binary.BigEndian.AppendUint16(ciphers, c)
	}
	for _, e := range info.Extensions {
		exts = binary.BigEndian.AppendUint16(exts, e)
	}
	if want := b64sha1(ciphers); fp.TLSCiphers != want {
		t.Errorf("tls_ciphers = %s, want %s", fp.TLSCiphers, want)
	}
	if want := b64sha1(exts); fp.TLSExt != want {
		t.Errorf("tls_ext = %s, want %s", fp.TLSExt, want)
	}
	if fp.SNI != "example.com" || !slices.Equal(fp.ALPN, []string{"h2", "http/1.1"}) {
		t.Errorf("sni = %q, alpn = %q", fp.SNI, fp.ALPN)
	}
	if fp.JA4[:10] != "t13d"+fp.JA4[4:8]+"h2" {
		t.Errorf("ja4 = %s", fp.JA4)
	}
}

func TestIsGrease(t *testing.T) {
	for _, v := range []uint16{0x0a0a, 0x1a1a, 0xfafa} {
		if !isGrease(v) {
			t.Errorf("isGrease(%#04x) = false", v)
		}
	}
	for _, v := range []uint16{0x0a1a, 0x1301, 0x0000} {
		if isGrease(v) {
			t.Errorf("isGrease(%#04x) = true", v)
		}
	}
}
