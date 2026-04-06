package main

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	iradix "github.com/hashicorp/go-immutable-radix/v2"
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

var TREE *iradix.Tree[string]

func setupFallbackTree(ctx context.Context) {
	if CFG.Fallbacks == "" {
		return
	}

	TREE = iradix.New[string]()
	for _, dir := range slices.Backward(strings.Split(CFG.Fallbacks, ",")) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		LOG.Debugf(ctx, "walk fallback directory: %s", dir)
		if err := walkDirAndBuildTree(ctx, dir); err != nil {
			panic(err)
		}
	}
}

func walkDirAndBuildTree(ctx context.Context, root string) error {
	walkDirFunc := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		buf := reverseBytesFromString(relPath)
		LOG.Debugf(ctx, "discover %s as %s", path, buf)
		TREE, _, _ = TREE.Insert(buf, path)
		return nil
	}
	return filepath.WalkDir(root, walkDirFunc)
}

func reverseBytesFromString(s string) []byte {
	buf := []byte(s)
	slices.Reverse(buf)
	return buf
}

func main() {
	ctx := context.Background()
	setupConfigAndLogger(ctx)
	LOG.Debugf(ctx, "use config: %+v", CFG)

	setupFallbackTree(ctx)
	if TREE != nil {
		LOG.Warnf(ctx, "build fallback tree with %d entries, restart needed if fallback files change", TREE.Len())
	}

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
			if TREE != nil {
				buf := reverseBytesFromString(fpath)
				if key, val, ok := TREE.Root().LongestPrefix(buf); ok {
					LOG.Debugf(store.R.Context(), "fallback %s matches %s", val, key)
					http.ServeFile(store.W, store.R, val)
					return
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
