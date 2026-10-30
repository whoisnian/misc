//go:build linux

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// writeTimeout bounds a write to a stalled link; it is longer than the 1 s SSR latency in the SDP record.
const writeTimeout = 2 * time.Second

type bdaddr [6]byte

func parseBDAddr(s string) (bdaddr, error) {
	var a bdaddr
	parts := strings.Split(strings.ToUpper(strings.TrimSpace(s)), ":")
	if len(parts) != 6 {
		return a, fmt.Errorf("invalid bluetooth address %q", s)
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 16, 8)
		if err != nil || len(p) != 2 {
			return a, fmt.Errorf("invalid bluetooth address %q", s)
		}
		a[i] = byte(n)
	}
	return a, nil
}

func (a bdaddr) String() string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", a[0], a[1], a[2], a[3], a[4], a[5])
}

// unix.SockaddrL2.sockaddr() reverses Addr into the kernel's little-endian bdaddr_t, but the
// Sockaddr returned by Accept copies the raw bytes back without reversing, so undo it here.
func bdaddrFromRaw(raw [6]uint8) bdaddr {
	var a bdaddr
	for i := range raw {
		a[i] = raw[5-i]
	}
	return a
}

// l2capConn is a connected HID channel. The fd is non-blocking and wrapped in os.File so that
// Read blocks on the runtime poller and Close interrupts it.
type l2capConn struct {
	f    *os.File
	peer bdaddr
}

func newL2CAPSocket() (int, error) {
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, unix.BTPROTO_L2CAP)
	if err != nil {
		return -1, fmt.Errorf("socket: %w", err)
	}
	// struct bt_security { uint8_t level; uint8_t key_size; }
	if err := unix.SetsockoptString(fd, unix.SOL_BLUETOOTH, btSecurity, string([]byte{btSecurityMedium, 0})); err != nil {
		unix.Close(fd)
		return -1, fmt.Errorf("BT_SECURITY: %w", err)
	}
	return fd, nil
}

func wrapL2CAP(fd int, peer bdaddr) (*l2capConn, error) {
	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return &l2capConn{f: os.NewFile(uintptr(fd), "l2cap:"+peer.String()), peer: peer}, nil
}

func (c *l2capConn) Read(buf []byte) (int, error) { return c.f.Read(buf) }
func (c *l2capConn) Close() error                 { return c.f.Close() }

// Write runs with Relay.mu held, so a stalled link has to fail instead of blocking input handling.
func (c *l2capConn) Write(pkt []byte) (int, error) {
	if err := c.f.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return 0, err
	}
	return c.f.Write(pkt)
}

// listenL2CAP binds a PSM on the local adapter. PSMs below 0x1001 need CAP_NET_BIND_SERVICE.
func listenL2CAP(local bdaddr, psm uint16) (int, error) {
	fd, err := newL2CAPSocket()
	if err != nil {
		return -1, err
	}
	if err := unix.Bind(fd, &unix.SockaddrL2{PSM: psm, Addr: local}); err != nil {
		unix.Close(fd)
		return -1, fmt.Errorf("bind psm %d: %w", psm, err)
	}
	if err := unix.Listen(fd, 1); err != nil {
		unix.Close(fd)
		return -1, fmt.Errorf("listen psm %d: %w", psm, err)
	}
	return fd, nil
}

func acceptL2CAP(lfd int) (*l2capConn, error) {
	nfd, sa, err := unix.Accept4(lfd, unix.SOCK_CLOEXEC)
	if err != nil {
		return nil, err
	}
	l2, ok := sa.(*unix.SockaddrL2)
	if !ok {
		unix.Close(nfd)
		return nil, fmt.Errorf("unexpected sockaddr %T", sa)
	}
	return wrapL2CAP(nfd, bdaddrFromRaw(l2.Addr))
}

// dialL2CAP connects to a PSM on the remote device; it blocks for the page timeout on failure.
func dialL2CAP(local, remote bdaddr, psm uint16) (*l2capConn, error) {
	fd, err := newL2CAPSocket()
	if err != nil {
		return nil, err
	}
	if err := unix.Bind(fd, &unix.SockaddrL2{PSM: 0, Addr: local}); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("bind: %w", err)
	}
	if err := unix.Connect(fd, &unix.SockaddrL2{PSM: psm, Addr: remote}); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return wrapL2CAP(fd, remote)
}
