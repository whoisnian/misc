//go:build linux

package main

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

const (
	retryMin = 3 * time.Second
	retryMax = 30 * time.Second
)

type Relay struct {
	ctx        context.Context
	bz         *bluez
	toggleCode uint16
	toggleName string
	only       string
	explicit   []string
	test       bool
	keepalive  time.Duration
	heldKeys   func() keyBits // key state of the forwarded devices; caller holds mu

	mu sync.Mutex

	// bluetooth link
	ctrl, intr *l2capConn
	peer       bdaddr
	gen        uint64 // bumped whenever the control channel changes, so a stale dial can notice
	hosts      []bdaddr
	dialing    bool
	retryDelay time.Duration
	retryTimer *time.Timer

	// input devices and HID state
	devices    map[string]*InputDev // keyed by resolved path
	forwarding bool
	pendingOn  bool // switching to the host once the held keys are released
	kbMods     byte
	kbKeys     []byte
	kbDirty    bool
	btns       byte
	rel        [4]int32 // X, Y, wheel, hwheel
	mouseDirty bool
	lastReport map[byte][]byte
}

type RelayOptions struct {
	ToggleCode uint16
	ToggleName string
	Only       string   // "", "keyboard" or "mouse"
	Devices    []string // explicit evdev paths, empty for auto discovery
	Hosts      []bdaddr // allowed hosts, empty for every paired device
	Test       bool
	Keepalive  time.Duration // 0 to disable
}

func NewRelay(ctx context.Context, bz *bluez, opt RelayOptions) *Relay {
	r := &Relay{
		ctx:        ctx,
		bz:         bz,
		toggleCode: opt.ToggleCode,
		toggleName: opt.ToggleName,
		only:       opt.Only,
		explicit:   opt.Devices,
		hosts:      opt.Hosts,
		test:       opt.Test,
		keepalive:  opt.Keepalive,
		retryDelay: retryMin,
		devices:    map[string]*InputDev{},
		lastReport: map[byte][]byte{
			ridKeyboard: append([]byte{ridKeyboard}, make([]byte, 8)...),
			ridMouse:    append([]byte{ridMouse}, make([]byte, 5)...),
			ridConsumer: {ridConsumer, 0, 0},
		},
	}
	r.heldKeys = r.readHeldKeys
	return r
}

func (r *Relay) connected() bool { return r.ctrl != nil && r.intr != nil }

// ---------------------------------------------------------------- lifecycle

func (r *Relay) Run() error {
	for _, psm := range []uint16{psmCtrl, psmIntr} {
		lfd, err := listenL2CAP(r.bz.addr, psm)
		if err != nil {
			if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
				return errors.New("binding L2CAP PSM 17/19 needs root (CAP_NET_BIND_SERVICE)")
			}
			if errors.Is(err, unix.EADDRINUSE) {
				return errors.New("PSM already in use: another bluetoothd with the input plugin is running")
			}
			return err
		}
		go r.acceptLoop(lfd, psm)
	}
	LOG.Infof(r.ctx, "listening on L2CAP PSM %d/%d as %s", psmCtrl, psmIntr, r.bz.addr)

	if sigs, err := r.bz.devicePropertyChanges(); err == nil {
		go r.watchDevices(sigs)
	} else {
		LOG.Warnf(r.ctx, "cannot watch device properties: %v", err)
	}

	r.discoverDevices()
	if len(r.devices) == 0 {
		LOG.Warnf(r.ctx, "no input device found under /dev/input/by-id, use -dev to specify one")
	}
	go func() { // hotplug
		for {
			time.Sleep(5 * time.Second)
			r.discoverDevices()
		}
	}()
	if r.keepalive > 0 {
		go r.keepaliveLoop()
	}

	r.mu.Lock()
	r.scheduleDial(time.Second)
	r.mu.Unlock()
	return nil
}

func (r *Relay) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.forwarding {
		r.setForwarding(false, true)
	}
	for _, d := range r.devices {
		d.Close()
	}
	r.devices = map[string]*InputDev{}
	r.dropConnection("shutting down", false)
}

