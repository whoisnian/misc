package main

import (
	"context"
	"os"

	"github.com/whoisnian/glb/ansi"
	"github.com/whoisnian/glb/config"
	"github.com/whoisnian/glb/logger"
)

var CFG struct {
	Debug    bool   `flag:"d,false,Enable debug output"`
	Interval int    `flag:"i,5,Interval seconds between two measurements"`
	Pid      int    `flag:"p,,Find target process by pid"`
	Cmd      string `flag:"c,,Find target process by cmdline substring, oldest if multiple matches"`
	Sock     string `flag:"s,/var/run/docker.sock,List all docker containers by docker socket"`
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

	var err error
	fcache := NewFileCache()
	defer func() {
		fcache.CloseAll()
		if err != nil {
			LOG.Fatalf(ctx, "error: %v", err)
		}
	}()

	if CFG.Pid != 0 {
		err = ShowProcessStatsByPid(ctx, fcache, CFG.Pid)
	} else if CFG.Cmd != "" {
		err = ShowProcessStatsByCmd(ctx, fcache, CFG.Cmd)
	} else {
		err = ShowDockerStatsBySock(ctx, fcache, CFG.Sock)
	}
}
