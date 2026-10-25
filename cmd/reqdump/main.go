package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/whoisnian/glb/ansi"
	"github.com/whoisnian/glb/config"
	"github.com/whoisnian/glb/logger"
	"github.com/whoisnian/glb/util/osutil"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/net/http2"
)

var CFG struct {
	Debug      bool   `flag:"d,false,Enable debug output"`
	ListenAddr string `flag:"l,0.0.0.0:443,HTTPS listen addr"`
	HTTPAddr   string `flag:"http,,Plain HTTP listen addr (e.g. 0.0.0.0:80), disabled if empty"`
	CertFile   string `flag:"cert,,TLS certificate file"`
	KeyFile    string `flag:"key,,TLS private key file"`
	ACMEHosts  string `flag:"acme,,Get certificates from Let's Encrypt for these hosts (comma-separated)"`
	ACMECache  string `flag:"acme-cache,acme-cache,Directory to cache ACME certificates"`
	Output     string `flag:"o,reqdump.jsonl,Append records to this jsonl file"`
}

var LOG *logger.Logger

func setupConfigAndLogger(_ context.Context) {
	_, err := config.FromCommandLine(&CFG)
	if err != nil {
		panic(err)
	}
	level := logger.LevelInfo
	if CFG.Debug {
		level = logger.LevelDebug
	}
	LOG = logger.New(logger.NewNanoHandler(os.Stderr, logger.Options{
		Level:     level,
		Colorful:  ansi.IsSupported(os.Stderr.Fd()),
		AddSource: CFG.Debug,
	}))
}

func setupTLSConfig(ctx context.Context) *tls.Config {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS10, // accept old clients too, they are part of what we want to see
		NextProtos: []string{"h2", "http/1.1"},
	}
	switch {
	case CFG.ACMEHosts != "":
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(strings.Split(CFG.ACMEHosts, ",")...),
			Cache:      autocert.DirCache(CFG.ACMECache),
		}
		cfg.GetCertificate = m.GetCertificate
		cfg.NextProtos = append(cfg.NextProtos, acme.ALPNProto) // tls-alpn-01 challenge on the same port
		LOG.Infof(ctx, "use ACME certificates for %s", CFG.ACMEHosts)
	case CFG.CertFile != "":
		cert, err := tls.LoadX509KeyPair(CFG.CertFile, CFG.KeyFile)
		if err != nil {
			panic(err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	default:
		cfg.Certificates = []tls.Certificate{selfSignedCert()}
		LOG.Warn(ctx, "no certificate specified, use a self-signed one")
	}
	return cfg
}

func selfSignedCert() tls.Certificate {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "reqdump"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// chanListener hands connections that have been accepted (and tapped) elsewhere to http.Server.
type chanListener struct {
	addr net.Addr
	ch   chan net.Conn
	done chan struct{}
	once sync.Once
}

func newChanListener(addr net.Addr) *chanListener {
	return &chanListener{addr: addr, ch: make(chan net.Conn), done: make(chan struct{})}
}

func (l *chanListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *chanListener) Push(c net.Conn) {
	select {
	case l.ch <- c:
	case <-l.done:
		c.Close()
	}
}

func (l *chanListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *chanListener) Addr() net.Addr { return l.addr }

func serveTLS(ctx context.Context, c net.Conn, cfg *tls.Config, h1 *chanListener, h2s *http2.Server, base *http.Server) {
	hc := &helloConn{Conn: c}
	tc := tls.Server(hc, cfg)
	tc.SetDeadline(time.Now().Add(10 * time.Second))
	herr := tc.HandshakeContext(ctx)
	tc.SetDeadline(time.Time{})

	info := &connInfo{Remote: c.RemoteAddr().String()}
	fp, perr := ParseClientHello(hc.Hello())
	if herr != nil {
		tc.Close()
		if perr != nil { // not even a ClientHello, most likely a scanner
			LOG.Debugf(ctx, "%s handshake failed: %v (%v)", info.Remote, herr, perr)
			return
		}
		LOG.Infof(ctx, "%s handshake failed: %v", info.Remote, herr)
		RECORDER.Write(&Record{Time: time.Now().Format(time.RFC3339Nano), Remote: info.Remote, TLS: &tlsInfo{Hello: fp}, Error: herr.Error()})
		return
	}
	if perr != nil {
		LOG.Warnf(ctx, "%s parse client hello failed: %v", info.Remote, perr)
	}

	st := tc.ConnectionState()
	if st.NegotiatedProtocol == acme.ALPNProto { // answered inside the handshake by autocert
		tc.Close()
		return
	}
	info.TLS = &tlsInfo{
		Version:    tls.VersionName(st.Version),
		Cipher:     tls.CipherSuiteName(st.CipherSuite),
		ALPN:       st.NegotiatedProtocol,
		ServerName: st.ServerName,
		Hello:      fp,
	}
	tap := &tapConn{Conn: tc, info: info}
	if st.NegotiatedProtocol == "h2" {
		info.h2 = true
		h2s.ServeConn(tap, &http2.ServeConnOpts{Context: withConnInfo(ctx, info), BaseConfig: base})
		return
	}
	h1.Push(tap)
}

func acceptLoop(ctx context.Context, ln net.Listener, handle func(net.Conn)) {
	for {
		c, err := ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			return
		} else if err != nil {
			LOG.Warnf(ctx, "accept failed: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go handle(c)
	}
}

func main() {
	ctx := context.Background()
	setupConfigAndLogger(ctx)
	LOG.Debugf(ctx, "use config: %+v", CFG)

	var err error
	if RECORDER, err = newRecorder(CFG.Output); err != nil {
		panic(err)
	}
	tlsCfg := setupTLSConfig(ctx)

	ln, err := net.Listen("tcp", CFG.ListenAddr)
	if err != nil {
		LOG.Fatal(ctx, "listen", logger.Error(err))
	}
	h1 := newChanListener(ln.Addr())
	server := &http.Server{
		Handler:           http.HandlerFunc(dumpHandler),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			if tc, ok := c.(*tapConn); ok {
				return withConnInfo(ctx, tc.info)
			}
			return ctx
		},
	}
	h2s := &http2.Server{IdleTimeout: 60 * time.Second}
	go func() {
		if err := server.Serve(h1); err != nil && !errors.Is(err, http.ErrServerClosed) {
			LOG.Fatal(ctx, "http server", logger.Error(err))
		}
	}()

	LOG.Infof(ctx, "https service started: %s, records to %s", ln.Addr(), CFG.Output)
	go acceptLoop(ctx, ln, func(c net.Conn) { serveTLS(ctx, c, tlsCfg, h1, h2s, server) })

	var plain net.Listener
	if CFG.HTTPAddr != "" {
		if plain, err = net.Listen("tcp", CFG.HTTPAddr); err != nil {
			LOG.Fatal(ctx, "listen", logger.Error(err))
		}
		LOG.Infof(ctx, "http service started: %s", plain.Addr())
		go acceptLoop(ctx, plain, func(c net.Conn) {
			h1.Push(&tapConn{Conn: c, info: &connInfo{Remote: c.RemoteAddr().String()}})
		})
	}

	osutil.WaitForStop()

	ln.Close()
	if plain != nil {
		plain.Close()
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		LOG.Warn(ctx, "service stop", logger.Error(err))
	}
}