// ---------------------------------------------------------------- reconnect targets

func (r *Relay) dialTargets() []bdaddr {
	if len(r.hosts) > 0 {
		return r.hosts
	}
	paired, err := r.bz.pairedDevices()
	if err != nil {
		LOG.Debugf(r.ctx, "list paired devices: %v", err)
	}
	return paired
}

func (r *Relay) watchDevices(sigs <-chan *dbus.Signal) {
	for sig := range sigs {
		if len(sig.Body) < 2 {
			continue
		}
		changed, _ := sig.Body[1].(map[string]dbus.Variant)
		mac := deviceMAC(sig.Path)
		if v, ok := changed["Paired"]; ok {
			LOG.Infof(r.ctx, "device %s paired=%v", mac, v.Value())
			if paired, _ := v.Value().(bool); paired {
				r.mu.Lock()
				r.scheduleDial(time.Second)
				r.mu.Unlock()
			}
		}
		if v, ok := changed["Connected"]; ok {
			LOG.Debugf(r.ctx, "device %s acl connected=%v", mac, v.Value())
		}
	}
}

// ---------------------------------------------------------------- outbound

// scheduleDial arms the reconnect timer; caller holds r.mu.
func (r *Relay) scheduleDial(delay time.Duration) {
	if r.retryTimer != nil || r.connected() || r.dialing {
		return
	}
	r.retryTimer = time.AfterFunc(delay, r.dial)
}

func (r *Relay) dial() {
	r.mu.Lock()
	r.retryTimer = nil
	if r.connected() || r.dialing {
		r.mu.Unlock()
		return
	}
	r.dialing = true
	local := r.bz.addr
	r.mu.Unlock()

	targets := r.dialTargets()
	for _, host := range targets {
		if r.dialOne(local, host) {
			break
		}
	}

	r.mu.Lock()
	r.dialing = false
	if !r.connected() {
		if len(targets) == 0 {
			LOG.Debugf(r.ctx, "no paired device to reconnect to, waiting for the host")
		} else {
			LOG.Debugf(r.ctx, "reconnect failed, retry in %s", r.retryDelay)
		}
		r.scheduleDial(r.retryDelay)
		r.retryDelay = min(r.retryDelay*2, retryMax)
	}
	r.mu.Unlock()
}

// dialOne opens control then interrupt to one host; it reports whether the link is now up.
func (r *Relay) dialOne(local, host bdaddr) bool {
	LOG.Debugf(r.ctx, "dialing %s psm %d", host, psmCtrl)
	ctrl, err := dialL2CAP(local, host, psmCtrl)
	if err != nil {
		LOG.Debugf(r.ctx, "dial %s psm %d: %v", host, psmCtrl, err)
		return false
	}
	r.mu.Lock()
	if r.ctrl != nil { // the host connected to us in the meantime
		r.mu.Unlock()
		ctrl.Close()
		return true
	}
	gen := r.attach(ctrl, psmCtrl)
	r.mu.Unlock()

	LOG.Debugf(r.ctx, "dialing %s psm %d", host, psmIntr)
	intr, err := dialL2CAP(local, host, psmIntr)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gen != gen || r.intr != nil { // the host took over or opened interrupt while we were dialing
		if intr != nil {
			intr.Close()
		}
		return r.connected()
	}
	if err != nil {
		LOG.Debugf(r.ctx, "dial %s psm %d: %v", host, psmIntr, err)
		r.dropConnection("interrupt channel dial failed", false)
		return false
	}
	r.attach(intr, psmIntr)
	return true
}

// ---------------------------------------------------------------- inbound

func (r *Relay) acceptLoop(lfd int, psm uint16) {
	for {
		c, err := acceptL2CAP(lfd)
		if err != nil {
			LOG.Warnf(r.ctx, "accept psm %d: %v", psm, err)
			time.Sleep(time.Second)
			continue
		}
		r.mu.Lock()
		if why := r.refuse(c.peer, psm); why != "" {
			r.mu.Unlock()
			LOG.Warnf(r.ctx, "ignoring psm %d connection from %s: %s", psm, c.peer, why)
			c.Close()
			continue
		}
		if psm == psmCtrl {
			// a new control connection means the host started a fresh HID session
			r.dropConnection("host opened a new session", false)
		}
		r.attach(c, psm)
		r.mu.Unlock()
	}
}

