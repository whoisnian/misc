package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/whoisnian/glb/ansi"
	"github.com/whoisnian/glb/config"
	"github.com/whoisnian/glb/httpd"
	"github.com/whoisnian/glb/logger"
	"github.com/whoisnian/glb/util/fsutil"
	"github.com/whoisnian/glb/util/netutil"
	"github.com/whoisnian/glb/util/osutil"
)

var CFG struct {
	Debug      bool   `flag:"d,false,Enable debug output"`
	ListenAddr string `flag:"l,0.0.0.0:54321,Server listen addr"`
	BaseDir    string `flag:"b,base,Base directory to serve files from"`
	Fallbacks  string `flag:"f,,Fallback directories to serve files if not found (comma-separated)"`
	Live       bool   `flag:"live,false,Enable fallback files live rebuild (at most once per 60s)"`
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

var (
	FALLBACK    *fallbackFiles
	rebuildMux  sync.Mutex
	lastRebuild time.Time
)

func setupFallbackFiles(ctx context.Context) {
	if CFG.Fallbacks == "" {
		return
	}
	FALLBACK = NewFallbackFiles(strings.Split(CFG.Fallbacks, ","))
	if err := FALLBACK.Rebuild(ctx); err != nil {
		panic(err)
	}
	LOG.Infof(ctx, "build fallback files with %d entries, live mode: %t", FALLBACK.Size(), CFG.Live)
	lastRebuild = time.Now()
}

func main() {
	ctx := context.Background()
	setupConfigAndLogger(ctx)
	LOG.Debugf(ctx, "use config: %+v", CFG)

	setupFallbackFiles(ctx)

	mux := httpd.NewMux()
	mux.HandleMiddleware(LOG.NewMiddleware())
	mux.Handle("/*", http.MethodGet, indexHandler)

	predictAddr := CFG.ListenAddr
	if host, port, err := net.SplitHostPort(CFG.ListenAddr); err == nil && (host == "" || host == "0.0.0.0") {
		if ip, err := netutil.GetOutBoundIP(); err == nil {
			predictAddr = net.JoinHostPort(ip.String(), port)
		}
	}

	server := &http.Server{Addr: CFG.ListenAddr, Handler: mux}
	go func() {
		LOG.Infof(ctx, "service started: http://%s", predictAddr)
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

func indexHandler(store *httpd.Store) {
	fpath := fsutil.ResolveUrlPath(CFG.BaseDir, store.RouteParamAny())
	_, err := os.Stat(fpath)
	if err != nil {
		if os.IsNotExist(err) {
			if FALLBACK != nil {
				if key, val, ok := FALLBACK.LongestSuffix(fpath); ok {
					LOG.Debugf(store.R.Context(), "fallback %s matches %s", val, key)
					http.ServeFile(store.W, store.R, val)
					return
				}

				if CFG.Live {
					rebuildMux.Lock()
					if time.Since(lastRebuild) > 60*time.Second {
						lastRebuild = time.Now()
						LOG.Warnf(store.R.Context(), "file not found, trigger live rebuild")
						if err = FALLBACK.Rebuild(store.R.Context()); err != nil {
							LOG.Errorf(store.R.Context(), "live rebuild failed: %v", err)
						}
					}
					rebuildMux.Unlock()
					if key, val, ok := FALLBACK.LongestSuffix(fpath); ok {
						LOG.Debugf(store.R.Context(), "fallback %s matches %s", val, key)
						http.ServeFile(store.W, store.R, val)
						return
					}
				}
			}
			store.Error404("file not found")
			return
		} else {
			LOG.Error(store.R.Context(), "os.Stat failed", logger.Error(err))
			store.Error500("internal error")
			return
		}
	}
	http.ServeFile(store.W, store.R, fpath)
}
