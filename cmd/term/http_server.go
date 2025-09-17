package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/coder/websocket"
	"github.com/creack/pty"
	"github.com/whoisnian/glb/httpd"
	"github.com/whoisnian/glb/logger"
	"github.com/whoisnian/glb/util/fsutil"
)

//go:embed web/*
var webFS embed.FS

func runHTTPServer(ctx context.Context, options string) error {
	shell := CFG.Shell
	if shell == "" {
		shell = "sh"
	}
	shellPath, err := exec.LookPath(shell)
	if err != nil {
		return fmt.Errorf("exec.LookPath error: %w", err)
	}
	workingDir, err := fsutil.ExpandHomeDir(CFG.WorkingDir)
	if err != nil {
		return fmt.Errorf("fsutil.ExpandHomeDir error: %w", err)
	}

	mux := httpd.NewMux()
	mux.HandleMiddleware(LOG.NewMiddleware())
	mux.Handle("/web", http.MethodGet, webHandler)
	mux.Handle("/static/*", http.MethodGet, staticHandler)
	mux.Handle("/ws", http.MethodGet, createWebSocketHandler(shellPath, workingDir))

	LOG.Infof(ctx, "http server listening on %s", options)
	server := &http.Server{Addr: options, Handler: mux}
	if err := server.ListenAndServe(); errors.Is(err, http.ErrServerClosed) {
		LOG.Warn(ctx, "server is shutting down")
	} else if err != nil {
		return err
	}
	return nil
}

func serveWebFile(store *httpd.Store, path string) {
	file, err := webFS.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			store.W.WriteHeader(http.StatusNotFound)
			return
		}
		LOG.Error(store.R.Context(), "serveWebFile failed", logger.Error(err))
		store.W.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		store.W.WriteHeader(http.StatusForbidden)
		return
	}

	ctype := mime.TypeByExtension(filepath.Ext(path))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	// } else if strings.Contains(ctype, "text/css") || strings.Contains(ctype, "application/javascript") {
	// 	// nginx expires max
	// 	// https://nginx.org/en/docs/http/ngx_http_headers_module.html#expires
	// 	store.W.Header().Set("cache-control", "max-age:315360000, public")
	// 	store.W.Header().Set("expires", "Thu, 31 Dec 2037 23:55:55 GMT")
	// }
	store.W.Header().Set("content-type", ctype)

	if store.W.Header().Get("content-encoding") == "" {
		store.W.Header().Set("content-length", strconv.FormatInt(info.Size(), 10))
	}
	if _, err := io.CopyN(store.W, file, info.Size()); err != nil {
		LOG.Error(store.R.Context(), "io.CopyN failed", logger.Error(err))
		store.W.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func webHandler(store *httpd.Store) {
	serveWebFile(store, "web/index.html")
}

func staticHandler(store *httpd.Store) {
	serveWebFile(store, filepath.Join("web/static", store.RouteParamAny()))
}

func createWebSocketHandler(shellPath string, workingDir string) func(store *httpd.Store) {
	return func(store *httpd.Store) {
		conn, err := websocket.Accept(store.W.Origin, store.R, nil)
		if err != nil {
			LOG.Error(store.R.Context(), "websocket.Accept failed", logger.Error(err))
			store.W.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer conn.CloseNow()

		w, _ := strconv.Atoi(store.R.URL.Query().Get("w"))
		if w == 0 {
			w = 80
		}
		h, _ := strconv.Atoi(store.R.URL.Query().Get("h"))
		if h == 0 {
			h = 24
		}

		cmd := exec.CommandContext(store.R.Context(), shellPath)
		cmd.Dir = workingDir
		ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)})
		if err != nil {
			LOG.Error(store.R.Context(), "pty.StartWithSize failed", logger.Error(err))
			conn.Close(websocket.StatusInternalError, "internal error")
			return
		}
		defer ptmx.Close()

		go func() {
			for {
				_, data, err := conn.Read(context.Background())
				if err != nil {
					LOG.Error(store.R.Context(), "websocket.Read failed", logger.Error(err))
					return
				}
				if _, err := ptmx.Write(data); err != nil {
					LOG.Error(store.R.Context(), "ptmx.Write failed", logger.Error(err))
					return
				}
			}
		}()

		for {
			buf := make([]byte, 4096)
			n, err := ptmx.Read(buf)
			if err != nil {
				if errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF) {
					break
				}
				LOG.Error(store.R.Context(), "ptmx.Read failed", logger.Error(err))
				conn.Close(websocket.StatusInternalError, "internal error")
				break
			}
			if n > 0 {
				err = conn.Write(context.Background(), websocket.MessageText, buf[:n])
				if err != nil {
					LOG.Error(store.R.Context(), "websocket.Write failed", logger.Error(err))
					break
				}
			}
		}

		if process := cmd.Process; process != nil {
			process.Signal(syscall.SIGTERM)
		}
		cmd.Wait()
		conn.Close(websocket.StatusNormalClosure, "")
	}
}
