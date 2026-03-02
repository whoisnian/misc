package main

type EncodeFunc func(KeyCode) []byte

func EncodeForCH9329(ks KeyCode) []byte {
	cmd := [14]byte{0x57, 0xab, 0x00, 0x02, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0c}
	if ks == EmptyKeyCode {
		return cmd[:]
	}

	pos := 7
	for _, k := range ks {
		switch k {
		case K_L_CTRL:
			cmd[5] |= 0x01
		case K_L_SHIFT:
			cmd[5] |= 0x02
		case K_L_ALT:
			cmd[5] |= 0x04
		default:
			cmd[pos] = byte(k)
			cmd[13] += cmd[pos]
			pos++
		}
	}
	cmd[13] += cmd[5]
	return cmd[:]
}

func EncodeForKCOM3(ks KeyCode) []byte {
	cmd := [11]byte{0x57, 0xab, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	if ks == EmptyKeyCode {
		return cmd[:]
	}

	pos := 5
	for _, k := range ks {
		switch k {
		case K_L_CTRL:
			cmd[3] |= 0x01
		case K_L_SHIFT:
			cmd[3] |= 0x02
		case K_L_ALT:
			cmd[3] |= 0x04
		default:
			cmd[pos] = byte(k)
			pos++
		}
	}
	return cmd[:]
}
