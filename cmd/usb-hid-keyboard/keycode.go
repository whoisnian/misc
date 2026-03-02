//go:generate stringer -type=Key -trimprefix=K_ -output=key_name.go
package main

type Key byte

// HID Key Definition
// https://www.usb.org/sites/default/files/hut1_22.pdf#chapter.10
const (
	K_A Key = iota + 0x04
	K_B
	K_C
	K_D
	K_E
	K_F
	K_G
	K_H
	K_I
	K_J
	K_K
	K_L
	K_M
	K_N
	K_O
	K_P
	K_Q
	K_R
	K_S
	K_T
	K_U
	K_V
	K_W
	K_X
	K_Y
	K_Z

	K_1
	K_2
	K_3
	K_4
	K_5
	K_6
	K_7
	K_8
	K_9
	K_0

	K_ENTER
	K_ESC
	K_BACKSPACE
	K_TAB
	K_SPACE
	K_MINUS      // -
	K_EQUAL      // =
	K_LEFTBRACE  // [
	K_RIGHTBRACE // ]
	K_BACKSLASH  // \
	_
	K_SEMICOLON  // ;
	K_APOSTROPHE // '
	K_GRAVE      // `
	K_COMMA      // ,
	K_DOT        // .
	K_SLASH      // /
	K_CAPSLOCK

	K_F1
	K_F2
	K_F3
	K_F4
	K_F5
	K_F6
	K_F7
	K_F8
	K_F9
	K_F10
	K_F11
	K_F12

	K_PRINTSCREEN
	K_SCROLLLOCK
	K_PAUSE
	K_INSERT
	K_HOME
	K_PAGEUP
	K_DELETE
	K_END
	K_PAGEDOWN
	K_RIGHT
	K_LEFT
	K_DOWN
	K_UP

	K_L_CTRL Key = iota + 0x91
	K_L_SHIFT
	K_L_ALT
	K_L_GUI
	K_R_CTRL
	K_R_SHIFT
	K_R_ALT
	K_R_GUI
)

type KeyCode [4]Key

var EmptyKeyCode KeyCode = KeyCode{}

func (ks KeyCode) String() string {
	if ks == EmptyKeyCode {
		return "NONE"
	} else if ks[1] == 0 && ks[2] == 0 && ks[3] == 0 {
		return ks[0].String()
	} else if ks[2] == 0 && ks[3] == 0 {
		return ks[0].String() + " + " + ks[1].String()
	} else if ks[3] == 0 {
		return ks[0].String() + " + " + ks[1].String() + " + " + ks[2].String()
	} else {
		return ks[0].String() + " + " + ks[1].String() + " + " + ks[2].String() + " + " + ks[3].String()
	}
}
