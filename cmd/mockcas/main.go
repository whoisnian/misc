package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/whoisnian/glb/ansi"
	"github.com/whoisnian/glb/config"
	"github.com/whoisnian/glb/httpd"
	"github.com/whoisnian/glb/logger"
	"github.com/whoisnian/glb/util/netutil"
	"github.com/whoisnian/glb/util/osutil"
)

var CFG struct {
	Debug      bool   `flag:"d,false,Enable debug output"`
	ListenAddr string `flag:"l,0.0.0.0:9090,Server listen addr"`

	CasServerUrlPrefix  string `flag:"cas-server-url-prefix,,URL prefix of the CAS server, auto detected if empty"`
	CasClientServiceUrl string `flag:"cas-client-service-url,,Service URL of the CAS client application, auto detected if empty"`
	CasClientLogoutUrl  string `flag:"cas-client-logout-url,,Logout URL of the CAS client application, auto detected if empty"`
	CasAuthMethod       string `flag:"cas-auth-method,static,Authentication method of the CAS server, static or ldap"`

	LDAPServerUrl    string `flag:"|ldap-server-url|ldap://127.0.0.1:3890|URL of the LDAP server"`
	LDAPBindDN       string `flag:"|ldap-bind-dn|cn=admin,ou=people,dc=example,dc=com|DN to bind to the LDAP server"`
	LDAPBindPass     string `flag:"|ldap-bind-pass|password|Password for the LDAP bind DN"`
	LDAPBaseDN       string `flag:"|ldap-base-dn|ou=people,dc=example,dc=com|Base DN for LDAP search"`
	LDAPSearchFilter string `flag:"|ldap-search-filter|(uid=%s)|Filter for LDAP search"`
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

func main() {
	ctx := context.Background()
	setupConfigAndLogger(ctx)
	LOG.Debugf(ctx, "use config: %+v", CFG)

	setupUserProvider(ctx)
	setupTicketProvider(ctx)

	mux := httpd.NewMux()
	mux.HandleMiddleware(LOG.NewMiddleware())
	mux.Handle("/cas/login", http.MethodGet, loginPageHandler)
	mux.Handle("/cas/login", http.MethodPost, loginCheckHandler)
	mux.Handle("/cas/logout", http.MethodGet, logoutHandler)
	mux.Handle("/cas/validate", http.MethodGet, validateHandler)
	mux.Handle("/cas/p3/serviceValidate", http.MethodGet, serviceValidateHandler)
	mux.Handle("/cas/p3/proxyValidate", http.MethodGet, proxyValidateHandler) // not implemented

	mux.Handle("/app/login", http.MethodGet, appLoginHandler)
	mux.Handle("/app/validate", http.MethodGet, appValidateHandler)
	mux.Handle("/app/logout", http.MethodGet, appLogoutHandler)
	mux.Handle("/app/logout", http.MethodPost, appSingleLogoutHandler)

	predictAddr := CFG.ListenAddr
	if host, port, err := net.SplitHostPort(CFG.ListenAddr); err == nil && (host == "" || host == "0.0.0.0") {
		if ip, err := netutil.GetOutBoundIP(); err == nil {
			predictAddr = net.JoinHostPort(ip.String(), port)
		}
	}
	if CFG.CasServerUrlPrefix == "" {
		CFG.CasServerUrlPrefix = "http://" + predictAddr + "/cas"
	}
	if CFG.CasClientServiceUrl == "" {
		CFG.CasClientServiceUrl = "http://" + predictAddr + "/app/validate"
	}
	if CFG.CasClientLogoutUrl == "" {
		CFG.CasClientLogoutUrl = "http://" + predictAddr + "/app/logout"
	}
	LOG.Infof(ctx, "using cas server url prefix:  %s", CFG.CasServerUrlPrefix)
	LOG.Infof(ctx, "using cas client service url: %s", CFG.CasClientServiceUrl)
	LOG.Infof(ctx, "using cas client logout url:  %s", CFG.CasClientLogoutUrl)

	server := &http.Server{Addr: CFG.ListenAddr, Handler: mux}
	go func() {
		LOG.Infof(ctx, "service started: http://%s", CFG.ListenAddr)
		if err := server.ListenAndServe(); errors.Is(err, http.ErrServerClosed) {
			LOG.Warn(ctx, "service shutting down")
		} else if err != nil {
			LOG.Fatal(ctx, "service start", logger.Error(err))
		}
	}()

	osutil.WaitForStop()

	shutdownCtx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		LOG.Warn(ctx, "service stop", logger.Error(err))
	}
}
