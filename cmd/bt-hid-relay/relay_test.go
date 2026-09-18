//go:build linux

package main

import (
	"context"
	"encoding/hex"
	"os"
	"strings"
	"testing"

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
	if _, err := parseBDAddr("78:2b:46:4e:4d"); err == nil {
		t.Error("expected error for short address")
	}
	raw := [6]uint8{0x7c, 0x4d, 0x4e, 0x46, 0x2b, 0x78}
	if bdaddrFromRaw(raw) != a {
		t.Errorf("bdaddrFromRaw: %v", bdaddrFromRaw(raw))
	}
}
