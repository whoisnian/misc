package main

// checksum computes the BLE packet checksum: sum(data) & 0xFF.
func checksum(data []byte) byte {
	var s uint32
	for _, b := range data {
		s += uint32(b)
	}
	return byte(s & 0xFF)
}

// addChecksum appends a checksum byte to data.
func addChecksum(data []byte) []byte {
	return append(data, checksum(data))
}

// makeStartCommand builds the start-image-push command (0x6A 0x01 0x08).
//
//	[0x6A, 0x01, 0x08, 0x01, 0x04, imageIndex, screenInfo,
//	 len_b3, len_b2, len_b1, len_b0, checksum]  — 12 bytes
func makeStartCommand(imageIndex, screenInfo uint8, dataLength int) []byte {
	cmd := []byte{
		0x6A, 0x01, 0x08, 0x01, 0x04,
		imageIndex,
		screenInfo,
		byte(dataLength >> 24),
		byte(dataLength >> 16),
		byte(dataLength >> 8),
		byte(dataLength),
	}
	return addChecksum(cmd)
}

// makeEndCommand builds the end-image-push command (0x6A 0x01 0x06).
//
//	[0x6A, 0x01, 0x06, 0x00, 0x04, imageIndex, screenInfo, 0x00, 0x00, checksum]  — 10 bytes
func makeEndCommand(imageIndex, screenInfo uint8) []byte {
	cmd := []byte{
		0x6A, 0x01, 0x06, 0x00, 0x04,
		imageIndex,
		screenInfo,
		0x00, 0x00,
	}
	return addChecksum(cmd)
}

// makeCleanFlashCommand builds the clean-flash command.
//
//	[0x6A, 0x16, 0x01, 0x01, checksum]
func makeCleanFlashCommand() []byte {
	return addChecksum([]byte{0x6A, 0x16, 0x01, 0x01})
}

// makeUpdateImageCommand builds the display-refresh command (0x6A 0x14 0x10).
//
//	[0x6A, 0x14, 0x10, 0x01, 0x00×17, checksum]  — 22 bytes
func makeUpdateImageCommand() []byte {
	cmd := make([]byte, 21)
	cmd[0] = 0x6A
	cmd[1] = 0x14
	cmd[2] = 0x10
	cmd[3] = 0x01
	// bytes 4–20 are zero
	return addChecksum(cmd)
}

// makeRowPacket builds an image-data row packet with 0xAA framing.
//
//	[0xAA, imageIndex, rowHi, rowLo, size, data..., checksum]
func makeRowPacket(imageIndex uint8, rowNum int, rowData []byte) []byte {
	frame := []byte{
		0xAA,
		imageIndex,
		byte(rowNum >> 8),
		byte(rowNum),
		byte(len(rowData)),
	}
	frame = append(frame, rowData...)
	return append(frame, checksum(frame))
}

// makeEndOfDataMarker builds the end-of-data marker (no checksum).
//
//	[0xAA, imageIndex, 0xFF, 0xFF, 0x00]
func makeEndOfDataMarker(imageIndex uint8) []byte {
	return []byte{0xAA, imageIndex, 0xFF, 0xFF, 0x00}
}

// makeImageUpdateTimeCommand sets the carousel auto-refresh interval.
//
//	[0x6A, 0x17, 0x02, intervalHi, intervalLo, checksum]
//
// intervalSeconds=0 disables auto-update. 0xFFFF queries current value.
func makeImageUpdateTimeCommand(intervalSeconds int) []byte {
	cmd := []byte{
		0x6A, 0x17, 0x02,
		byte(intervalSeconds >> 8),
		byte(intervalSeconds),
	}
	return addChecksum(cmd)
}

// makeGetVersionCommand requests the firmware version.
//
//	[0x6A, 0x11, 0x03, 0x00, 0x00, 0x00, checksum]
func makeGetVersionCommand() []byte {
	return addChecksum([]byte{0x6A, 0x11, 0x03, 0x00, 0x00, 0x00})
}

// makeDevInfoCommand requests device info.
//
//	[0x6A, 0x15, 0x02, 0x00, 0x00, checksum]
func makeDevInfoCommand() []byte {
	return addChecksum([]byte{0x6A, 0x15, 0x02, 0x00, 0x00})
}