// refuse returns why an inbound channel is not acceptable, empty if it is; caller holds r.mu.
func (r *Relay) refuse(peer bdaddr, psm uint16) string {
	switch {
	case len(r.hosts) > 0 && !slices.Contains(r.hosts, peer):
		return "not an allowed -host"
	case psm == psmCtrl && r.connected() && r.peer != peer:
		return "already connected to " + r.peer.String()
	case psm == psmIntr && (r.ctrl == nil || r.peer != peer):
		return "no control channel"
	}
	return ""
}

// ---------------------------------------------------------------- link management

// attach installs a channel and starts its reader; caller holds r.mu. It returns the current generation.
func (r *Relay) attach(c *l2capConn, psm uint16) uint64 {
	wasConnected := r.connected()
	r.peer = c.peer
	if psm == psmCtrl {
		r.ctrl = c
		r.gen++
	} else {
		if r.intr != nil { // reopened by the host, the old reader exits once it is closed
			r.intr.Close()
		}
		r.intr = c
	}
	go r.readLoop(c, psm)
	if r.connected() && !wasConnected {
		r.retryDelay = retryMin
		LOG.Infof(r.ctx, "connected to host %s, press %s to switch input", c.peer, r.toggleName)
		if r.test {
			time.AfterFunc(1500*time.Millisecond, r.selfTest)
		}
	}
	return r.gen
}

// dropConnection closes both channels; caller holds r.mu.
func (r *Relay) dropConnection(why string, reconnect bool) {
	had := r.ctrl != nil || r.intr != nil
	for _, c := range []*l2capConn{r.ctrl, r.intr} {
		if c != nil {
			c.Close()
		}
	}
	r.ctrl, r.intr = nil, nil
	r.gen++
	if had {
		LOG.Infof(r.ctx, "disconnected from host %s: %s", r.peer, why)
	}
	r.pendingOn = false
	if r.forwarding {
		r.setForwarding(false, false)
	}
	if reconnect {
		r.scheduleDial(r.retryDelay)
	}
}

func (r *Relay) readLoop(c *l2capConn, psm uint16) {
	buf := make([]byte, 1024)
	for {
		n, err := c.Read(buf)
		if err != nil {
			r.mu.Lock()
			if c == r.ctrl || c == r.intr { // still current, not already replaced
				if errors.Is(err, io.EOF) {
					r.dropConnection("closed by host", true)
				} else if !errors.Is(err, os.ErrClosed) {
					r.dropConnection(err.Error(), true)
				}
			}
			r.mu.Unlock()
			return
		}
		data := buf[:n]
		if psm == psmCtrl {
			r.mu.Lock()
			if c == r.ctrl {
				r.handleControl(data)
			}
			r.mu.Unlock()
		} else {
			LOG.Debugf(r.ctx, "interrupt <- %s", hex.EncodeToString(data)) // usually the LED output report
		}
	}
}

// handleControl answers host requests on the control channel; caller holds r.mu.
func (r *Relay) handleControl(data []byte) {
	if len(data) == 0 {
		return
	}
	msg, param := data[0]>>4, data[0]&0x0F
	LOG.Debugf(r.ctx, "control <- %s", hex.EncodeToString(data))
	switch msg {
	case msgHIDControl:
		if param == ctrlVirtualCableUnplug {
			LOG.Infof(r.ctx, "host sent VIRTUAL_CABLE_UNPLUG (device removed on the host side)")
			r.dropConnection("virtual cable unplug", false)
		}
	case msgGetReport:
		rtype := param & 0x3
		var rid byte
		if len(data) > 1 {
			rid = data[1]
		}
		if last, ok := r.lastReport[rid]; rtype == reportInput && ok {
			r.ctrlSend(append([]byte{dataInput}, last...))
		} else if rtype == reportInput {
			r.handshake(hsErrInvalidReportID)
		} else {
			r.handshake(hsErrUnsupported)
		}
	case msgSetReport, msgSetIdle:
		r.handshake(hsSuccess)
	case msgGetProtocol:
		r.ctrlSend([]byte{dataOther, 1}) // 1 = report protocol
	case msgSetProtocol:
		if param&1 == 1 {
			r.handshake(hsSuccess)
		} else {
			r.handshake(hsErrUnsupported) // boot protocol is not implemented
		}
	case msgGetIdle:
		r.ctrlSend([]byte{dataOther, 0})
	case msgData:
		// output report on the control channel, ignored
	default:
		r.handshake(hsErrUnsupported)
	}
}

