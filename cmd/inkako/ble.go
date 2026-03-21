package main

import (
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

// BLE UUIDs — parsed at init time; values are static constants so errors are fatal.
var (
	serviceUUID = mustParseUUID("79223401-1a11-21e1-8300-0940a1146603")
	writeUUID   = mustParseUUID("79223402-1a11-21e1-8300-0940a1146603")
	notifyUUID  = mustParseUUID("79223403-1a11-21e1-8300-0940a1146603")
)

func mustParseUUID(s string) bluetooth.UUID {
	u, err := bluetooth.ParseUUID(s)
	if err != nil {
		panic("invalid UUID: " + s + ": " + err.Error())
	}
	return u
}

// ScanDevice holds information about a discovered BLE device.
type ScanDevice struct {
	Address string
	Name    string
	RSSI    int16
}

// ScanDevices scans for BLE devices for the given duration.
func ScanDevices(timeout time.Duration) ([]ScanDevice, error) {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return nil, fmt.Errorf("enable adapter: %w", err)
	}

	var mu sync.Mutex
	seen := map[string]ScanDevice{}

	done := make(chan struct{})
	time.AfterFunc(timeout, func() {
		adapter.StopScan()
		close(done)
	})

	err := adapter.Scan(func(a *bluetooth.Adapter, result bluetooth.ScanResult) {
		name := result.LocalName()
		addr := result.Address.String()
		mu.Lock()
		seen[addr] = ScanDevice{Address: addr, Name: name, RSSI: result.RSSI}
		mu.Unlock()
	})
	<-done

	if err != nil && err.Error() != "bluetooth: scan stopped" {
		// StopScan causes Scan to return an error; ignore known stop errors
		_ = err
	}

	mu.Lock()
	defer mu.Unlock()
	devices := make([]ScanDevice, 0, len(seen))
	for _, d := range seen {
		devices = append(devices, d)
	}
	return devices, nil
}

// TransferOptions holds parameters for an image transfer operation.
type TransferOptions struct {
	MacAddress string
	ImageData  []byte
	ScreenInfo uint8
	ImageIndex uint8 // 1-based
	Width      int
	Verbose    bool
}

const maxRetries = 3

// scanForDevice does a BLE scan until the device with the given MAC address is
// discovered (case-insensitive) or the timeout elapses.  This is required on
// Linux/BlueZ so that a D-Bus device object exists before we call Connect.
func scanForDevice(adapter *bluetooth.Adapter, mac string, timeout time.Duration) error {
	found := make(chan struct{}, 1)

	go func() {
		time.Sleep(timeout)
		adapter.StopScan()
	}()

	err := adapter.Scan(func(a *bluetooth.Adapter, result bluetooth.ScanResult) {
		addr := result.Address.String()
		if strings.EqualFold(addr, mac) {
			select {
			case found <- struct{}{}:
			default:
			}
			adapter.StopScan()
		}
	})

	select {
	case <-found:
		return nil
	default:
		if err != nil {
			return fmt.Errorf("device %s not found within timeout", mac)
		}
		return fmt.Errorf("device %s not found within timeout", mac)
	}
}

