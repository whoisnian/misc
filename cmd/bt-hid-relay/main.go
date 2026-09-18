//go:build linux

package main

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/whoisnian/glb/ansi"
	"github.com/whoisnian/glb/config"
	"github.com/whoisnian/glb/logger"
)

var CFG struct {
	Debug   bool   `flag:"d,false,Enable debug output"`
	Pair    int    `flag:"pair,-1,Open a pairing window for N seconds (0 = no limit, -1 = closed)"`
	Host    string `flag:"host,,Host address to reconnect to (default: every device paired with the adapter)"`
	Adapter string `flag:"adapter,,Bluetooth adapter like hci0 (default: the first one)"`
	Name    string `flag:"name,Linux HID Relay,Device name shown to the host"`
	Toggle  string `flag:"toggle,KEY_SCROLLLOCK,Key that switches input between local and host: KEY_* name or evdev code"`
	Dev     string `flag:"|dev||Comma-separated evdev devices to forward (default: /dev/input/by-id/*-event-kbd and *-event-mouse)"`
	Only    string `flag:"only,,Forward only 'keyboard' or 'mouse'"`
	Test    bool   `flag:"test,false,Send synthetic mouse movement after connecting to verify the link"`
	Btd     bool   `flag:"btd,true,Spawn a private bluetoothd with the input plugin disabled"`
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

func parseToggle(s string) (uint16, bool) {
	if code, ok := keyNames[strings.ToUpper(s)]; ok {
		return code, true
	}
	if n, err := strconv.ParseUint(s, 10, 16); err == nil {
		return uint16(n), true
	}
	return 0, false
}

func main() {
	ctx := context.Background()
	setupConfigAndLogger(ctx)
	LOG.Debugf(ctx, "use config: %+v", CFG)

	opt := RelayOptions{ToggleName: CFG.Toggle, Only: CFG.Only, Test: CFG.Test}
	var ok bool
	if opt.ToggleCode, ok = parseToggle(CFG.Toggle); !ok {
		names := make([]string, 0, len(keyNames))
		for n := range keyNames {
			names = append(names, n)
		}
		LOG.Fatalf(ctx, "unknown -toggle key %q, use an evdev code or one of %s", CFG.Toggle, strings.Join(names, " "))
	}
	if CFG.Only != "" && CFG.Only != "keyboard" && CFG.Only != "mouse" {
		LOG.Fatalf(ctx, "-only must be 'keyboard' or 'mouse'")
	}
	for _, p := range strings.Split(CFG.Dev, ",") {
		if p = strings.TrimSpace(p); p != "" {
			opt.Devices = append(opt.Devices, p)
		}
	}
	if CFG.Host != "" {
		host, err := parseBDAddr(CFG.Host)
		if err != nil {
			LOG.Fatalf(ctx, "%v", err)
		}
		opt.Hosts = []bdaddr{host}
	}

	// bluetoothd may only own org.bluez as root, and L2CAP PSM 17/19 need CAP_NET_BIND_SERVICE
	if os.Geteuid() != 0 {
		LOG.Fatalf(ctx, "root is required, run with sudo")
	}

	if CFG.Btd {
		stop, err := spawnBluetoothd(ctx, CFG.Name)
		if err != nil {
			LOG.Fatalf(ctx, "spawn bluetoothd: %v", err)
		}
		defer stop()
	}

	bz, err := connectBlueZ(ctx, CFG.Adapter)
	if err != nil {
		LOG.Fatalf(ctx, "%v", err)
	}
	LOG.Infof(ctx, "adapter %s (%s)", bz.adapterPath, bz.addr)
	if err := bz.registerAgent(); err != nil {
		LOG.Fatalf(ctx, "%v", err)
	}
	// only borrow BlueZ to publish the SDP record; the L2CAP channels are handled by the relay
	if err := bz.registerProfile(CFG.Name); err != nil {
		LOG.Fatalf(ctx, "%v", err)
	}
	for _, p := range []struct {
		name  string
		value any
	}{
		{"Alias", CFG.Name},
		{"Powered", true},
		{"DiscoverableTimeout", uint32(max(CFG.Pair, 0))},
		{"PairableTimeout", uint32(max(CFG.Pair, 0))},
		{"Pairable", CFG.Pair >= 0},
		{"Discoverable", CFG.Pair >= 0},
	} {
		if err := bz.setAdapter(p.name, p.value); err != nil {
			LOG.Fatalf(ctx, "set adapter %s: %v", p.name, err)
		}
	}
	if CFG.Pair == 0 {
		LOG.Infof(ctx, "*** pairing window open with no time limit, add %q on the host", CFG.Name)
	} else if CFG.Pair > 0 {
		LOG.Infof(ctx, "*** pairing window open for %ds, add %q on the host", CFG.Pair, CFG.Name)
	}

	relay := NewRelay(ctx, bz, opt)
	if err := relay.Run(); err != nil {
		LOG.Fatalf(ctx, "%v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	LOG.Infof(ctx, "exiting")
	relay.Close()
	// the agent and profile are unregistered by BlueZ when our bus connection goes away
}
