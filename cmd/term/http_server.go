package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/creack/pty"
	"github.com/whoisnian/glb/httpd"
	"github.com/whoisnian/glb/logger"
	"github.com/whoisnian/glb/util/fsutil"
	fe "github.com/whoisnian/misc/cmd/term/fe/dist"
)

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
	mux.Handle("/static/*", http.MethodGet, func(s *httpd.Store) { serveFileFromFE(s, filepath.Join("static", s.RouteParamAny())) })
	mux.Handle("/favicon.ico", http.MethodGet, func(s *httpd.Store) { serveFileFromFE(s, "favicon.ico") })
	mux.Handle("/robots.txt", http.MethodGet, func(s *httpd.Store) { serveFileFromFE(s, "robots.txt") })
	mux.Handle("/web", http.MethodGet, func(s *httpd.Store) { serveFileFromFE(s, "static/index.html") })
	mux.Handle("/ws", http.MethodGet, webSocketHandlerWith(shellPath, workingDir))

	LOG.Infof(ctx, "http server listening on %s", options)
	server := &http.Server{Addr: options, Handler: mux}
	if err := server.ListenAndServe(); errors.Is(err, http.ErrServerClosed) {
		LOG.Warn(ctx, "server is shutting down")
	} else if err != nil {
		return err
	}
	return nil
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
			defer cmd.Process.Kill()
			for {
				_, data, err := conn.Read(context.Background())
				if err != nil {
					LOG.Error(store.R.Context(), "websocket.Read failed", logger.Error(err))
					conn.Close(websocket.StatusInternalError, "internal error")
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
				if err != nil {
					LOG.Error(store.R.Context(), "ptmx.Read failed", logger.Error(err))
					conn.Close(websocket.StatusInternalError, "internal error")
					return
				}
				if n > 0 {
					err = conn.Write(context.Background(), websocket.MessageBinary, buf[:n+1])
					if err != nil {
						LOG.Error(store.R.Context(), "websocket.Write failed", logger.Error(err))
						conn.Close(websocket.StatusInternalError, "internal error")
						return
					}
				}
			}
		})

		cmd.Wait()
		wg.Wait()
	}
}

// shell exit
// 2025-09-18 20:08:12 [E] ptmx.Read failed read /dev/ptmx: input/output error
// 2025-09-18 20:08:12 [E] websocket.Read failed failed to get reader: received close frame: status = StatusInternalError and reason = "internal error"

// client close tab
// 2025-09-18 20:08:45 [E] websocket.Read failed failed to get reader: received close frame: status = StatusGoingAway and reason = ""
// 2025-09-18 20:08:45 [E] ptmx.Read failed read /dev/ptmx: input/output error

// client refresh tab
// 2025-09-18 20:09:31 [E] websocket.Read failed failed to get reader: received close frame: status = StatusGoingAway and reason = ""
// 2025-09-18 20:09:31 [E] ptmx.Read failed read /dev/ptmx: input/output error

// client call ws.close()
// 2025-09-18 20:11:01 [E] websocket.Read failed failed to get reader: received close frame: status = StatusNoStatusRcvd and reason = ""
// 2025-09-18 20:11:01 [E] ptmx.Read failed read /dev/ptmx: input/output error
