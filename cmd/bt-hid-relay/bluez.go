//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

const (
	bluezService = "org.bluez"
	agentPath    = dbus.ObjectPath("/bt_hid_relay/agent")
	profilePath  = dbus.ObjectPath("/bt_hid_relay/profile")
	bluetoothd   = "/usr/lib/bluetooth/bluetoothd"
)

// bluetoothdConf is the main.conf for the private bluetoothd; nothing under /etc is touched.
const bluetoothdConf = `[General]
Name = %s
# Class of Device: major 0x05 (Peripheral), minor 0x30 (keyboard + pointing device combo)
Class = 0x0005C0
# 0 = the relay controls discoverable/pairable timeouts over D-Bus
DiscoverableTimeout = 0
PairableTimeout = 0
# let the host re-pair after "removing" the device without cleaning up on this side
JustWorksRepairing = always
# PnP (Device ID) record read by the host; 1d6b = Linux Foundation
DeviceID = bluetooth:1d6b:0246:0537

[Policy]
AutoEnable = true
`

// agent implements org.bluez.Agent1 and accepts every pairing request while -pair is open.
type agent struct {
	ctx     context.Context
	canPair func() bool
}

// allow reports whether a pairing request may proceed, as the error to hand back to BlueZ.
func (a *agent) allow(dev dbus.ObjectPath, what string) *dbus.Error {
	if !a.canPair() {
		LOG.Warnf(a.ctx, "%s for %s: rejected, pairing window is closed", what, deviceMAC(dev))
		return dbus.NewError("org.bluez.Error.Rejected", nil)
	}
	LOG.Infof(a.ctx, "%s for %s: accepted", what, deviceMAC(dev))
	return nil
}

func (a *agent) Release() *dbus.Error { return nil }
func (a *agent) Cancel() *dbus.Error  { return nil }
func (a *agent) RequestPinCode(dev dbus.ObjectPath) (string, *dbus.Error) {
	if err := a.allow(dev, "pin code request"); err != nil {
		return "", err
	}
	return "0000", nil
}
func (a *agent) RequestPasskey(dev dbus.ObjectPath) (uint32, *dbus.Error) {
	return 0, a.allow(dev, "passkey request")
}
func (a *agent) DisplayPinCode(dev dbus.ObjectPath, pin string) *dbus.Error {
	LOG.Infof(a.ctx, "pin code %s for %s", pin, deviceMAC(dev))
	return nil
}
func (a *agent) DisplayPasskey(dev dbus.ObjectPath, passkey uint32, entered uint16) *dbus.Error {
	LOG.Infof(a.ctx, "passkey %06d for %s", passkey, deviceMAC(dev))
	return nil
}
func (a *agent) RequestConfirmation(dev dbus.ObjectPath, passkey uint32) *dbus.Error {
	return a.allow(dev, fmt.Sprintf("confirm passkey %06d", passkey))
}
func (a *agent) RequestAuthorization(dev dbus.ObjectPath) *dbus.Error {
	return a.allow(dev, "authorization")
}
func (a *agent) AuthorizeService(dev dbus.ObjectPath, uuid string) *dbus.Error {
	LOG.Debugf(a.ctx, "authorize service %s for %s: accepted", uuid, deviceMAC(dev))
	return nil
}

// profile implements org.bluez.Profile1. It exists only so BlueZ publishes our SDP record; the
// L2CAP sockets are managed by the relay itself, so any fd BlueZ hands over is simply closed.
type profile struct{}

func (p *profile) Release() *dbus.Error { return nil }
func (p *profile) NewConnection(dev dbus.ObjectPath, fd dbus.UnixFD, props map[string]dbus.Variant) *dbus.Error {
	unix.Close(int(fd))
	return nil
}
func (p *profile) RequestDisconnection(dev dbus.ObjectPath) *dbus.Error { return nil }

// deviceMAC turns /org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF into AA:BB:CC:DD:EE:FF.
func deviceMAC(path dbus.ObjectPath) string {
	s := string(path)
	if i := strings.LastIndex(s, "/dev_"); i >= 0 {
		return strings.ReplaceAll(s[i+5:], "_", ":")
	}
	return s
}

type bluez struct {
	ctx         context.Context
	conn        *dbus.Conn
	adapterPath dbus.ObjectPath
	addr        bdaddr
}

func connectBlueZ(ctx context.Context, wantAdapter string) (*bluez, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("system bus: %w", err)
	}
	b := &bluez{ctx: ctx, conn: conn}
	// bluetoothd owns org.bluez before it has finished probing the adapters
	for attempt := 0; attempt < 25; attempt++ {
		found, err := b.findAdapter(wantAdapter)
		if err != nil {
			return nil, err
		}
		if found {
			return b, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("bluetooth adapter %q not found", wantAdapter)
}

func (b *bluez) findAdapter(wantAdapter string) (bool, error) {
	objs, err := b.managedObjects()
	if err != nil {
		return false, fmt.Errorf("GetManagedObjects (is bluetoothd running?): %w", err)
	}
	paths := make([]string, 0, len(objs))
	for p := range objs {
		paths = append(paths, string(p))
	}
	sort.Strings(paths)
	for _, p := range paths {
		ad, ok := objs[dbus.ObjectPath(p)]["org.bluez.Adapter1"]
		if !ok || (wantAdapter != "" && !strings.HasSuffix(p, "/"+wantAdapter)) {
			continue
		}
		addr, err := parseBDAddr(ad["Address"].Value().(string))
		if err != nil {
			return false, err
		}
		b.adapterPath, b.addr = dbus.ObjectPath(p), addr
		return true, nil
	}
	return false, nil
}

func (b *bluez) managedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, error) {
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	err := b.conn.Object(bluezService, "/").Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objs)
	return objs, err
}

