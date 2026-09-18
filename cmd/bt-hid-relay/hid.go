package main

import (
	"bytes"
	"encoding/hex"
	"encoding/xml"
	"fmt"
)

const (
	hidUUID = "00001124-0000-1000-8000-00805f9b34fb"
	psmCtrl = 17 // HID control channel
	psmIntr = 19 // HID interrupt channel

	btSecurity       = 4 // SOL_BLUETOOTH socket option
	btSecurityMedium = 2 // encryption required, MITM not required

	// HIDP transport header: high nibble is the message type, low nibble the parameter
	msgHandshake   = 0x0
	msgHIDControl  = 0x1
	msgGetReport   = 0x4
	msgSetReport   = 0x5
	msgGetProtocol = 0x6
	msgSetProtocol = 0x7
	msgGetIdle     = 0x8
	msgSetIdle     = 0x9
	msgData        = 0xA

	hsSuccess            = 0
	hsErrInvalidReportID = 2
	hsErrUnsupported     = 3

	ctrlVirtualCableUnplug = 5

	reportInput = 1
	dataInput   = msgData<<4 | reportInput // 0xA1
	dataOther   = msgData << 4             // 0xA0

	ridKeyboard = 1
	ridMouse    = 2
	ridConsumer = 3
)

// HID Report Descriptor: keyboard (ID 1) + mouse with 5 buttons, wheel and horizontal wheel (ID 2) + consumer control (ID 3)
var reportDesc = []byte{
	// --- Keyboard, Report ID 1 ---
	0x05, 0x01, 0x09, 0x06, 0xA1, 0x01, 0x85, ridKeyboard,
	0x05, 0x07, 0x19, 0xE0, 0x29, 0xE7, 0x15, 0x00, 0x25, 0x01,
	0x75, 0x01, 0x95, 0x08, 0x81, 0x02, // 8 modifier bits
	0x95, 0x01, 0x75, 0x08, 0x81, 0x01, // reserved byte
	0x95, 0x05, 0x75, 0x01, 0x05, 0x08, 0x19, 0x01, 0x29, 0x05, 0x91, 0x02, // 5 LEDs
	0x95, 0x01, 0x75, 0x03, 0x91, 0x01, // LED padding
	0x95, 0x06, 0x75, 0x08, 0x15, 0x00, 0x26, 0xFF, 0x00,
	0x05, 0x07, 0x19, 0x00, 0x2A, 0xFF, 0x00, 0x81, 0x00, // 6-key rollover array
	0xC0,
	// --- Mouse, Report ID 2 ---
	0x05, 0x01, 0x09, 0x02, 0xA1, 0x01, 0x85, ridMouse, 0x09, 0x01, 0xA1, 0x00,
	0x05, 0x09, 0x19, 0x01, 0x29, 0x05, 0x15, 0x00, 0x25, 0x01,
	0x95, 0x05, 0x75, 0x01, 0x81, 0x02, // 5 button bits
	0x95, 0x01, 0x75, 0x03, 0x81, 0x01, // padding
	0x05, 0x01, 0x09, 0x30, 0x09, 0x31, 0x09, 0x38,
	0x15, 0x81, 0x25, 0x7F, 0x75, 0x08, 0x95, 0x03, 0x81, 0x06, // X, Y, Wheel (relative)
	0x05, 0x0C, 0x0A, 0x38, 0x02,
	0x15, 0x81, 0x25, 0x7F, 0x75, 0x08, 0x95, 0x01, 0x81, 0x06, // AC Pan (horizontal wheel)
	0xC0, 0xC0,
	// --- Consumer Control, Report ID 3 ---
	0x05, 0x0C, 0x09, 0x01, 0xA1, 0x01, 0x85, ridConsumer,
	0x15, 0x00, 0x26, 0xFF, 0x03, 0x19, 0x00, 0x2A, 0xFF, 0x03,
	0x75, 0x10, 0x95, 0x01, 0x81, 0x00, // one 16-bit usage
	0xC0,
}

// sdpRecord returns the HID Profile SDP record in BlueZ ServiceRecord XML format.
func sdpRecord(name string) string {
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(name))
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" ?>
<record>
  <attribute id="0x0001"><sequence><uuid value="0x1124"/></sequence></attribute>
  <attribute id="0x0004">
    <sequence>
      <sequence><uuid value="0x0100"/><uint16 value="0x0011"/></sequence>
      <sequence><uuid value="0x0011"/></sequence>
    </sequence>
  </attribute>
  <attribute id="0x0005"><sequence><uuid value="0x1002"/></sequence></attribute>
  <attribute id="0x0006">
    <sequence><uint16 value="0x656e"/><uint16 value="0x006a"/><uint16 value="0x0100"/></sequence>
  </attribute>
  <attribute id="0x0009">
    <sequence><sequence><uuid value="0x1124"/><uint16 value="0x0101"/></sequence></sequence>
  </attribute>
  <attribute id="0x000d">
    <sequence><sequence>
      <sequence><uuid value="0x0100"/><uint16 value="0x0013"/></sequence>
      <sequence><uuid value="0x0011"/></sequence>
    </sequence></sequence>
  </attribute>
  <attribute id="0x0100"><text value="%s"/></attribute>
  <attribute id="0x0101"><text value="Keyboard + Mouse relay"/></attribute>
  <attribute id="0x0102"><text value="Linux"/></attribute>
  <attribute id="0x0200"><uint16 value="0x0100"/></attribute>
  <attribute id="0x0201"><uint16 value="0x0111"/></attribute>
  <attribute id="0x0202"><uint8 value="0xC0"/></attribute>
  <attribute id="0x0203"><uint8 value="0x00"/></attribute>
  <attribute id="0x0204"><boolean value="true"/></attribute>
  <attribute id="0x0205"><boolean value="true"/></attribute>
  <attribute id="0x0206">
    <sequence><sequence>
      <uint8 value="0x22"/>
      <text encoding="hex" value="%s"/>
    </sequence></sequence>
  </attribute>
  <attribute id="0x0207">
    <sequence><sequence><uint16 value="0x0409"/><uint16 value="0x0100"/></sequence></sequence>
  </attribute>
  <attribute id="0x020b"><uint16 value="0x0100"/></attribute>
  <attribute id="0x020c"><uint16 value="0x0c80"/></attribute>
  <attribute id="0x020d"><boolean value="true"/></attribute>
  <attribute id="0x020e"><boolean value="false"/></attribute>
  <attribute id="0x020f"><uint16 value="0x0640"/></attribute>
  <attribute id="0x0210"><uint16 value="0x0320"/></attribute>
