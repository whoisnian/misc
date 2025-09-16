package main

import (
	"context"
	"errors"
	"os"

	"github.com/whoisnian/glb/ansi"
	"github.com/whoisnian/glb/config"
	"github.com/whoisnian/glb/logger"
)

var CFG struct {
	Debug      bool   `flag:"d,false,Enable debug output"`
	Shell      string `flag:"s,,Specify shell for local/sshd/httpd mode"`
	WorkingDir string `flag:"dir,~,Specify working directory for local/sshd/httpd mode"`
	Local      bool   `flag:"local,,Run as local terminal"`
	SSHServer  string `flag:"sshd,,Run as ssh server (e.g. user:pass@127.0.0.1:2222)"`
	SSHClient  string `flag:"ssh,,Run as ssh client (e.g. user:pass@127.0.0.1:2222)"`
	HTTPServer string `flag:"httpd,,Run as http server (e.g. 127.0.0.1:8080)"`
	HTTPClient string `flag:"http,,Run as http client (e.g. 127.0.0.1:8080)"`
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
	if CFG.SSHServer != "" {
		err = runSSHServer(ctx, CFG.SSHServer)
	} else if CFG.SSHClient != "" {
		err = runSSHClient(ctx, CFG.SSHClient)
	} else if CFG.HTTPServer != "" {
		err = runHTTPServer(ctx, CFG.HTTPServer)
	} else if CFG.HTTPClient != "" {
		err = runHTTPClient(ctx, CFG.HTTPClient)
	} else if CFG.Local {
		err = runLocal(ctx)
	} else {
		err = errors.New("no run mode specified, use -help to see usage")
	}
	if err != nil {
		LOG.Fatalf(ctx, "run error: %v", err)
	}
}
