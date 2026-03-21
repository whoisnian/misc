# inkako.py
Reverse-engineered from the inkako Android app (`com.zhishudongli.inkako`).  
Powered by GitHub Copilot Chat with `Claude Opus 4.6`.  

## example
* install python dependencies and prepare directory
  ```sh
  uv sync && mkdir -p ../output
  ```
* extract image bin data and preview bmp from btsnoop_hci.log
  ```sh
  uv run python extract_hci.py ../data/btsnoop_hci.1.log --output ../output/extracted.1
  uv run python extract_hci.py ../data/btsnoop_hci.2.log --output ../output/extracted.2
  ```
* convert input to bin data and preview bmp from source image
  ```sh
  uv run python inkako.py convert ../data/source.1.jpg --output ../output/output.1
  uv run python inkako.py convert ../data/source.2.jpg --output ../output/output.2
  ```
* compare pixels of extracted bin data and converted bin data
  ```sh
  uv run python compare_bins.py ../output/extracted.1.bin ../output/output.1.bin
  uv run python compare_bins.py ../output/extracted.2.bin ../output/output.2.bin
  ```
* convert and transfer image to device via bluetooth (**disconnect device in android app before running the commands**)
  ```sh
  uv run python inkako.py scan # Found: NDB-8-2B7E (CC:E1:00:00:2B:7E) RSSI=-60
  uv run python inkako.py transfer ../data/source.1.jpg --mac "CC:E1:00:00:2B:7E"
  uv run python inkako.py transfer ../data/source.2.jpg --mac "CC:E1:00:00:2B:7E"
  ```

## prompts
### start reverse engineering
```md
This is the apk of an arm64-v8a android mobile app called "inkako" (粉墨), which is used to transfer images into an four-colors eink screen (resolution 428x428) via bluetooth.
Use JADX, radare2 or other tools to reverse engineer the apk and analyze the source code.
Find the image processing algorithm used to convert the images for the eink screen and the protocol used for the bluetooth communication.
Write a script that can take an image as input and convert it into the format required for the eink screen, then transfer it to the eink screen via bluetooth.
Image conversion should follow the same processing algorithm used in the android app, which may involve resizing, dithering, and color reduction to fit the eink screen's capabilities. The script should provide options to output converted images in bin and bmp formats for testing and verification purposes.
Additionally, document the image processing algorithm and the bluetooth communication protocol used by the app to transfer the processed images to the eink screen.

According to the json data from https://api.inkako.com/api/panel/query?model=nubia_notes, the device description is as follows:
{
  "code": 200,
  "msg": "",
  "data": {
    "id": 17,
    "model": "nubia_notes",
    "width": 616,
    "height": 718,
    "screen_x": 57,
    "screen_y": 103,
    "screen_width": 501,
    "screen_height": 500,
    "screen_pixel_width": null,
    "screen_pixel_height": null,
    "styles": [
      {
        "id": 58,
        "block_data": "",
        "model_id": 17,
        "name": "努比亚手机壳",
        "src": "https://inkako-1333407303.cos.ap-shanghai.myqcloud.com//uploads/20250826/db6fd0ea6bce14e8d75ce4ea30fb1823.png",
        "thumb": "https://inkako-1333407303.cos.ap-shanghai.myqcloud.com//uploads/20250826/db6fd0ea6bce14e8d75ce4ea30fb1823.png"
      }
    ]
  }
}
```
### extract actual bluetooth hci log
```md
`./data/btsnoop_hci.1.log` is the hci log from android transfering image of `./data/source.1.jpg`.
Write a new script to extract image data from the log, and save image as bin and bmp.
Update `./inkako.py` and `./PROTOCOL.md` to match the actual hci log data for the bluetooth communication protocol.
```
### compare bin data and fix the script
```md
Write a new script to calculate pixels similarity of two image bin data, like `./data/extracted.1.bin` and `./output/output.1.bin`.
```
```md
The arm64-v8a Android app provides adjustments like brightness, contrast and saturation to the image in `ImageBean.java`.
Diff `./data/extracted.1.bin` (extract from android hci log) with `./output/output.1.bin` (the image data output of the python script), and fix the python script to output as close as possible to the extracted.bin.
```
### optimize the python script
```md
The current python script is too slow in image processing. Optimize the python script to accelerate the image processing.
```
### translate the python script into Golang
```md
Based on the `PROTOCOL.md` and existing `inkako.py`, translate the python script into Golang for better performance and easier deployment.
```

## about image similarity
The `compare_bins.py` script calculates the image similarity pixel by pixel, which maybe quite different from what the human eye can directly perceive. For example:  
```sh
uv run python inkako.py convert ../data/source.1.jpg --output ../output/test.adjustments.default
uv run python inkako.py convert ../data/source.1.jpg --output ../output/test.adjustments.42-42-30 --brightness 42 --contrast 42 --saturation 30

uv run python compare_bins.py ../output/extracted.1.bin ../output/test.adjustments.default.bin  # pixels matching: 62.90%
uv run python compare_bins.py ../output/extracted.1.bin ../output/test.adjustments.42-42-30.bin # pixels matching: 66.63%
```
The `test.adjustments.42-42-30` has a higher pixel similarity, but the `test.adjustments.default` looks much closer to the extracted image.  

With the help of GitHub Copilot, the common image similarity metrics results are as follows:
| metrics          | default adjustments | 42-42-30 adjustments | range      | better |
| ---------------- | ------------------- | -------------------- | ---------- | ------ |
| compare_bins.py  | 62.90%              | 66.63%               | [0%, 100%] | higher |
| MSE              | 5068.6356           | 4418.9454            | [0, 65025] | lower  |
| PSNR (dB)        | 11.0819             | 11.6776              | [0, inf]   | higher |
| SSIM             | 0.6629              | 0.6749               | [-1, 1]    | higher |
| Hist Correlation | 1.0000              | 0.9932               | [-1, 1]    | higher |
| pHash Distance   | 0                   | 4                    | [0, 64]    | lower  |
| pHash Similarity | 1.0000              | 0.9375               | [0, 1]     | higher |
| LPIPS            | 0.0870              | 0.3313               | [0, 1]     | lower  |

Among these metrics, Histogram Correlation and pHash Similarity are a little bit closer to human visual perception. And the LPIPS metrics is best for measuring perceptual similarity, it is also more computationally expensive than other metrics because it requires PyTorch and a pre-trained neural network model.
