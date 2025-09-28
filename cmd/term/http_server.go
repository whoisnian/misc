package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/coder/websocket"
	"github.com/creack/pty"
	"github.com/whoisnian/glb/httpd"
	"github.com/whoisnian/glb/logger"
	"github.com/whoisnian/glb/util/fsutil"
	fe "github.com/whoisnian/misc/cmd/term/fe/dist"
)

func runHTTPServer(ctx context.Context, options string) error {
	// parse options
	parsed, err := url.Parse("http://" + options)
	if err != nil {
		return fmt.Errorf("url.Parse error: %w", err)
	}
	auth := parsed.User.String()
	addr := parsed.Host
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
	mux.Handle("/static/*", http.MethodGet, func(s *httpd.Store) { serveFileFromFE(s, filepath.Join("static", s.RouteParamAny())) })
	mux.Handle("/favicon.ico", http.MethodGet, func(s *httpd.Store) { serveFileFromFE(s, "favicon.ico") })
	mux.Handle("/robots.txt", http.MethodGet, func(s *httpd.Store) { serveFileFromFE(s, "robots.txt") })
	mux.Handle("/web", http.MethodGet, authRequire(func(s *httpd.Store) { serveFileFromFE(s, "static/index.html") }, auth))
	mux.Handle("/ws", http.MethodGet, authRequire(webSocketHandlerWith(shellPath, workingDir), auth))

	LOG.Infof(ctx, "http server listening on %s", addr)
	server := &http.Server{Addr: addr, Handler: mux}
	if err := server.ListenAndServe(); errors.Is(err, http.ErrServerClosed) {
		LOG.Warn(ctx, "server is shutting down")
	} else if err != nil {
		return err
	}
	return nil
}

func authRequire(handler httpd.HandlerFunc, userinfo string) httpd.HandlerFunc {
	if userinfo == "" {
		return handler
	}
	b64str := base64.StdEncoding.EncodeToString([]byte(userinfo))
	return func(store *httpd.Store) {
		if store.R.Header.Get("Authorization") != "Basic "+b64str {
			store.W.Header().Add("WWW-Authenticate", `Basic realm="terminal server"`)
			store.W.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler(store)
	}
}

func serveFileFromFE(store *httpd.Store, path string) {
	file, err := fe.FS.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			store.W.WriteHeader(http.StatusNotFound)
			return
		}
		LOG.Error(store.R.Context(), "serveFileFromFE failed", logger.Error(err))
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
	} else if strings.Contains(ctype, "text/css") || strings.Contains(ctype, "application/javascript") {
		// nginx expires max
		// https://nginx.org/en/docs/http/ngx_http_headers_module.html#expires
		store.W.Header().Set("cache-control", "max-age:315360000, public")
		store.W.Header().Set("expires", "Thu, 31 Dec 2037 23:55:55 GMT")
	}
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

const MSG_TYPE_DATA = '0'
const MSG_TYPE_RESIZE = '1'

func webSocketHandlerWith(shellPath string, workingDir string) func(store *httpd.Store) {
	return func(store *httpd.Store) {
		conn, err := websocket.Accept(store.W, store.R, nil)
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

		wg := new(sync.WaitGroup)
		wg.Go(func() {
			defer cmd.Process.Signal(syscall.SIGHUP)
			for {
				_, data, err := conn.Read(store.R.Context())
				if err != nil && websocket.CloseStatus(err) == websocket.StatusNormalClosure {
					// server or client normally closed the connection
					return
				} else if err != nil && websocket.CloseStatus(err) == websocket.StatusInternalError {
					// server already closed the connection
					return
				} else if err != nil && websocket.CloseStatus(err) == websocket.StatusGoingAway {
					LOG.Warn(store.R.Context(), "websocket client is going away")
					return
				} else if err != nil {
					LOG.Error(store.R.Context(), "websocket.Read failed", logger.Error(err))
					return
				} else if len(data) == 0 {
					continue
				}
				switch data[0] {
				case MSG_TYPE_RESIZE:
					cols := uint16(data[1]) | uint16(data[2])<<8
					rows := uint16(data[3]) | uint16(data[4])<<8
					if err := pty.Setsize(ptmx, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
						LOG.Error(store.R.Context(), "pty.Setsize failed", logger.Error(err))
					}
				case MSG_TYPE_DATA:
					if _, err := ptmx.Write(data[1:]); err != nil {
						LOG.Error(store.R.Context(), "ptmx.Write failed", logger.Error(err))
						conn.Close(websocket.StatusInternalError, "internal error")
						return
					}
				default:
					LOG.Errorf(store.R.Context(), "unknown message type %d(%s)", data[0], string(data[0]))
					conn.Close(websocket.StatusUnsupportedData, "unsupported message")
					return
				}
			}
		})

		wg.Go(func() {
			buf := make([]byte, 4096)
			buf[0] = MSG_TYPE_DATA
			for {
				n, err := ptmx.Read(buf[1:])
				if err != nil && errors.Is(err, syscall.EIO) {
					conn.Close(websocket.StatusNormalClosure, "shell exited")
					return
				} else if err != nil {
					LOG.Error(store.R.Context(), "ptmx.Read failed", logger.Error(err))
					conn.Close(websocket.StatusInternalError, "ptmx read error")
					return
				}
				if n > 0 {
					err = conn.Write(store.R.Context(), websocket.MessageBinary, buf[:n+1])
					if err != nil && errors.Is(err, net.ErrClosed) {
						LOG.Warn(store.R.Context(), "websocket is already closed")
						return
					} else if err != nil {
						LOG.Error(store.R.Context(), "websocket.Write failed", logger.Error(err))
						conn.Close(websocket.StatusInternalError, "websocket write error")
						return
					}
				}
			}
		})

		cmd.Wait()
		wg.Wait()
	}
}
