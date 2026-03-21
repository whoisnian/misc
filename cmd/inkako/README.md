# inkako
Reverse-engineered from the inkako Android app (`com.zhishudongli.inkako`).  
Powered by GitHub Copilot Chat with `Claude Opus 4.6`.  

## example
```sh
mkdir -p ./output

# convert input to bin data and preview png from source image
go run ./cmd/inkako convert -o ./output/output.1 ./cmd/inkako/data/source.1.jpg

# convert and transfer image to device via bluetooth (disconnect device in android app before running the commands)
go run ./cmd/inkako scan # Found: NDB-8-2B7E (CC:E1:00:00:2B:7E) RSSI=-58
go run ./cmd/inkako transfer -mac "CC:E1:00:00:2B:7E" ./cmd/inkako/data/source.2.jpg
```

## usage
### convert
```
Usage: inkako convert [options] <input>

Options:
  -H int
        Screen height (default 428)
  -W int
        Screen width (default 428)
  -brightness int
        Brightness seekbar value (default 51)
  -contrast int
        Contrast seekbar value (default 30)
  -no-dither
        Disable Floyd-Steinberg dithering
  -o string
        Output base name (without extension)
  -saturation int
        Saturation seekbar value (default 23)
```
### scan
```
Usage: inkako scan [options]

Options:
  -t float
        Scan timeout in seconds (default 10)
```
### transfer
```
Usage: inkako transfer [options] <input>

Options:
  -H int
        Screen height (default 428)
  -W int
        Screen width (default 428)
  -brightness int
        Brightness seekbar value (default 51)
  -contrast int
        Contrast seekbar value (default 30)
  -image-index int
        Image slot index (1-based) (default 1)
  -mac string
        BLE device MAC address (required)
  -no-dither
        Disable Floyd-Steinberg dithering
  -saturation int
        Saturation seekbar value (default 23)
  -save string
        Also save .bin and .png output to this base name
  -screen-info int
        Screen type byte (default 8 for 428x428) (default 8)
```
