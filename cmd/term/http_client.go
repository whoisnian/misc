package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/coder/websocket"
	"github.com/whoisnian/glb/logger"
	"golang.org/x/term"
)

func runHTTPClient(ctx context.Context, options string) error {
	// parse options
	parsed, err := url.Parse("http://" + options)
	if err != nil {
		return fmt.Errorf("url.Parse error: %w", err)
	}
	auth := parsed.User.String()
	addr := parsed.Host

	// prepare terminal
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return errors.New("stdin is not a terminal")
	}
	w, h, err := term.GetSize(fd)
	if err != nil {
		return fmt.Errorf("term.GetSize error: %w", err)
	}
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("term.MakeRaw error: %w", err)
	}
	defer term.Restore(fd, oldState)

	// connect to httpd server
	var opts *websocket.DialOptions
	if auth != "" {
		opts = &websocket.DialOptions{
			HTTPHeader: map[string][]string{
				"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte(auth))},
			},
		}
	}
	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://%s/ws?w=%d&h=%d", addr, w, h), opts)
	if err != nil {
		return fmt.Errorf("websocket.Dial error: %w", err)
	}
	defer conn.CloseNow()

	// monitor terminal resize
	sigCh := make(chan os.Signal, 1)
	defer close(sigCh)
	signal.Notify(sigCh, syscall.SIGWINCH)
	defer signal.Stop(sigCh)
	go func() {
		data := make([]byte, 5)
		data[0] = MSG_TYPE_RESIZE
		for range sigCh {
			w, h, err := term.GetSize(fd)
			if err != nil {
				LOG.Errorf(ctx, "term.GetSize error: %v", err)
				continue
			}
			data[1] = byte(w)
			data[2] = byte(w >> 8)
			data[3] = byte(h)
			data[4] = byte(h >> 8)
			err = conn.Write(ctx, websocket.MessageBinary, data)
			if err != nil {
				LOG.Error(ctx, "websocket.Write failed", logger.Error(err))
				conn.Close(websocket.StatusInternalError, "internal error")
				return
			}
		}
	}()

	go func() {
		buf := make([]byte, 4096)
		buf[0] = MSG_TYPE_DATA
		for {
			n, err := os.Stdin.Read(buf[1:])
			if err != nil {
				LOG.Error(ctx, "stdin.Read failed", logger.Error(err))
				conn.Close(websocket.StatusInternalError, "internal error")
				return
			}
			if n > 0 {
				err = conn.Write(ctx, websocket.MessageBinary, buf[:n+1])
				if err != nil {
					LOG.Error(ctx, "websocket.Write failed", logger.Error(err))
					conn.Close(websocket.StatusInternalError, "internal error")
					return
				}
			}
		}
	}()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			conn.Close(websocket.StatusInternalError, "internal error")
			return err
		} else if len(data) == 0 {
			continue
		}
		switch data[0] {
		case MSG_TYPE_RESIZE:
			cols := uint16(data[1]) | uint16(data[2])<<8
			rows := uint16(data[3]) | uint16(data[4])<<8
			return fmt.Errorf("server resize terminal to %dx%d", cols, rows)
		case MSG_TYPE_DATA:
			if _, err := os.Stdout.Write(data[1:]); err != nil {
				conn.Close(websocket.StatusInternalError, "internal error")
				return err
			}
		default:
			conn.Close(websocket.StatusUnsupportedData, "unsupported message")
			return err
		}
	}
}