func (r *Relay) handshake(result byte) { r.ctrlSend([]byte{msgHandshake<<4 | result}) }

func (r *Relay) ctrlSend(pkt []byte) {
	if r.ctrl == nil {
		return
	}
	if _, err := r.ctrl.Write(pkt); err != nil {
		r.dropConnection("control send: "+err.Error(), true)
	}
}

// sendReport pushes one input report over the interrupt channel; caller holds r.mu.
func (r *Relay) sendReport(rid byte, body []byte) {
	report := append([]byte{rid}, body...)
	r.lastReport[rid] = report
	if !r.connected() {
		return
	}
	pkt := append([]byte{dataInput}, report...)
	if _, err := r.intr.Write(pkt); err != nil {
		r.dropConnection("interrupt send: "+err.Error(), true)
		return
	}
	LOG.Debugf(r.ctx, "interrupt -> %s", hex.EncodeToString(pkt))
}

// keepaliveLoop keeps the link awake while forwarding. After an idle moment the USB adapter is
// autosuspended (2 s by default) and the host may have put the link in sniff mode, and the first
// reports sent into that state can get lost.
func (r *Relay) keepaliveLoop() {
	for range time.Tick(r.keepalive) {
		r.mu.Lock()
		r.sendKeepalive()
		r.mu.Unlock()
	}
}

// sendKeepalive repeats the keyboard state, which is a no-op for the host, unlike a zero mouse
// move that may count as user activity; caller holds r.mu.
func (r *Relay) sendKeepalive() {
	if r.forwarding {
		r.sendReport(ridKeyboard, r.lastReport[ridKeyboard][1:])
	}
}

func (r *Relay) sendReleaseAll() {
	r.sendReport(ridKeyboard, make([]byte, 8))
	r.sendReport(ridMouse, make([]byte, 5))
	r.sendReport(ridConsumer, make([]byte, 2))
}

// selfTest draws a square with the mouse and taps right shift so the link can be verified
// without touching the local devices.
func (r *Relay) selfTest() {
	send := func(rid byte, body []byte, wait time.Duration) {
		r.mu.Lock()
		r.sendReport(rid, body)
		r.mu.Unlock()
		time.Sleep(wait)
	}
	LOG.Infof(r.ctx, "self test: sending synthetic mouse and keyboard reports")
	for _, d := range [][2]int8{{8, 0}, {0, 8}, {-8, 0}, {0, -8}} {
		for i := 0; i < 5; i++ {
			send(ridMouse, []byte{0, byte(d[0]), byte(d[1]), 0, 0}, 20*time.Millisecond)
		}
	}
	send(ridKeyboard, []byte{0x20, 0, 0, 0, 0, 0, 0, 0}, 50*time.Millisecond) // right shift down
	send(ridKeyboard, make([]byte, 8), 0)
}

// ---------------------------------------------------------------- input devices

func (r *Relay) discoverDevices() {
	for _, cand := range discoverInputPaths(r.explicit, r.only) {
		real, err := filepath.EvalSymlinks(cand.path)
		if err != nil {
			continue
		}
		r.mu.Lock()
		if _, ok := r.devices[real]; ok {
			r.mu.Unlock()
			continue
		}
		dev, err := openInputDev(cand.path, cand.forward)
		if err != nil {
			r.mu.Unlock()
			LOG.Warnf(r.ctx, "open %s: %v", cand.path, err)
			continue
		}
		r.devices[dev.real] = dev
		note := ""
		if !dev.forward {
			note = " (toggle key only, not forwarded)"
		}
		LOG.Infof(r.ctx, "input device %q <- %s%s", dev.name, cand.path, note)
		if r.forwarding && dev.forward {
			if err := dev.Grab(true); err != nil {
				LOG.Warnf(r.ctx, "grab %s: %v", dev.name, err)
			}
		}
		r.mu.Unlock()
		go r.inputLoop(dev)
	}
}

