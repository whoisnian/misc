package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
	"github.com/whoisnian/glb/util/fsutil"
	"golang.org/x/crypto/ssh"
)

func runSSHServer(ctx context.Context, options string) error {
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

	// generate host key
	data, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return fmt.Errorf("os.ReadFile /etc/machine-id error: %w", err)
	}
	seed := sha256.Sum256(data)
	hostKey, err := ssh.NewSignerFromKey(ed25519.NewKeyFromSeed(seed[:]))
	if err != nil {
		return fmt.Errorf("ssh.NewSignerFromKey error: %w", err)
	}

	// start ssh server
	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
			if c.User() == user && string(p) == pass {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected for %q", c.User())
		},
	}
	config.AddHostKey(hostKey)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	defer listener.Close()
	LOG.Infof(ctx, "ssh server listening on %s", addr)

	// accept incoming connections
	connID := 0
	for {
		connID++
		tcpconn, err := listener.Accept()
		if err != nil {
			return fmt.Errorf("failed to accept incoming connection: %w", err)
		}
		go func() {
			srvconn, chans, reqs, err := ssh.NewServerConn(tcpconn, config)
			if err != nil {
				LOG.Errorf(ctx, "%d.0.0 ssh.NewServerConn error: %v", connID, err)
				return
			}
			defer srvconn.Close()
			go func() {
				LOG.Debugf(ctx, "%d.0.0 ssh.DiscardRequests start", connID)
				ssh.DiscardRequests(reqs)
				LOG.Debugf(ctx, "%d.0.0 ssh.DiscardRequests end", connID)
			}()
			channelID := 0
			for newChannel := range chans {
				channelID++
				go handleNewChannel(ctx, newChannel, shellPath, workingDir, connID, channelID)
			}
		}()
	}
}

type ptyRequestMsg struct {
	Term     string
	Columns  uint32
	Rows     uint32
	Width    uint32
	Height   uint32
	Modelist string
}

type ptyWindowChangeMsg struct {
	Columns uint32
	Rows    uint32
	Width   uint32
	Height  uint32
}

type exitStatusMsg struct {
	ExitStatus uint32
}

func handleNewChannel(ctx context.Context, newChannel ssh.NewChannel, shellPath string, workingDir string, connID int, channelID int) {
	LOG.Debugf(ctx, "%d.%d.0 handleNewChannel %s start", connID, channelID, newChannel.ChannelType())
	defer LOG.Debugf(ctx, "%d.%d.0 handleNewChannel %s end", connID, channelID, newChannel.ChannelType())

	if newChannel.ChannelType() != "session" {
		newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
		return
	}
	channel, requests, err := newChannel.Accept()
	if err != nil {
		LOG.Errorf(ctx, "%d.%d.0 ssh.NewChannel.Accept error: %v", connID, channelID, err)
		return
	}
	defer channel.Close()

	var ptmx, tty *os.File
	requestID := 0
	for req := range requests {
		requestID++
		switch req.Type {
		case "pty-req":
			var ptyreq ptyRequestMsg
			if err := ssh.Unmarshal(req.Payload, &ptyreq); err != nil {
				LOG.Errorf(ctx, "%d.%d.%d ssh.Unmarshal pty-req error: %v", connID, channelID, requestID, err)
				req.Reply(false, nil)
				return
			}
			LOG.Debugf(ctx, "%d.%d.%d request pty-req: term=%q, columns=%d, rows=%d", connID, channelID, requestID, ptyreq.Term, ptyreq.Columns, ptyreq.Rows)
			if ptmx != nil {
				LOG.Errorf(ctx, "%d.%d.%d pty already allocated", connID, channelID, requestID)
				req.Reply(false, nil)
				return
			}
			ptmx, tty, err = pty.Open()
			if err != nil {
				LOG.Errorf(ctx, "%d.%d.%d pty.Open error: %v", connID, channelID, requestID, err)
				req.Reply(false, nil)
				return
			}
			defer func() {
				LOG.Debugf(ctx, "%d.%d.%d pty.Close start", connID, channelID, requestID)
				ptmx.Close()
				tty.Close()
				LOG.Debugf(ctx, "%d.%d.%d pty.Close end", connID, channelID, requestID)
			}()
			pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(ptyreq.Rows), Cols: uint16(ptyreq.Columns)})
			req.Reply(true, nil)
		case "shell":
			LOG.Debugf(ctx, "%d.%d.%d request shell: %q", connID, channelID, requestID, shellPath)
			if ptmx == nil {
				LOG.Errorf(ctx, "%d.%d.%d no pty allocated", connID, channelID, requestID)
				req.Reply(false, nil)
				return
			}
			cmd := exec.CommandContext(ctx, shellPath)
			cmd.Dir = workingDir
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
			cmd.Stdin = tty
			cmd.Stdout = tty
			cmd.Stderr = tty
			if err := cmd.Start(); err != nil {
				LOG.Errorf(ctx, "%d.%d.%d exec.Command.Start error: %v", connID, channelID, requestID, err)
				req.Reply(false, nil)
				return
			}
			go func() {
				LOG.Debugf(ctx, "%d.%d.%d io.Copy input start", connID, channelID, requestID)
				io.Copy(ptmx, channel)
				cmd.Process.Signal(syscall.SIGHUP)
				LOG.Debugf(ctx, "%d.%d.%d io.Copy input end", connID, channelID, requestID)
			}()
			go func() {
				LOG.Debugf(ctx, "%d.%d.%d io.Copy output start", connID, channelID, requestID)
				io.Copy(channel, ptmx)
				LOG.Debugf(ctx, "%d.%d.%d io.Copy output end", connID, channelID, requestID)
			}()
			go func() {
				LOG.Debugf(ctx, "%d.%d.%d cmd.Wait start", connID, channelID, requestID)
				cmd.Wait()
				channel.SendRequest("exit-status", false, ssh.Marshal(exitStatusMsg{0}))
				channel.Close()
				LOG.Debugf(ctx, "%d.%d.%d cmd.Wait end", connID, channelID, requestID)
			}()
			req.Reply(true, nil)
		case "window-change":
			var winch ptyWindowChangeMsg
			if err := ssh.Unmarshal(req.Payload, &winch); err != nil {
				LOG.Errorf(ctx, "%d.%d.%d ssh.Unmarshal window-change error: %v", connID, channelID, requestID, err)
				return
			}
			LOG.Debugf(ctx, "%d.%d.%d request window-change: columns=%d, rows=%d", connID, channelID, requestID, winch.Columns, winch.Rows)
			if ptmx != nil {
				pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(winch.Rows), Cols: uint16(winch.Columns)})
			}
		default:
			LOG.Warnf(ctx, "%d.%d.%d unknown request type: %q", connID, channelID, requestID, req.Type)
			req.Reply(false, nil)
		}
	}
}
