//go:build linux

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	evSyn      = 0
	evKey      = 1
	evRel      = 2
	synReport  = 0
	synDropped = 3

	relX      = 0
	relY      = 1
	relHWheel = 6
	relWheel  = 8

	btnLeft  = 0x110
	btnExtra = 0x114

	eviocgrab = 0x40044590 // _IOW('E', 0x90, int)
	keyCnt    = 0x300      // KEY_CNT
)

// eviocgname returns EVIOCGNAME(n): _IOC(_IOC_READ, 'E', 0x06, n)
func eviocgname(n int) uint { return 0x80004506 | uint(n)<<16 }

// eviocgkey returns EVIOCGKEY(n): _IOC(_IOC_READ, 'E', 0x18, n)
func eviocgkey(n int) uint { return 0x80004518 | uint(n)<<16 }

// keyBits is the EVIOCGKEY bitmap of keys and buttons held down, indexed by evdev code.
type keyBits [keyCnt / 8]byte

func (k *keyBits) held(code uint16) bool { return k[code/8]>>(code%8)&1 != 0 }

// struct input_event: struct timeval + __u16 type + __u16 code + __s32 value
var eventSize = int(unsafe.Sizeof(unix.Timeval{})) + 8

type inputEvent struct {
	Type  uint16
	Code  uint16
	Value int32
}

type InputDev struct {
	path    string
	real    string
	name    string
	forward bool // false: only watched for the toggle key, never grabbed or forwarded
	f       *os.File
	grabbed bool
	dropped bool // SYN_DROPPED seen, skip events up to the next SYN_REPORT
}

func openInputDev(path string, forward bool) (*InputDev, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	// O_NONBLOCK lets os.NewFile register the fd with the runtime poller, so Close() unblocks Read()
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	d := &InputDev{path: path, real: real, forward: forward, f: os.NewFile(uintptr(fd), path)}
	buf := make([]byte, 256)
	if err := d.ioctl(eviocgname(len(buf)), unsafe.Pointer(&buf[0])); err != nil {
		d.f.Close()
		return nil, fmt.Errorf("EVIOCGNAME: %w", err)
	}
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		buf = buf[:i]
	}
	d.name = string(buf)
	return d, nil
}

// ioctl passes a pointer argument; the uintptr conversion must stay inside the Syscall call.
func (d *InputDev) ioctl(req uint, arg unsafe.Pointer) error {
	return d.control(func(fd uintptr) unix.Errno {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, uintptr(req), uintptr(arg))
		return errno
	})
}

func (d *InputDev) control(fn func(fd uintptr) unix.Errno) error {
	rc, err := d.f.SyscallConn()
	if err != nil {
		return err
	}
	var errno unix.Errno
	if err := rc.Control(func(fd uintptr) { errno = fn(fd) }); err != nil {
		return err
	}
	if errno != 0 {
		return errno
	}
	return nil
}

// Grab takes or releases exclusive access; while grabbed no other reader (the desktop) sees the events.
func (d *InputDev) Grab(on bool) error {
	if on == d.grabbed {
		return nil
	}
	arg := uintptr(0)
	if on {
		arg = 1
	}
	if err := d.control(func(fd uintptr) unix.Errno {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, eviocgrab, arg)
		return errno
	}); err != nil {
		return err
	}
	d.grabbed = on
	return nil
}

// Keys returns the keys and buttons currently held down on the device.
func (d *InputDev) Keys() (keyBits, error) {
	var k keyBits
	err := d.ioctl(eviocgkey(len(k)), unsafe.Pointer(&k))
	return k, err
}

// ReadEvents blocks until at least one event is available.
func (d *InputDev) ReadEvents(buf []byte) ([]inputEvent, error) {
	n, err := d.f.Read(buf)
	if err != nil {
		return nil, err
	}
	events := make([]inputEvent, 0, n/eventSize)
	for off := 0; off+eventSize <= n; off += eventSize {
		p := buf[off+eventSize-8:]
		events = append(events, inputEvent{
			Type:  binary.NativeEndian.Uint16(p[0:]),
			Code:  binary.NativeEndian.Uint16(p[2:]),
			Value: int32(binary.NativeEndian.Uint32(p[4:])),
		})
	}
	return events, nil
}

func (d *InputDev) Close() error {
	_ = d.Grab(false)
	return d.f.Close()
}

type inputCandidate struct {
	path    string
	forward bool
}

// discoverInputPaths returns candidate devices and whether each should be forwarded.
func discoverInputPaths(explicit []string, only string) []inputCandidate {
	var res []inputCandidate
	if len(explicit) > 0 {
		for _, p := range explicit {
			res = append(res, inputCandidate{p, true})
		}
		return res
	}
	kbds, _ := filepath.Glob("/dev/input/by-id/*-event-kbd")
	mice, _ := filepath.Glob("/dev/input/by-id/*-event-mouse")
	// keyboards are always opened (the toggle key lives there) but not forwarded with -only mouse
	for _, p := range kbds {
		res = append(res, inputCandidate{p, only != "mouse"})
	}
	if only != "keyboard" {
		for _, p := range mice {
			res = append(res, inputCandidate{p, true})
		}
	}
	return res
}
