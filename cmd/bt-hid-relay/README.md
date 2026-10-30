# bt-hid-relay
Turn a Linux box into a Bluetooth keyboard and mouse for another machine (e.g. a headless Windows host),
so the physical keyboard and mouse can be shared with a hotkey instead of re-plugging them.

The host needs nothing installed: it sees an ordinary Bluetooth HID combo device, so the input also works
on the lock screen, the login screen and in some firmware setup screens.

Nothing under `/etc` is modified and no systemd unit is enabled. The tool spawns its own `bluetoothd`
(`-P input -f <tempdir>/main.conf`) because the stock input plugin listens on L2CAP PSM 17/19 as a HID
*host*, which conflicts with acting as a HID *device*. The system `bluetooth.service` must be stopped
while the tool runs, since both would claim `org.bluez`. Pairing keys still end up in
`/var/lib/bluetooth/` as usual; that is runtime state, not configuration.

## how it works
```
 keyboard/mouse ──USB──▶ kernel evdev (/dev/input/eventN)
                             │  read events; while switched to the host the devices are
                             │  EVIOCGRAB'ed so the local desktop no longer sees them
                             ▼
                  linux keycode → USB HID usage → HID input report
                             │
                             ▼
        L2CAP PSM 19 (interrupt channel, reports)  ┐
        L2CAP PSM 17 (control channel, handshakes)  ├─ Bluetooth Classic ──▶ host HID driver
        SDP record (published through bluetoothd)   ┘
```

* `org.bluez.ProfileManager1.RegisterProfile` is only used to publish the HID SDP record with the report
  descriptor (keyboard ID 1, mouse with 5 buttons + wheel + horizontal wheel ID 2, consumer control ID 3).
  The two L2CAP channels are opened by the tool itself with `BT_SECURITY_MEDIUM`.
* Control channel requests (`GET/SET_REPORT`, `GET/SET_PROTOCOL`, `SET_IDLE`) are answered per spec;
  `VIRTUAL_CABLE_UNPLUG` means the host removed the device.
* Reconnection works both ways: the host usually reconnects on its own (`HIDNormallyConnectable`), and the
  tool also dials every paired device (or `-host`) with a 3–30 s backoff (`HIDReconnectInitiate`).
* A `NoInputNoOutput` agent accepts pairing only while `-pair` is open; afterwards the adapter is
  connectable but neither discoverable nor pairable, and the agent rejects requests.
* A live session is never taken over by another device, and with `-host` every other device is refused.
* The hotkey acts on key *release* so a press/release pair never gets split across the two machines.
  Input is handed back to the local desktop automatically when the link drops or the tool exits.
  While a key or button is still held the switch to the host waits for its release, otherwise the local
  desktop would keep it pressed; press the hotkey again to cancel.
* While switched to the host the keyboard report is repeated every `-keepalive` (a no-op for the host)
  so the USB adapter does not autosuspend and the link stays out of sniff mode. Without it the first
  key presses after an idle moment can get lost.

## example
```sh
# first time: open a 120 s pairing window, then add "Linux HID Relay" on the host
sudo go run ./cmd/bt-hid-relay -pair 120

# daily use: reconnect to whatever is paired, ScrollLock switches the input
sudo go run ./cmd/bt-hid-relay

# only share the mouse (the keyboard is still watched for the hotkey but stays local)
sudo go run ./cmd/bt-hid-relay -only mouse

# verify the link without touching local devices: draws a small square with the host cursor
sudo go run ./cmd/bt-hid-relay -d -test
```

Root is required: binding L2CAP PSMs below 0x1001 needs `CAP_NET_BIND_SERVICE` and `bluetoothd` itself
runs as root.

## notes
* **RDP hides the input.** While an RDP session is connected Windows detaches the physical console
  session, and every local HID device — including this one — feeds that detached session. Look at the
  host through something that mirrors the console instead (Moonlight/Sunshine, Parsec, VNC), or run
  `tscon %SESSIONNAME% /dest:console` from an elevated prompt to move the RDP session back to the console.
* Pairing is Just Works (no MITM protection); keep the `-pair` window short.
* With several paired devices, disconnect the current host before connecting another one.
* Only Classic (BR/EDR) HID in report protocol mode is implemented, no BLE HOGP and no boot protocol,
  so some firmware setup screens may not accept it.
* Standard 6-key rollover; high-resolution wheel events are ignored; LED state is not echoed back.
* If Windows says "Bluetooth is not available on this device" after a reboot, its Bluetooth firmware is
  stuck: do a full `shutdown /s /f /t 0`, cut the power for a few minutes and boot again. No re-pairing
  is needed afterwards.

## usage
```
  -help    bool     Show usage message and quit
  -config  string   Specify file path of custom configuration json
  -d       bool     Enable debug output [CFG_DEBUG]
  -pair    int      Open a pairing window for N seconds (0 = no limit, -1 = closed) [CFG_PAIR] (default -1)
  -host    string   Only connect to this host address (default: every device paired with the adapter) [CFG_HOST]
  -adapter string   Bluetooth adapter like hci0 (default: the first one) [CFG_ADAPTER]
  -name    string   Device name shown to the host [CFG_NAME] (default "Linux HID Relay")
  -toggle  string   Key that switches input between local and host: KEY_* name or evdev code [CFG_TOGGLE] (default "KEY_SCROLLLOCK")
  -dev     string   Comma-separated evdev devices to forward (default: /dev/input/by-id/*-event-kbd and *-event-mouse) [CFG_DEV]
  -only    string   Forward only 'keyboard' or 'mouse' (not with -dev) [CFG_ONLY]
  -test    bool     Send synthetic mouse movement after connecting to verify the link [CFG_TEST]
  -btd     bool     Spawn a private bluetoothd with the input plugin disabled [CFG_BTD] (default true)
  -keepalive duration   Repeat the keyboard report at this interval while switched to the host (0 = off) [CFG_KEEPALIVE] (default 1s)
```

## prompt history
```md
本机是 linux，能否蓝牙连接到自己的另一台 headless windows，模拟 HID 设备，避免频繁插拔切换键鼠
尽量避免修改系统级配置
目录使用 ./bt-hid，编写相关脚本，并补充 README 记录原理和注意事项
切换过去后操作鼠标键盘无反应，推测是远程桌面的虚拟键鼠覆盖了模拟出的 HID 设备，导致操作不生效
测试使用 moonlight 可正常工作
重启后 win10 提示“此设备上的蓝牙无法使用。请尝试使用外部适配器为这台计算机添加蓝牙功能。”
主机断电几分钟后开机，蓝牙设备恢复
python 脚本主要依赖是什么，能否简单翻译为 go 程序提交至 ~/Git/misc
```
