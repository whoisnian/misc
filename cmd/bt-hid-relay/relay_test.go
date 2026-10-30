//go:build linux

package main

import (
	"context"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/whoisnian/glb/logger"
)

// fakeIntr collects everything written to the interrupt channel.
func newTestRelay(t *testing.T) (*Relay, *os.File) {
	t.Helper()
	LOG = logger.New(logger.NewNanoHandler(os.Stderr, logger.Options{Level: logger.LevelWarn}))
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pr.Close(); pw.Close() })
	r := NewRelay(context.Background(), &bluez{}, RelayOptions{ToggleCode: 70, ToggleName: "KEY_SCROLLLOCK"})
	r.ctrl = &l2capConn{f: pw}
	r.intr = &l2capConn{f: pw}
	return r, pr
}

func readPackets(t *testing.T, pr *os.File, n int) []string {
	t.Helper()
	buf := make([]byte, 4096)
	var out []string
	got := 0
	for got < n {
		k, err := pr.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		// packets are concatenated in the pipe; split on the 0xA1 header using known report sizes
		for p := buf[:k]; len(p) > 0; {
			size := map[byte]int{ridKeyboard: 10, ridMouse: 7, ridConsumer: 4}[p[1]]
			out = append(out, hex.EncodeToString(p[:size]))
			p = p[size:]
			got++
		}
	}
	return out
}

func TestEventTranslation(t *testing.T) {
	r, pr := newTestRelay(t)
	r.mu.Lock()
	r.setForwarding(true, true)
	if !r.forwarding {
		t.Fatal("expected forwarding on")
	}
	// LShift + A, then release
	r.handleEvent(inputEvent{evKey, 42, 1})
	r.handleEvent(inputEvent{evKey, 30, 1})
	r.handleEvent(inputEvent{evSyn, synReport, 0})
	r.handleEvent(inputEvent{evKey, 30, 0})
	r.handleEvent(inputEvent{evKey, 42, 0})
	r.handleEvent(inputEvent{evSyn, synReport, 0})
	// mouse: x=300 must be split, y=-5, left button
	r.handleEvent(inputEvent{evRel, relX, 300})
	r.handleEvent(inputEvent{evRel, relY, -5})
	r.handleEvent(inputEvent{evKey, btnLeft, 1})
	r.handleEvent(inputEvent{evSyn, synReport, 0})
	// volume up press/release
	r.handleEvent(inputEvent{evKey, 115, 1})
	r.handleEvent(inputEvent{evKey, 115, 0})
	// toggle key release switches back and releases everything on the host
	r.handleEvent(inputEvent{evKey, 70, 1})
	r.handleEvent(inputEvent{evKey, 70, 0})
	r.mu.Unlock()

	want := []string{
		"a1010000000000000000", // wake report after switching
		"a1010200040000000000", // mods=LShift keys=[A]
		"a1010000000000000000",
		"a102017ffb0000", // btn=1 x=127 y=-5
		"a102017f000000", // x=127
		"a102012e000000", // x=46
		"a103e900",       // VolUp
		"a1030000",
		"a1010000000000000000", // release all
		"a1020000000000",
		"a1030000",
	}
	got := readPackets(t, pr, len(want))
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("packets mismatch\n got: %v\nwant: %v", got, want)
	}
	if r.forwarding {
		t.Error("expected forwarding off after toggle")
	}
}

func heldBits(codes ...uint16) keyBits {
	var k keyBits
	for _, c := range codes {
		k[c/8] |= 1 << (c % 8)
	}
	return k
}

func TestSwitchWaitsForHeldKeys(t *testing.T) {
	r, pr := newTestRelay(t)
	held := heldBits(29, 272) // LCtrl and left button still down locally
	r.heldKeys = func() keyBits { return held }
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handleEvent(inputEvent{evKey, 70, 0})
	if r.forwarding || !r.pendingOn {
		t.Fatalf("forwarding=%v pendingOn=%v, want the switch to be pending", r.forwarding, r.pendingOn)
	}
	held = heldBits(272)
	r.handleEvent(inputEvent{evKey, 29, 0})
	if r.forwarding || !r.pendingOn {
		t.Fatal("switched while the button is still held")
	}
	held = keyBits{}
	r.handleEvent(inputEvent{evKey, btnLeft, 0})
	if !r.forwarding || r.pendingOn {
		t.Fatalf("forwarding=%v pendingOn=%v, want switched", r.forwarding, r.pendingOn)
	}
	if got := readPackets(t, pr, 1); got[0] != "a1010000000000000000" {
		t.Errorf("got %v, the release that completed the switch must not reach the host", got)
	}

	// the toggle key cancels a pending switch
	r.setForwarding(false, false)
	held = heldBits(29)
	r.handleEvent(inputEvent{evKey, 70, 0})
	r.handleEvent(inputEvent{evKey, 70, 0})
	held = keyBits{}
	r.handleEvent(inputEvent{evKey, 29, 0})
	if r.forwarding || r.pendingOn {
		t.Fatalf("forwarding=%v pendingOn=%v, want cancelled", r.forwarding, r.pendingOn)
	}
}