</record>
`, escaped.String(), hex.EncodeToString(reportDesc))
}

// linuxToHID maps linux input keycodes to USB HID usages; usages 0xE0..0xE7 become modifier bits.
var linuxToHID = map[uint16]byte{
	1: 0x29,                                                                                    // ESC
	2: 0x1E, 3: 0x1F, 4: 0x20, 5: 0x21, 6: 0x22, 7: 0x23, 8: 0x24, 9: 0x25, 10: 0x26, 11: 0x27, // 1..0
	12: 0x2D, 13: 0x2E, 14: 0x2A, 15: 0x2B, // - = Backspace Tab
	16: 0x14, 17: 0x1A, 18: 0x08, 19: 0x15, 20: 0x17, 21: 0x1C, 22: 0x18, 23: 0x0C, 24: 0x12, 25: 0x13, // Q..P
	26: 0x2F, 27: 0x30, 28: 0x28, 29: 0xE0, // [ ] Enter LCtrl
	30: 0x04, 31: 0x16, 32: 0x07, 33: 0x09, 34: 0x0A, 35: 0x0B, 36: 0x0D, 37: 0x0E, 38: 0x0F, // A..L
	39: 0x33, 40: 0x34, 41: 0x35, 42: 0xE1, 43: 0x31, // ; ' ` LShift \
	44: 0x1D, 45: 0x1B, 46: 0x06, 47: 0x19, 48: 0x05, 49: 0x11, 50: 0x10, // Z..M
	51: 0x36, 52: 0x37, 53: 0x38, 54: 0xE5, 55: 0x55, 56: 0xE2, 57: 0x2C, 58: 0x39, // , . / RShift KP* LAlt Space Caps
	59: 0x3A, 60: 0x3B, 61: 0x3C, 62: 0x3D, 63: 0x3E, 64: 0x3F, 65: 0x40, 66: 0x41, 67: 0x42, 68: 0x43, // F1..F10
	69: 0x53, 70: 0x47, // NumLock ScrollLock
	71: 0x5F, 72: 0x60, 73: 0x61, 74: 0x56, 75: 0x5C, 76: 0x5D, 77: 0x5E, 78: 0x57, // KP7 8 9 - 4 5 6 +
	79: 0x59, 80: 0x5A, 81: 0x5B, 82: 0x62, 83: 0x63, // KP1 2 3 0 .
	86: 0x64, 87: 0x44, 88: 0x45, // 102ND F11 F12
	89: 0x87, 92: 0x8A, 93: 0x88, 94: 0x8B, // RO HENKAN KATAKANAHIRAGANA MUHENKAN
	96: 0x58, 97: 0xE4, 98: 0x54, 99: 0x46, 100: 0xE6, // KPEnter RCtrl KP/ SysRq(PrtSc) RAlt
	102: 0x4A, 103: 0x52, 104: 0x4B, 105: 0x50, 106: 0x4F, 107: 0x4D, 108: 0x51, 109: 0x4E, // Home Up PgUp Left Right End Down PgDn
	110: 0x49, 111: 0x4C, // Insert Delete
	117: 0x67, 119: 0x48, 121: 0x85, 122: 0x90, 123: 0x91, 124: 0x89, // KP= Pause KP, Hangeul Hanja Yen
	125: 0xE3, 126: 0xE7, 127: 0x65, // LMeta RMeta Compose(Menu)
	183: 0x68, 184: 0x69, 185: 0x6A, 186: 0x6B, 187: 0x6C, 188: 0x6D,
	189: 0x6E, 190: 0x6F, 191: 0x70, 192: 0x71, 193: 0x72, 194: 0x73, // F13..F24
}

// linuxToConsumer maps media keys to Consumer Page usages.
var linuxToConsumer = map[uint16]uint16{
	113: 0x00E2, // MUTE
	114: 0x00EA, // VOLUMEDOWN
	115: 0x00E9, // VOLUMEUP
	140: 0x0192, // CALC
	155: 0x018A, // MAIL
	163: 0x00B5, // NEXTSONG
	164: 0x00CD, // PLAYPAUSE
	165: 0x00B6, // PREVIOUSSONG
	166: 0x00B7, // STOPCD
	172: 0x0223, // HOMEPAGE
}

// keyNames lists the key names accepted by -toggle.
var keyNames = map[string]uint16{
	"KEY_SCROLLLOCK": 70, "KEY_PAUSE": 119, "KEY_SYSRQ": 99, "KEY_COMPOSE": 127,
	"KEY_RIGHTCTRL": 97, "KEY_RIGHTALT": 100, "KEY_RIGHTMETA": 126,
	"KEY_F12": 88, "KEY_KPASTERISK": 55, "KEY_INSERT": 110,
}