func (r *Relay) inputLoop(dev *InputDev) {
	buf := make([]byte, eventSize*64)
	for {
		events, err := dev.ReadEvents(buf)
		if err != nil {
			r.mu.Lock()
			if r.devices[dev.real] == dev {
				LOG.Infof(r.ctx, "input device removed %q", dev.name)
				delete(r.devices, dev.real)
				dev.Close()
				r.resyncKeys() // whatever it held will never be released
			}
			r.mu.Unlock()
			return
		}
		r.mu.Lock()
		r.handleEvents(dev, events)
		r.mu.Unlock()
	}
}

// handleEvents processes one batch read from dev; caller holds r.mu.
func (r *Relay) handleEvents(dev *InputDev, events []inputEvent) {
	for _, ev := range events {
		switch {
		case ev.Type == evSyn && ev.Code == synDropped:
			LOG.Warnf(r.ctx, "input events dropped on %q", dev.name)
			dev.dropped = true
		case dev.dropped:
			if ev.Type == evSyn && ev.Code == synReport {
				dev.dropped = false
				r.resyncKeys()
			}
		case dev.forward || (ev.Type == evKey && ev.Code == r.toggleCode):
			r.handleEvent(ev)
		}
	}
}

// readHeldKeys merges the key state of every forwarded device; caller holds r.mu.
func (r *Relay) readHeldKeys() keyBits {
	var all keyBits
	for _, d := range r.devices {
		if !d.forward {
			continue
		}
		k, err := d.Keys()
		if err != nil {
			LOG.Debugf(r.ctx, "read keys of %s: %v", d.name, err)
			continue
		}
		for i := range all {
			all[i] |= k[i]
		}
	}
	return all
}

// resyncKeys rebuilds the HID state from the kernel after events went missing; caller holds r.mu.
func (r *Relay) resyncKeys() {
	if !r.forwarding {
		return
	}
	held := r.heldKeys()
	r.resetHIDState()
	r.sendReport(ridConsumer, make([]byte, 2))
	for code := range uint16(keyCnt) {
		if held.held(code) && code != r.toggleCode {
			r.applyKey(code, true)
		}
	}
	r.kbDirty, r.mouseDirty = true, true
	r.flush()
}

// setForwarding switches input between the local desktop and the host; caller holds r.mu.
func (r *Relay) setForwarding(on bool, notifyHost bool) {
	wasPending := r.pendingOn
	r.pendingOn = false
	if on {
		if !r.connected() {
			LOG.Warnf(r.ctx, "not connected to a host, staying local")
			return
		}
		if r.heldKeys() != (keyBits{}) { // once grabbed, the desktop would never see their release
			r.pendingOn = true
			if !wasPending {
				LOG.Infof(r.ctx, "switching once the held keys are released (press %s to cancel)", r.toggleName)
			}
			return
		}
		for _, d := range r.devices {
			if !d.forward {
				continue
			}
			if err := d.Grab(true); err != nil {
				for _, d2 := range r.devices {
					_ = d2.Grab(false)
				}
				LOG.Warnf(r.ctx, "grab %s failed (%v), staying local", d.name, err)
				return
			}
		}
		r.resetHIDState()
		r.forwarding = true
		what := map[string]string{"keyboard": "keyboard", "mouse": "mouse"}[r.only]
		if what == "" {
			what = "keyboard and mouse"
		}
		LOG.Infof(r.ctx, ">>> %s switched to host %s (press %s to switch back)", what, r.peer, r.toggleName)
		r.sendKeepalive() // wake the link before the first real key press
		return
	}
	for _, d := range r.devices {
		_ = d.Grab(false)
	}
	r.forwarding = false
	r.resetHIDState()
	if notifyHost && r.connected() {
		r.sendReleaseAll()
	}
	LOG.Infof(r.ctx, "<<< input switched back to local")
}