func TestSynDroppedResync(t *testing.T) {
	r, pr := newTestRelay(t)
	dev := &InputDev{forward: true}
	var held keyBits
	r.heldKeys = func() keyBits { return held }
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setForwarding(true, true)
	r.handleEvents(dev, []inputEvent{{evKey, 30, 1}, {evKey, 48, 1}, {evSyn, synReport, 0}})
	// the release of A is lost, B stays held and the events up to SYN_REPORT are unreliable
	held = heldBits(48, 42)
	r.handleEvents(dev, []inputEvent{{evSyn, synDropped, 0}, {evKey, 31, 1}, {evSyn, synReport, 0}})
	if dev.dropped {
		t.Error("expected the drop to be cleared by SYN_REPORT")
	}
	want := []string{
		"a1010000000000000000", // wake report
		"a1010000040500000000", // A + B
		"a1030000",             // consumer cleared
		"a1010200050000000000", // LShift + B from the kernel state
		"a1020000000000",
	}
	if got := readPackets(t, pr, len(want)); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("packets mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestKeepaliveRepeatsKeyboardState(t *testing.T) {
	r, pr := newTestRelay(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sendKeepalive() // not forwarding, must stay silent
	r.setForwarding(true, true)
	r.handleEvent(inputEvent{evKey, 30, 1})
	r.handleEvent(inputEvent{evSyn, synReport, 0})
	r.sendKeepalive()
	want := []string{"a1010000000000000000", "a1010000040000000000", "a1010000040000000000"}
	if got := readPackets(t, pr, len(want)); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("packets mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestStalledLinkDropsConnection(t *testing.T) {
	r, _ := newTestRelay(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dialing = true // keep the reconnect timer from firing
	r.setForwarding(true, true)
	start := time.Now()
	for r.connected() { // nothing drains the pipe, so it fills up and the write times out
		r.sendReport(ridKeyboard, make([]byte, 8))
	}
	if d := time.Since(start); d > 2*writeTimeout {
		t.Errorf("took %s to give up", d)
	}
	if r.forwarding {
		t.Error("expected input to be handed back to the local desktop")
	}
}

func TestRefuseInbound(t *testing.T) {
	r, _ := newTestRelay(t)
	a, b := bdaddr{1}, bdaddr{2}
	r.peer = a
	for _, tc := range []struct {
		name    string
		hosts   []bdaddr
		peer    bdaddr
		psm     uint16
		refused bool
	}{
		{"same host reconnects", nil, a, psmCtrl, false},
		{"other device cannot take over", nil, b, psmCtrl, true},
		{"interrupt from other device", nil, b, psmIntr, true},
		{"not the configured host", []bdaddr{a}, b, psmCtrl, true},
	} {
		r.hosts = tc.hosts
		if why := r.refuse(tc.peer, tc.psm); (why != "") != tc.refused {
			t.Errorf("%s: refuse = %q", tc.name, why)
		}
	}
	r.hosts = nil
	r.intr = nil // half-open session, nothing to protect yet
	if why := r.refuse(b, psmCtrl); why != "" {
		t.Errorf("half-open session: refuse = %q", why)
	}
}

func TestAttachReplacesInterrupt(t *testing.T) {
	r, _ := newTestRelay(t)
	pipe := func() *l2capConn {
		pr, pw, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pr.Close(); pw.Close() })
		return &l2capConn{f: pr}
	}
	old, repl := pipe(), pipe()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.intr = old
	r.attach(repl, psmIntr)
	if r.intr != repl {
		t.Error("interrupt channel not replaced")
	}
	if err := old.f.SetDeadline(time.Now()); err == nil {
		t.Error("old channel still open")
	}
}

func TestSDPRecordEscapesName(t *testing.T) {
	rec := sdpRecord(`a<b>&"c"`)
	if !strings.Contains(rec, `<text value="a&lt;b&gt;&amp;&#34;c&#34;"/>`) {
		t.Errorf("name not escaped: %s", rec)
	}
	if !strings.Contains(rec, hex.EncodeToString(reportDesc)) {
		t.Error("descriptor missing")
	}
}

func TestParseBDAddr(t *testing.T) {
	a, err := parseBDAddr("78:2b:46:4e:4d:7c")
	if err != nil || a.String() != "78:2B:46:4E:4D:7C" {
		t.Errorf("got %v %v", a, err)
	}
	for _, s := range []string{"78:2b:46:4e:4d", "78:2G:46:4E:4D:7C", "78: 2:46:4E:4D:7C", "78:+2:46:4E:4D:7C"} {
		if _, err := parseBDAddr(s); err == nil {
			t.Errorf("expected error for %q", s)
		}
	}
	raw := [6]uint8{0x7c, 0x4d, 0x4e, 0x46, 0x2b, 0x78}
	if bdaddrFromRaw(raw) != a {
		t.Errorf("bdaddrFromRaw: %v", bdaddrFromRaw(raw))
	}
}
