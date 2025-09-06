package serial

import (
	"errors"

	"golang.org/x/sys/unix"
)

var baudrateMap = map[int]uint32{
	300:    unix.B300,
	600:    unix.B600,
	1200:   unix.B1200,
	2400:   unix.B2400,
	4800:   unix.B4800,
	9600:   unix.B9600,
	19200:  unix.B19200,
	38400:  unix.B38400,
	57600:  unix.B57600,
	115200: unix.B115200,
}

var databitsMap = map[int]uint32{
	5: unix.CS5,
	6: unix.CS6,
	7: unix.CS7,
	8: unix.CS8,
}

const (
	StopBits1 uint32 = 0
	StopBits2 uint32 = unix.CSTOPB

	ParityNone  uint32 = 0
	ParityOdd   uint32 = unix.PARENB | unix.PARODD
	ParityEven  uint32 = unix.PARENB
	ParityMark  uint32 = unix.PARENB | unix.CMSPAR | unix.PARODD
	ParitySpace uint32 = unix.PARENB | unix.CMSPAR
)

var (
	ErrInvalidBaudRate = errors.New("invalid baudrate")
	ErrInvalidDataBits = errors.New("invalid databits")
)
