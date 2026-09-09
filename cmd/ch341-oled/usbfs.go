//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// linux/usbdevice_fs.h 里的 ioctl 编号，x/sys/unix 没有导出，这里自行拼装。
const (
	iocNone  uintptr = 0
	iocWrite uintptr = 1
	iocRead  uintptr = 2
)

func iocValue(dir, typ, nr, size uintptr) uintptr {
	return dir<<30 | size<<16 | typ<<8 | nr
}

// struct usbdevfs_bulktransfer，末尾指针由 Go 自动按平台对齐，布局与 C 一致。
type usbBulkTransfer struct {
	ep      uint32
	length  uint32
	timeout uint32 // 单位毫秒
	data    *byte
}

// struct usbdevfs_ioctl，用于把 DISCONNECT/CONNECT 转发给指定接口。
type usbIoctl struct {
	ifno      int32
	ioctlCode int32
	data      *byte
}

var (
	usbdevfsBulk             = iocValue(iocRead|iocWrite, 'U', 2, unsafe.Sizeof(usbBulkTransfer{}))
	usbdevfsClaimInterface   = iocValue(iocRead, 'U', 15, 4)
	usbdevfsReleaseInterface = iocValue(iocRead, 'U', 16, 4)
	usbdevfsIoctl            = iocValue(iocRead|iocWrite, 'U', 18, unsafe.Sizeof(usbIoctl{}))
	usbdevfsDisconnect       = iocValue(iocNone, 'U', 22, 0)
	usbdevfsConnect          = iocValue(iocNone, 'U', 23, 0)
)

var ErrDeviceNotFound = errors.New("usb device not found")

// FindDevice 在 sysfs 中按 vid:pid 查找设备，返回 usbfs 节点路径和 sysfs 名称。
func FindDevice(vid, pid uint16) (devPath string, sysName string, err error) {
	const base = "/sys/bus/usb/devices"
	entries, err := os.ReadDir(base)
	if err != nil {
		return "", "", err
	}
	readHex := func(dir, name string) (uint64, bool) {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return 0, false
		}
		v, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 16, 64)
		return v, err == nil
	}
	readDec := func(dir, name string) (uint64, bool) {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return 0, false
		}
		v, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
		return v, err == nil
	}
	for _, entry := range entries {
		dir := filepath.Join(base, entry.Name())
		gotVid, ok := readHex(dir, "idVendor")
		if !ok || uint16(gotVid) != vid {
			continue
		}
		gotPid, ok := readHex(dir, "idProduct")
		if !ok || uint16(gotPid) != pid {
			continue
		}
		bus, ok1 := readDec(dir, "busnum")
		dev, ok2 := readDec(dir, "devnum")
		if !ok1 || !ok2 {
			continue
		}
		path := "/dev/bus/usb/" + pad3(bus) + "/" + pad3(dev)
		return path, entry.Name(), nil
	}
	return "", "", ErrDeviceNotFound
}

func pad3(v uint64) string {
	s := strconv.FormatUint(v, 10)
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

// USBDevice 通过 usbfs 直接与 USB 设备通信，不依赖 libusb。
type USBDevice struct {
	file *os.File
	ifno int32
}

// OpenDevice 打开 usbfs 节点，先把已绑定的内核驱动从接口摘掉，再独占该接口。
func OpenDevice(ctx context.Context, devPath string, ifno int32) (*USBDevice, error) {
	file, err := os.OpenFile(devPath, os.O_RDWR, 0666)
	if err != nil {
		return nil, err
	}
	d := &USBDevice{file: file, ifno: ifno}

	// CH341 在内核里会被 spi-ch341 抢走，这里断开它才能自己收发。
	if err := d.forwardIoctl(usbdevfsDisconnect); err != nil && !errors.Is(err, unix.ENODATA) {
		LOG.Debugf(ctx, "disconnect kernel driver from interface %d: %v", ifno, err)
	}
	if err := d.ioctl(usbdevfsClaimInterface, unsafe.Pointer(&ifno)); err != nil {
		file.Close()
		return nil, err
	}
	return d, nil
}

// Close 释放接口，reconnect 为真时把设备交还给内核驱动。
func (d *USBDevice) Close(ctx context.Context, reconnect bool) error {
	ifno := d.ifno
	if err := d.ioctl(usbdevfsReleaseInterface, unsafe.Pointer(&ifno)); err != nil {
		LOG.Debugf(ctx, "release interface %d: %v", ifno, err)
	}
	if reconnect {
		if err := d.forwardIoctl(usbdevfsConnect); err != nil {
			LOG.Debugf(ctx, "reconnect kernel driver to interface %d: %v", ifno, err)
		}
	}
	return d.file.Close()
}

// BulkTransfer 在指定端点上收发一次批量传输，ep 的最高位为 1 表示读入。
func (d *USBDevice) BulkTransfer(ep uint8, buf []byte, timeoutMS uint32) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	bt := usbBulkTransfer{
		ep:      uint32(ep),
		length:  uint32(len(buf)),
		timeout: timeoutMS,
		data:    &buf[0],
	}
	n, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		d.file.Fd(),
		usbdevfsBulk,
		uintptr(unsafe.Pointer(&bt)),
	)
	runtime.KeepAlive(buf)
	if errno != 0 {
		return 0, errno
	}
	return int(n), nil
}

func (d *USBDevice) ioctl(req uintptr, arg unsafe.Pointer) error {
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, d.file.Fd(), req, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}

// forwardIoctl 通过 USBDEVFS_IOCTL 把子命令转发到接口上，用于绑定/解绑内核驱动。
func (d *USBDevice) forwardIoctl(code uintptr) error {
	arg := usbIoctl{ifno: d.ifno, ioctlCode: int32(code)}
	return d.ioctl(usbdevfsIoctl, unsafe.Pointer(&arg))
}
