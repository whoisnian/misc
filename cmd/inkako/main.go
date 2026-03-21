package main

import (
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// screenTypes maps screenInfo byte → (width, height)
var screenTypes = map[int][2]int{
	1: {200, 200},
	2: {200, 200},
	3: {168, 384},
	4: {200, 300},
	5: {528, 768},
	6: {192, 176},
	7: {124, 250},
	8: {428, 428},
}

func usage() {
	fmt.Fprintf(os.Stderr, `inkako — 4-color e-ink image converter and BLE uploader

Usage:
  inkako [global flags] <command> [options]

Commands:
  convert   Convert image to 4-color e-ink format (.bin + .bmp preview)
  transfer  Convert and upload image to device via BLE
  scan      Scan for BLE devices

Global flags:
  -v        Verbose logging

Run 'inkako <command> -h' for command-specific options.
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	verbose := false
	// Allow -v before or after the subcommand
	args := os.Args[1:]
	filtered := args[:0]
	for _, a := range args {
		if a == "-v" || a == "--verbose" {
			verbose = true
		} else {
			filtered = append(filtered, a)
		}
	}

	if verbose {
		log.SetFlags(log.Ltime)
	} else {
		log.SetFlags(log.Ltime)
	}

	if len(filtered) == 0 {
		usage()
		os.Exit(1)
	}

	switch filtered[0] {
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	case "convert":
		runConvert(filtered[1:], verbose)
	case "transfer":
		runTransfer(filtered[1:], verbose)
	case "scan":
		runScan(filtered[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", filtered[0])
		usage()
		os.Exit(1)
	}
}

// ── convert command ─────────────────────────────────────────────────────────

func runConvert(args []string, verbose bool) {
	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	output := fs.String("o", "", "Output base name (without extension)")
	width := fs.Int("W", 428, "Screen width")
	height := fs.Int("H", 428, "Screen height")
	noDither := fs.Bool("no-dither", false, "Disable Floyd-Steinberg dithering")
	brightness := fs.Int("brightness", 51, "Brightness seekbar value")
	contrast := fs.Int("contrast", 30, "Contrast seekbar value")
	saturation := fs.Int("saturation", 23, "Saturation seekbar value")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: inkako convert [options] <input>\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}
	if fs.NArg() < 1 {
		fs.Usage()
		os.Exit(1)
	}
	input := fs.Arg(0)

	params := colorAdjustParams{
		Brightness: *brightness,
		Contrast:   *contrast,
		Saturation: *saturation,
	}

	if verbose {
		log.Printf("Loading image: %s", input)
		log.Printf("Target size: %dx%d", *width, *height)
	}

	result, err := convertImage(input, *width, *height, !*noDither, params)
	if err != nil {
		log.Fatalf("Conversion failed: %v", err)
	}

	outBase := *output
	if outBase == "" {
		outBase = strings.TrimSuffix(filepath.Base(input), filepath.Ext(input)) + "_eink"
	}

	// Save .bin
	binPath := outBase + ".bin"
	if err := os.WriteFile(binPath, result.Packed, 0644); err != nil {
		log.Fatalf("Write bin: %v", err)
	}
	log.Printf("Binary data saved to %s (%d bytes)", binPath, len(result.Packed))

	// Save .png preview
	pngPath := outBase + ".png"
	preview := indicesToRGBA(result.Indices)
	f, err := os.Create(pngPath)
	if err != nil {
		log.Fatalf("Create preview PNG: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, preview); err != nil {
		log.Fatalf("Encode preview PNG: %v", err)
	}
	log.Printf("Preview PNG saved to %s", pngPath)

	printColorDistribution(result.Indices)
	log.Printf("Conversion complete.")
}

// ── transfer command ─────────────────────────────────────────────────────────

func runTransfer(args []string, verbose bool) {
	fs := flag.NewFlagSet("transfer", flag.ExitOnError)
	mac := fs.String("mac", "", "BLE device MAC address (required)")
	width := fs.Int("W", 428, "Screen width")
	height := fs.Int("H", 428, "Screen height")
	noDither := fs.Bool("no-dither", false, "Disable Floyd-Steinberg dithering")
	brightness := fs.Int("brightness", 51, "Brightness seekbar value")
	contrast := fs.Int("contrast", 30, "Contrast seekbar value")
	saturation := fs.Int("saturation", 23, "Saturation seekbar value")
	screenInfo := fs.Int("screen-info", 8, "Screen type byte (default 8 for 428x428)")
	imageIndex := fs.Int("image-index", 1, "Image slot index (1-based)")
	save := fs.String("save", "", "Also save .bin and .png output to this base name")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: inkako transfer [options] <input>\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}
	if fs.NArg() < 1 {
		fs.Usage()
		os.Exit(1)
	}
	if *mac == "" {
		fmt.Fprintln(os.Stderr, "error: --mac is required")
		fs.Usage()
		os.Exit(1)
	}
	input := fs.Arg(0)

	params := colorAdjustParams{
		Brightness: *brightness,
		Contrast:   *contrast,
		Saturation: *saturation,
	}

	result, err := convertImage(input, *width, *height, !*noDither, params)
	if err != nil {
		log.Fatalf("Conversion failed: %v", err)
	}

	if *save != "" {
		if err := os.WriteFile(*save+".bin", result.Packed, 0644); err != nil {
			log.Fatalf("Write bin: %v", err)
		}
		preview := indicesToRGBA(result.Indices)
		f, err := os.Create(*save + ".png")
		if err != nil {
			log.Fatalf("Create preview PNG: %v", err)
		}
		if err := png.Encode(f, preview); err != nil {
			f.Close()
			log.Fatalf("Encode preview PNG: %v", err)
		}
		f.Close()
		log.Printf("Saved .bin and .png to %s.*", *save)
	}

	printColorDistribution(result.Indices)

	opts := TransferOptions{
		MacAddress: *mac,
		ImageData:  result.Packed,
		ScreenInfo: uint8(*screenInfo),
		ImageIndex: uint8(*imageIndex),
		Width:      *width,
		Verbose:    verbose,
	}
	if err := TransferImage(opts); err != nil {
		log.Fatalf("Transfer failed: %v", err)
	}
}

// ── scan command ─────────────────────────────────────────────────────────────

func runScan(args []string) {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	timeout := fs.Float64("t", 10.0, "Scan timeout in seconds")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: inkako scan [options]\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	dur := time.Duration(*timeout * float64(time.Second))
	log.Printf("Scanning for BLE devices (%.0fs)...", *timeout)

	devices, err := ScanDevices(dur)
	if err != nil {
		log.Fatalf("Scan failed: %v", err)
	}

	var named []ScanDevice
	for _, d := range devices {
		if d.Name != "" {
			named = append(named, d)
		}
	}

	if len(named) == 0 {
		log.Printf("No named devices found. All discovered addresses:")
		for _, d := range devices {
			log.Printf("  %s (name=N/A, RSSI=%d)", d.Address, d.RSSI)
		}
	} else {
		for _, d := range named {
			log.Printf("  Found: %s (%s) RSSI=%d", d.Name, d.Address, d.RSSI)
		}
	}
}