func (r *Relay) resetHIDState() {
	r.kbMods, r.kbKeys, r.kbDirty = 0, r.kbKeys[:0], false
	r.btns, r.rel, r.mouseDirty = 0, [4]int32{}, false
}

// handleEvent translates one evdev event; caller holds r.mu.
func (r *Relay) handleEvent(ev inputEvent) {
	if ev.Type == evKey && ev.Code == r.toggleCode {
		if ev.Value != 0 { // switch on release so press/release pairs stay on one side
			return
		}
		if r.pendingOn {
			r.pendingOn = false
			LOG.Infof(r.ctx, "switch to host cancelled")
			return
		}
		r.setForwarding(!r.forwarding, true)
		return
	}
	if r.pendingOn && ev.Type == evKey && ev.Value == 0 {
		r.setForwarding(true, true)
		return
	}
	if !r.forwarding {
		return
	}
	switch ev.Type {
	case evKey:
		if ev.Value == 2 { // autorepeat is generated by the host
			return
		}
		r.applyKey(ev.Code, ev.Value != 0)
	case evRel:
		switch ev.Code {
		case relX:
			r.rel[0] += ev.Value
		case relY:
			r.rel[1] += ev.Value
		case relWheel:
			r.rel[2] += ev.Value
		case relHWheel:
			r.rel[3] += ev.Value
		default:
			return
		}
		r.mouseDirty = true
	case evSyn:
		if ev.Code == synReport {
			r.flush()
		}
	}
}

// applyKey updates the HID state for one key or button; consumer keys are sent right away.
func (r *Relay) applyKey(code uint16, down bool) {
	switch {
	case code >= btnLeft && code <= btnExtra:
		bit := byte(1) << (code - btnLeft)
		if down {
			r.btns |= bit
		} else {
			r.btns &^= bit
		}
		r.mouseDirty = true
	case linuxToConsumer[code] != 0:
		var usage uint16
		if down {
			usage = linuxToConsumer[code]
		}
		r.sendReport(ridConsumer, []byte{byte(usage), byte(usage >> 8)})
	case linuxToHID[code] != 0:
		u := linuxToHID[code]
		switch {
		case u >= 0xE0 && u <= 0xE7:
			bit := byte(1) << (u - 0xE0)
			if down {
				r.kbMods |= bit
			} else {
				r.kbMods &^= bit
			}
		case down:
			if !slices.Contains(r.kbKeys, u) && len(r.kbKeys) < 6 {
				r.kbKeys = append(r.kbKeys, u)
			}
		default:
			if i := slices.Index(r.kbKeys, u); i >= 0 {
				r.kbKeys = slices.Delete(r.kbKeys, i, i+1)
			}
		}
		r.kbDirty = true
	default:
		LOG.Debugf(r.ctx, "unmapped key code %d", code)
	}
}

func clamp8(v int32) int8 { return int8(max(-127, min(127, v))) }

// flush sends the pending keyboard/mouse state; large mouse deltas are split across reports.
func (r *Relay) flush() {
	if r.kbDirty {
		r.kbDirty = false
		body := make([]byte, 8)
		body[0] = r.kbMods
		copy(body[2:], r.kbKeys)
		r.sendReport(ridKeyboard, body)
	}
	if r.mouseDirty {
		r.mouseDirty = false
		for {
			x, y, w, h := clamp8(r.rel[0]), clamp8(r.rel[1]), clamp8(r.rel[2]), clamp8(r.rel[3])
			r.sendReport(ridMouse, []byte{r.btns, byte(x), byte(y), byte(w), byte(h)})
			r.rel[0] -= int32(x)
			r.rel[1] -= int32(y)
			r.rel[2] -= int32(w)
			r.rel[3] -= int32(h)
			if r.rel == [4]int32{} {
				break
			}
		}
	}
}
