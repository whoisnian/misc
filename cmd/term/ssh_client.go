package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

func runSSHClient(ctx context.Context, options string) error {
	// parse options
	parsed, err := url.Parse("ssh://" + options)
	if err != nil {
		return fmt.Errorf("url.Parse error: %w", err)
	}

	user := parsed.User.Username()
	if user == "" {
		return errors.New("no username specified")
	}
	pass, _ := parsed.User.Password()
	host := parsed.Hostname()
	if host == "" {
		return errors.New("no host specified")
	}
	port := parsed.Port()
	if port == "" {
		port = "22"
	}
	addr := net.JoinHostPort(host, port)

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

	// connect to ssh server
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         time.Second * 10,
	}
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("ssh.Dial error: %w", err)
	}
	defer client.Close()

	// keepalive 10s
	go func() {
		ticker := time.NewTicker(time.Second * 10)
		defer ticker.Stop()
		for range ticker.C {
			_, _, err := client.SendRequest("keepalive@golang.org", true, nil)
			if err != nil {
				LOG.Warnf(ctx, "keepalive error: %v", err)
				return
			}
		}
	}()

	// bind session stdin/stdout/stderr
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("client.NewSession error: %w", err)
	}
	defer session.Close()
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	session.Stdin = os.Stdin

	// request pty and shell
	if err = session.RequestPty("xterm-256color", h, w, ssh.TerminalModes{}); err != nil {
		return fmt.Errorf("session.RequestPty error: %w", err)
	}
	if err = session.Shell(); err != nil {
		return fmt.Errorf("session.Shell error: %w", err)
	}

	// monitor terminal resize
	sigCh := make(chan os.Signal, 1)
	defer close(sigCh)
	signal.Notify(sigCh, syscall.SIGWINCH)
	defer signal.Stop(sigCh)
	go func() {
		for range sigCh {
			w, h, err := term.GetSize(fd)
			if err != nil {
				LOG.Errorf(ctx, "term.GetSize error: %v", err)
				continue
			}
			if err = session.WindowChange(h, w); err != nil {
				LOG.Errorf(ctx, "session.WindowChange error: %v", err)
			}
		}
	}()

	return session.Wait()
}
