package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/whoisnian/glb/ansi"
	"github.com/whoisnian/glb/config"
	"github.com/whoisnian/glb/logger"
	"github.com/whoisnian/misc/pkg/serial"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

var CFG struct {
	Debug   bool   `flag:"d,false,Enable debug output"`
	Test    bool   `flag:"t,false,Run in test mode without sending keycodes"`
	Device  string `flag:"dev,/dev/ttyUSB0,Serial device to use"`
	Encoder string `flag:"enc,ch9329,Encoder for keycodes, ch9329 or kcom3"`
}

var LOG *logger.Logger

func setupConfigAndLogger(_ context.Context) {
	_, err := config.FromCommandLine(&CFG)
	if err != nil {
		panic(err)
	}
	level := logger.LevelInfo
	if CFG.Debug {
		level = logger.LevelDebug
	}
	LOG = logger.New(logger.NewNanoHandler(os.Stderr, logger.Options{
		Level:     level,
		Colorful:  ansi.IsSupported(os.Stderr.Fd()),
		AddSource: CFG.Debug,
	}))
}

func main() {
	ctx := context.Background()
	setupConfigAndLogger(ctx)
	LOG.Debugf(ctx, "use config: %+v", CFG)

	if CFG.Test {
		runTestMode(ctx)
	} else {
		runCliMode(ctx)
	}
}

func runTestMode(ctx context.Context) {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		LOG.Fatalf(ctx, "failed to set terminal to raw mode: %v", err)
	}
	defer term.Restore(fd, oldState)

	var buf [8]byte
	var code KeyCode
	isCombo := false
	isExit := false
	for {
		n, err := unix.Read(fd, buf[:])
		fmt.Printf("ori: %s%x%s\r\n", ansi.BlueFG, buf[:n], ansi.Reset)
		code, isCombo, isExit = DecodeFromCli(buf[:n], isCombo)
		if isCombo {
			fmt.Printf("res: %sComboMode%s\r\n", ansi.GreenFG, ansi.Reset)
		} else {
			fmt.Printf("res: %s%s%s\r\n", ansi.GreenFG, code, ansi.Reset)
			if isExit {
				break
			}
		}

		if err == io.EOF {
			break
		} else if err != nil {
			fmt.Printf("err: %s%v%s\r\n", ansi.RedFG, err, ansi.Reset)
			break
		}
	}
}

func runCliMode(ctx context.Context) {
	var encodeFunc EncodeFunc
	switch CFG.Encoder {
	case "ch9329":
		encodeFunc = EncodeForCH9329
	case "kcom3":
		encodeFunc = EncodeForKCOM3
	default:
		LOG.Fatalf(ctx, "unknown encoder %q, must be ch9329 or kcom3", CFG.Encoder)
	}

	ttyPort, err := serial.Open(CFG.Device, 9600, 8, serial.ParityNone, serial.StopBits1)
	if err != nil {
		LOG.Fatalf(ctx, "failed to open serial port %s: %v", CFG.Device, err)
	}
	defer ttyPort.Close()

	ttyPort.SetInterval(time.Millisecond * 50)
	stop := ttyPort.GoWaitAndSend()

	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		LOG.Fatalf(ctx, "failed to set terminal to raw mode: %v", err)
	}
	defer term.Restore(fd, oldState)

	var buf [8]byte
	var code KeyCode
	isCombo := false
	isExit := false
	for {
		n, err := unix.Read(fd, buf[:])
		if CFG.Debug {
			fmt.Printf("ori: %s%x%s\r\n", ansi.BlueFG, buf[:n], ansi.Reset)
		}
		code, isCombo, isExit = DecodeFromCli(buf[:n], isCombo)
		if isCombo {
			fmt.Printf("res: %sComboMode%s\r\n", ansi.GreenFG, ansi.Reset)
		} else {
			fmt.Printf("res: %s%s%s\r\n", ansi.GreenFG, code, ansi.Reset)
			if isExit {
				break
			}
			if res := encodeFunc(code); len(res) > 0 {
				ttyPort.Push(res)
				ttyPort.Push(encodeFunc(EmptyKeyCode))
			}
		}

		if err == io.EOF {
			break
		} else if err != nil {
			fmt.Printf("err: %s%v%s\r\n", ansi.RedFG, err, ansi.Reset)
			break
		}
	}
	stop()
}
