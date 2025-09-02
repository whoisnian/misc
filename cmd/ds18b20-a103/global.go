package main

import (
	"context"
	"os"

	"github.com/whoisnian/glb/ansi"
	"github.com/whoisnian/glb/config"
	"github.com/whoisnian/glb/logger"
)

type Config struct {
	Debug  bool   `flag:"d,false,Enable debug output"`
	Device string `flag:"dev,/dev/ttyUSB0,Serial device to use"`
}

var CFG Config

func setupConfig(_ context.Context) {
	_, err := config.FromCommandLine(&CFG)
	if err != nil {
		panic(err)
	}
}

var LOG *logger.Logger

func setupLogger(_ context.Context) {
	if CFG.Debug {
		LOG = logger.New(logger.NewNanoHandler(os.Stderr, logger.Options{
			Level:     logger.LevelDebug,
			Colorful:  ansi.IsSupported(os.Stderr.Fd()),
			AddSource: true,
		}))
	} else {
		LOG = logger.New(logger.NewNanoHandler(os.Stderr, logger.Options{
			Level:     logger.LevelInfo,
			Colorful:  ansi.IsSupported(os.Stderr.Fd()),
			AddSource: false,
		}))
	}
}