// pairedDevices returns the addresses of every device paired with the adapter.
func (b *bluez) pairedDevices() ([]bdaddr, error) {
	objs, err := b.managedObjects()
	if err != nil {
		return nil, err
	}
	var res []bdaddr
	for p, ifaces := range objs {
		dev, ok := ifaces["org.bluez.Device1"]
		if !ok || !strings.HasPrefix(string(p), string(b.adapterPath)+"/") {
			continue
		}
		if paired, _ := dev["Paired"].Value().(bool); !paired {
			continue
		}
		if a, err := parseBDAddr(dev["Address"].Value().(string)); err == nil {
			res = append(res, a)
		}
	}
	sort.Slice(res, func(i, j int) bool { return res[i].String() < res[j].String() })
	return res, nil
}

func (b *bluez) registerAgent(canPair func() bool) error {
	if err := b.conn.Export(&agent{ctx: b.ctx, canPair: canPair}, agentPath, "org.bluez.Agent1"); err != nil {
		return err
	}
	mgr := b.conn.Object(bluezService, "/org/bluez")
	if err := mgr.Call("org.bluez.AgentManager1.RegisterAgent", 0, agentPath, "NoInputNoOutput").Err; err != nil {
		return fmt.Errorf("RegisterAgent: %w", err)
	}
	if err := mgr.Call("org.bluez.AgentManager1.RequestDefaultAgent", 0, agentPath).Err; err != nil {
		return fmt.Errorf("RequestDefaultAgent: %w", err)
	}
	return nil
}

func (b *bluez) registerProfile(name string) error {
	if err := b.conn.Export(&profile{}, profilePath, "org.bluez.Profile1"); err != nil {
		return err
	}
	opts := map[string]dbus.Variant{
		"ServiceRecord":         dbus.MakeVariant(sdpRecord(name)),
		"Role":                  dbus.MakeVariant("server"),
		"RequireAuthentication": dbus.MakeVariant(false),
		"RequireAuthorization":  dbus.MakeVariant(false),
	}
	err := b.conn.Object(bluezService, "/org/bluez").Call("org.bluez.ProfileManager1.RegisterProfile", 0, profilePath, hidUUID, opts).Err
	if err != nil {
		return fmt.Errorf("RegisterProfile: %w", err)
	}
	return nil
}

// setAdapter sets an org.bluez.Adapter1 property, retrying while the adapter is still powering up.
func (b *bluez) setAdapter(prop string, value any) error {
	obj := b.conn.Object(bluezService, b.adapterPath)
	const iface = "org.bluez.Adapter1."
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		if cur, gerr := obj.GetProperty(iface + prop); gerr == nil && cur.Value() == value {
			return nil
		}
		err = obj.SetProperty(iface+prop, dbus.MakeVariant(value))
		var derr dbus.Error
		if err == nil || !errors.As(err, &derr) || derr.Name != "org.bluez.Error.Busy" {
			return err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return err
}

// devicePropertyChanges delivers PropertiesChanged signals of org.bluez.Device1 objects.
func (b *bluez) devicePropertyChanges() (<-chan *dbus.Signal, error) {
	err := b.conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchArg(0, "org.bluez.Device1"),
	)
	if err != nil {
		return nil, err
	}
	ch := make(chan *dbus.Signal, 32)
	b.conn.Signal(ch)
	return ch, nil
}

// spawnBluetoothd starts a private bluetoothd with the input plugin disabled (it would otherwise
// own L2CAP PSM 17/19 as a HID host) and a config file written to a temp dir. It refuses to run
// next to the system bluetooth.service since both would claim org.bluez.
func spawnBluetoothd(ctx context.Context, name string) (stop func(), err error) {
	if exec.Command("systemctl", "is-active", "--quiet", "bluetooth").Run() == nil {
		return nil, errors.New("system bluetooth.service is running, stop it first: sudo systemctl stop bluetooth")
	}
	if conn, err := dbus.SystemBus(); err == nil {
		var owned bool
		if conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, bluezService).Store(&owned) == nil && owned {
			return nil, errors.New("org.bluez is already owned by another bluetoothd")
		}
	}
	dir, err := os.MkdirTemp("", "bt-hid-relay-")
	if err != nil {
		return nil, err
	}
	conf := dir + "/main.conf"
	if err := os.WriteFile(conf, []byte(fmt.Sprintf(bluetoothdConf, name)), 0o644); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	args := []string{"-n", "-P", "input", "-f", conf}
	cmd := exec.Command(bluetoothd, args...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("start %s: %w", bluetoothd, err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	stop = func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		<-exited
		os.RemoveAll(dir)
	}
	LOG.Debugf(ctx, "spawned %s %v (pid %d)", bluetoothd, args, cmd.Process.Pid)

	// wait for org.bluez to appear on the bus
	conn, err := dbus.SystemBus()
	if err != nil {
		stop()
		return nil, err
	}
	for i := 0; i < 50; i++ {
		var owned bool
		if conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, bluezService).Store(&owned) == nil && owned {
			return stop, nil
		}
		select {
		case err := <-exited:
			os.RemoveAll(dir)
			return nil, fmt.Errorf("bluetoothd exited: %v", err)
		case <-time.After(200 * time.Millisecond):
		}
	}
	stop()
	return nil, errors.New("bluetoothd did not claim org.bluez in time")
}
