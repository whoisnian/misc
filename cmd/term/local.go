package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"github.com/whoisnian/glb/util/fsutil"
	"golang.org/x/term"
)

func runLocal(ctx context.Context) error {
	// parse options
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

	// prepare terminal
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return errors.New("stdin is not a terminal")
	}
	w, h, err := term.GetSize(fd)
	if err != nil {
		return fmt.Errorf("term.GetSize error: %w", err)
	}

	// bind shell to pty
	cmd := exec.CommandContext(ctx, shellPath)
	cmd.Dir = workingDir
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
	if err != nil {
		return fmt.Errorf("pty.StartWithSize error: %w", err)
	}
	defer ptmx.Close()

	// monitor terminal resize
	sigCh := make(chan os.Signal, 1)
	defer close(sigCh)
	signal.Notify(sigCh, syscall.SIGWINCH)
	defer signal.Stop(sigCh)
	go func() {
		for range sigCh {
			if err := pty.InheritSize(os.Stdin, ptmx); err != nil {
				LOG.Errorf(ctx, "pty.InheritSize error: %v", err)
			}
		}
	}()

	// set terminal raw mode
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("term.MakeRaw error: %w", err)
	}
	defer term.Restore(fd, oldState)

	// pipe input and output
	go io.Copy(ptmx, os.Stdin)
	go io.Copy(os.Stdout, ptmx)
	return cmd.Wait()
}