// TransferImage connects to the device and uploads image data via BLE.
//
// Protocol sequence:
//  1. Connect, discover service + characteristics
//  2. Enable notifications
//  3. Clean flash, wait 50ms
//  4. Start command; wait for start ACK (0x6B 0x01 0x08)
//  5. Stream rows with 50ms delay; retry from row 0 on 0xAB error
//  6. End-of-data marker, wait 100ms
//  7. End command; wait for end ACK (0x6B 0x01 0x06)
//  8. Image-update-time (disable carousel); wait for echo
//  9. Refresh display
func TransferImage(opts TransferOptions) error {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return fmt.Errorf("enable adapter: %w", err)
	}

	// Parse MAC address (Set silently ignores parse errors; validate via ParseMAC)
	var targetAddr bluetooth.Address
	targetAddr.Set(opts.MacAddress)

	// Scan briefly so BlueZ discovers the device and creates a D-Bus object for
	// it; without this, adapter.Connect on Linux fails with a DBus Properties
	// error when the device is not already in the BlueZ cache.
	log.Printf("Scanning for %s...", opts.MacAddress)
	if err := scanForDevice(adapter, opts.MacAddress, 10*time.Second); err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	log.Printf("Connecting to %s...", opts.MacAddress)
	device, err := adapter.Connect(targetAddr, bluetooth.ConnectionParams{})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer device.Disconnect()

	log.Printf("Discovering services...")
	services, err := device.DiscoverServices([]bluetooth.UUID{serviceUUID})
	if err != nil || len(services) == 0 {
		return fmt.Errorf("discover service: %w", err)
	}
	svc := services[0]

	chars, err := svc.DiscoverCharacteristics([]bluetooth.UUID{writeUUID, notifyUUID})
	if err != nil || len(chars) < 2 {
		return fmt.Errorf("discover characteristics: %w", err)
	}

	var writeChar, notifyChar bluetooth.DeviceCharacteristic
	for _, c := range chars {
		switch c.UUID() {
		case writeUUID:
			writeChar = c
		case notifyUUID:
			notifyChar = c
		}
	}

	// Notification state
	var (
		startAck      = make(chan struct{}, 1)
		endAck        = make(chan struct{}, 1)
		updateTimeAck = make(chan struct{}, 1)
		transferErrCh = make(chan int, 1) // sends errored row number
		mu            sync.Mutex
		currentRow    int
	)

	err = notifyChar.EnableNotifications(func(buf []byte) {
		if len(buf) == 0 {
			return
		}
		b0, b1, b2 := buf[0], byte(0), byte(0)
		if len(buf) > 1 {
			b1 = buf[1]
		}
		if len(buf) > 2 {
			b2 = buf[2]
		}

		if b0 == 0xAB && len(buf) >= 6 && b2 != 0xFF && buf[5] == 0x00 {
			errRow := (int(buf[2]) << 8) | int(buf[3])
			log.Printf("Transfer error at row %d — will retry from row 0", errRow)
			mu.Lock()
			currentRow = 0
			mu.Unlock()
			select {
			case transferErrCh <- errRow:
			default:
			}
			return
		}
		if b0 == 0x6B {
			switch {
			case b1 == 0x01 && b2 == 0x08:
				log.Printf("Received start ACK")
				select {
				case startAck <- struct{}{}:
				default:
				}
			case b1 == 0x01 && b2 == 0x06:
				log.Printf("Received end ACK")
				select {
				case endAck <- struct{}{}:
				default:
				}
			case b1 == 0x17:
				select {
				case updateTimeAck <- struct{}{}:
				default:
				}
			case b1 == 0x01 && b2 == 0x01:
				log.Printf("Warning: device busy (display is refreshing)")
			}
		}
	})
	if err != nil {
		return fmt.Errorf("enable notifications: %w", err)
	}

	rowSize := opts.Width / 4
	totalRows := int(math.Ceil(float64(len(opts.ImageData)) / float64(rowSize)))

	// 1. Clean flash
	log.Printf("Sending clean flash...")
	if _, err := writeChar.WriteWithoutResponse(makeCleanFlashCommand()); err != nil {
		return fmt.Errorf("clean flash: %w", err)
	}
	time.Sleep(50 * time.Millisecond)

	// 2. Start command
	startCmd := makeStartCommand(opts.ImageIndex, opts.ScreenInfo, len(opts.ImageData))
	log.Printf("Sending start command (data size: %d bytes)...", len(opts.ImageData))
	if _, err := writeChar.WriteWithoutResponse(startCmd); err != nil {
		return fmt.Errorf("start command: %w", err)
	}

	// 3. Wait for start ACK
	log.Printf("Waiting for start ACK...")
	select {
	case <-startAck:
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timeout waiting for start ACK")
	}

	// 4. Stream rows
	log.Printf("Transferring image data (%d rows)...", totalRows)
	retryCount := 0

	for currentRow < totalRows {
		// Check for transfer error (non-blocking)
		select {
		case <-transferErrCh:
			retryCount++
			if retryCount > maxRetries {
				return fmt.Errorf("max retries (%d) exceeded", maxRetries)
			}
			log.Printf("Retrying from row 0 (attempt %d/%d)", retryCount, maxRetries)
			time.Sleep(2 * time.Second)
			mu.Lock()
			currentRow = 0
			mu.Unlock()
			continue
		default:
		}

		mu.Lock()
		row := currentRow
		mu.Unlock()

		offset := row * rowSize
		end := offset + rowSize
		if end > len(opts.ImageData) {
			end = len(opts.ImageData)
		}
		chunk := opts.ImageData[offset:end]
		packet := makeRowPacket(opts.ImageIndex, row+1, chunk)
		if _, err := writeChar.WriteWithoutResponse(packet); err != nil {
			return fmt.Errorf("send row %d: %w", row+1, err)
		}

		mu.Lock()
		currentRow++
		row = currentRow
		mu.Unlock()

		time.Sleep(50 * time.Millisecond)

		if row%50 == 0 || row == totalRows {
			log.Printf("  Progress: %.0f%% (%d/%d)",
				float64(row)/float64(totalRows)*100, row, totalRows)
		}
	}

	// 5. End-of-data marker
	if _, err := writeChar.WriteWithoutResponse(makeEndOfDataMarker(opts.ImageIndex)); err != nil {
		return fmt.Errorf("end-of-data marker: %w", err)
	}
	time.Sleep(100 * time.Millisecond)

	// 6. End command
	log.Printf("Sending end command...")
	endCmd := makeEndCommand(opts.ImageIndex, opts.ScreenInfo)
	if _, err := writeChar.WriteWithoutResponse(endCmd); err != nil {
		return fmt.Errorf("end command: %w", err)
	}
	select {
	case <-endAck:
	case <-time.After(30 * time.Second):
		return fmt.Errorf("timeout waiting for end ACK")
	}

	// 7. Disable carousel
	if _, err := writeChar.WriteWithoutResponse(makeImageUpdateTimeCommand(0)); err != nil {
		return fmt.Errorf("image update time: %w", err)
	}
	select {
	case <-updateTimeAck:
	case <-time.After(5 * time.Second):
		// Non-fatal: carry on
	}

	// 8. Trigger display refresh
	log.Printf("Sending display refresh command...")
	if _, err := writeChar.WriteWithoutResponse(makeUpdateImageCommand()); err != nil {
		return fmt.Errorf("update image: %w", err)
	}
	time.Sleep(2 * time.Second)

	log.Printf("Transfer complete!")
	return nil
}
