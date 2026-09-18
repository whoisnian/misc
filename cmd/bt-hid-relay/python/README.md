# bt-hid — 把 Linux 变成 Windows 的蓝牙键鼠

让这台 Linux 通过蓝牙**扮演一个 HID 键鼠外设**，把本机物理键盘/鼠标的输入转发给另一台
（headless）Windows。按一个热键在两台机器之间切换，不用插拔任何线。

Windows 侧零安装：它看到的就是一个普通蓝牙键盘+鼠标，所以在锁屏、登录界面、
甚至部分 BIOS/UEFI 里都能用。

**不修改任何系统级配置**：不改 `/etc`，不 enable 任何 systemd 单元，不安装额外软件包。
所有东西都在这个目录里，`Ctrl-C` 退出后系统恢复原状。

## 文件

| 文件 | 作用 |
|---|---|
| `start.sh` | 启动一个独立的 `bluetoothd`（禁用 input 插件，读本目录 `main.conf`），再运行 `relay.py`。需要 sudo，会自动提权。 |
| `main.conf` | 给这个 `bluetoothd` 用的配置：设备名、Class of Device（键鼠组合外设）、PnP 信息等。 |
| `relay.py` | 主程序：注册 HID SDP 记录、监听 L2CAP 17/19、抓取 evdev、转换并发送 HID 报告、处理热键切换、断线重连。 |
| `state/` | 运行时状态：`last-host`（上次连接的主机 MAC，用于自动重连）、`bluetoothd.log`。 |

依赖：`bluez`、`python`、`python-gobject`（本机均已安装）。不需要 `python-dbus`、`python-evdev`。

## 使用

### 第一次：配对

```sh
./start.sh --pair          # 打开 120 秒的配对窗口；--pair 0 为不限时
```

然后在 Windows 上「设置 → 蓝牙和其他设备 → 添加设备 → 蓝牙」，选择 **Linux HID Relay**。
headless 机器可以通过 RDP 做这一步，或用 PowerShell：

```powershell
# Windows 侧，需要管理员；先扫描再配对
Get-PnpDevice -Class Bluetooth            # 确认本机有蓝牙适配器
# 最省事的还是 RDP 进去点一次「添加设备」
```

配对采用 Just Works（无 PIN），Windows 会直接显示「已连接」。终端会打印
`=== 已连接到主机 XX:XX:... ===` 并把 MAC 写进 `state/last-host`。

> **配好之后请断开 RDP，改用 Moonlight/Sunshine、Parsec、VNC 之类镜像控制台的工具看画面。**
> RDP 会把物理控制台会话锁起来，蓝牙键鼠的输入全部进了那个被锁的会话，
> 在 RDP 窗口里看起来就像"没反应"。详见下面「注意事项 → RDP」。

### 日常使用

```sh
./start.sh                 # 不开配对窗口；自动重连 state/last-host 里的主机
```

- 按 **ScrollLock**（松开时生效）→ 键鼠切到 Windows，本机不再收到任何输入
- 再按 ScrollLock → 切回本机
- 热键可换：`./start.sh --toggle KEY_PAUSE`（支持的名字见 `relay.py --help` 报错提示，或直接给 evdev 数字码）
- 默认转发 `/dev/input/by-id/` 下所有 `*-event-kbd` 和 `*-event-mouse`，支持热插拔；
  想指定设备用 `--dev /dev/input/eventN`（可多次）
- `-v` 打印协议细节，排障时用
- `--only mouse` / `--only keyboard` 只转发其中一种。只转发鼠标时键盘仍会被读取以响应热键，
  但不独占，键盘输入留在本机；只转发键盘时鼠标设备根本不会被打开
- `--test` 连上主机后自动发一组合成鼠标移动（画方框）+ 一次右 Shift，用来验证链路而不依赖本机键鼠

### 退出

`Ctrl-C`。会先把键鼠还给本机、向 Windows 发「全部松开」、关闭连接、结束 bluetoothd。

## 原理

```
 物理键鼠 ──USB──▶ Linux 内核 evdev (/dev/input/eventN)
                       │  relay.py 读取事件；切到 Windows 时 EVIOCGRAB 独占，
                       │  本机桌面就收不到了
                       ▼
                 Linux keycode → USB HID usage → HID Input Report
                       │
                       ▼
        L2CAP PSM 19 (Interrupt 通道，发报告)   ┐
        L2CAP PSM 17 (Control 通道，握手/查询)   ├─ 蓝牙 Classic ──▶ Windows 蓝牙 HID 驱动
        SDP 记录 (由 bluetoothd 代为发布)        ┘                   (hidbth.sys) → 系统键鼠
```

1. **SDP 记录**：Windows 在配对/连接时会查询设备的 SDP，看到 HID 服务（UUID 0x1124）
   以及里面的 **HID Report Descriptor**，就知道这是一个键盘+鼠标+多媒体键的组合设备。
   `relay.py` 通过 D-Bus `org.bluez.ProfileManager1.RegisterProfile` 让 bluetoothd 把这条
   记录挂到 SDP 服务器上（只借它发布记录，socket 自己管）。
2. **L2CAP 通道**：蓝牙 HID 规定两条固定通道，PSM 17（control）和 PSM 19（interrupt）。
   `relay.py` 直接用 `AF_BLUETOOTH/SOCK_SEQPACKET` 监听这两个 PSM，并设置
   `BT_SECURITY_MEDIUM` 要求链路加密。
3. **bluetoothd 的 input 插件**默认也会监听 17/19（它是 HID *主机*侧的实现，用来接蓝牙键鼠），
   两者冲突，所以 `start.sh` 用 `-P input` 禁用它。这是为什么不能直接用系统的
   `bluetooth.service`。
4. **报告格式**：每个 HID 报告前面加一个字节 `0xA1`（DATA | Input）从 interrupt 通道发出。
   三种 Report ID：1 = 键盘（8 字节：修饰键、保留、6 个键码），2 = 鼠标（5 字节：按键位、
   ΔX、ΔY、滚轮、横向滚轮），3 = 多媒体键（16 位 Consumer usage）。
5. **Control 通道**上 Windows 会发 `SET_PROTOCOL`、`GET_REPORT`、`SET_IDLE` 之类的请求，
   `relay.py` 按规范回 HANDSHAKE 或 DATA；`VIRTUAL_CABLE_UNPLUG` 表示 Windows 那边删除了设备。
6. **重连**：断线后 Windows 通常会主动连回来（SDP 里 `HIDNormallyConnectable=true`），
   `relay.py` 同时也会主动去连 `state/last-host`（`HIDReconnectInitiate=true`），退避到最长 30 秒。
7. **配对**：`relay.py` 注册一个 `NoInputNoOutput` 能力的 Agent，只有 `--pair` 时才把适配器置为
   Discoverable + Pairable，超时后 BlueZ 自动关闭。平时只处于「可连接」状态，
   已配对的 Windows 可以连，陌生设备既搜不到也配不了。

## 注意事项

### 与系统蓝牙的关系

- `start.sh` 启动的 bluetoothd 和系统的 `bluetooth.service` **不能同时运行**（都要占 D-Bus 的
  `org.bluez`）。本机 `bluetooth.service` 目前是 disabled，没有冲突；如果哪天 enable 了，运行前
  `sudo systemctl stop bluetooth`。运行期间本机的蓝牙耳机等也走这个 bluetoothd，
  一般不受影响，只是没有 HID 主机功能（收不了蓝牙键鼠）。
- **配对密钥**由 bluetoothd 写在 `/var/lib/bluetooth/<适配器MAC>/<Windows MAC>/`。这是运行时状态
  而非配置，必须保留才能免重复配对。要彻底清除：`sudo rm -r /var/lib/bluetooth/<适配器MAC>/<Windows MAC>`，
  Windows 侧也删掉设备。
- `Alias`（设备名）会被 bluetoothd 存到 `/var/lib/bluetooth/<适配器MAC>/settings`，同样是状态。

### 权限

- 绑定 PSM 17/19（< 0x1001）需要 `CAP_NET_BIND_SERVICE`，bluetoothd 本身也要 root，所以整套以 root
  运行。没有 setcap、没有 sudoers 改动。
- 读 `/dev/input` 你当前用户在 `input` 组即可，但因为以 root 跑，这点无所谓。

### 安全

- `NoInputNoOutput` 的 Just Works 配对**没有 MITM 保护**：在 `--pair` 窗口开着的那两分钟内，
  附近任何设备都能配上。用完窗口会自动关闭；不要长期 `--pair 0`。
- 已配对后链路是加密的（`BT_SECURITY_MEDIUM`）。
- 切到 Windows 时输入设备被 **独占**（grab），本机桌面完全收不到键鼠。程序在
  **蓝牙断开、异常、退出** 时都会自动释放，防止把自己锁在外面。极端情况下（进程被 `kill -9`），
  内核会在 fd 关闭时自动解除 grab，不会永久锁死。

### 热键

- 切换在**松开**热键时触发，这样按下/松开成对留在同一侧，不会出现某一侧「卡住一个键」。
  切换时不要同时按着其他键（比如 Shift），否则那个键的松开事件会跑到另一侧。
- 热键本身不会转发到 Windows；在本机模式下它仍会正常到达 Linux（ScrollLock 通常无害）。

### RDP 会话与蓝牙键鼠互斥

Windows 的 RDP 连接期间，物理控制台会话（console）被切换为断开/锁屏状态，所有本地 HID
输入——包括这个蓝牙键鼠——都送往 console 会话，而 RDP 窗口里的键鼠完全由 RDP 客户端虚拟设备提供。
所以在 RDP 画面里蓝牙键鼠永远"没反应"，这不是链路故障。

- 日常用：**Moonlight + Sunshine**（已验证）、Parsec、VNC 等镜像 console 会话的工具，输入立刻可见。
- 只是临时在 RDP 里想切回去：以管理员运行 `tscon %SESSIONNAME% /dest:console`，
  会断开 RDP 并把当前桌面原样搬回物理控制台（不锁屏）。
- 排查时先跑 `./start.sh -v --test`，终端里能看到每个 `interrupt -> a102…` 报告成功发出就说明
  Linux 侧没问题，剩下的只可能是 Windows 的会话归属或设备状态。

### 兼容性 / 已知限制

- 只实现了 Classic（BR/EDR）HID，不是 BLE HOGP。AX200 和几乎所有 PC 蓝牙适配器都支持 Classic。
- 不支持 Boot Protocol（SDP 里 `HIDBootDevice=false`）。**这意味着部分 BIOS/UEFI 可能不认**；
  Windows 登录界面没问题。
- 键盘最多 6 键无冲（标准 HID 数组），超出的按键会被忽略。
- 高精度滚轮事件（`REL_WHEEL_HI_RES`）被忽略，只用普通滚轮档位。
- 鼠标位移超过 ±127 时会拆成多个报告发送，不会丢失。
- 延迟大约 10–20 ms（蓝牙 sniff 间隔），打字和日常操作无感，不适合竞技游戏。
- Windows 那边 Caps/Num Lock LED 状态不会同步回本机键盘（输出报告被忽略）。

## 排障

| 现象 | 处理 |
|---|---|
| `PSM 17 已被占用` | 系统 `bluetooth.service` 在跑，`sudo systemctl stop bluetooth` |
| `无法访问 org.bluez` | bluetoothd 没起来，看 `state/bluetoothd.log`；`BTD_DEBUG=1 ./start.sh` 可以打开 bluetoothd 调试日志 |
| Windows 搜不到设备 | 是否用了 `--pair`？窗口是否已超时？`bluetoothctl show` 看 Discoverable 是否 yes |
| Windows 显示「驱动程序错误」 | SDP/描述符问题，用 `-v` 看 control 通道交互；在 Windows 删除设备后重新配对 |
| 配对成功但一直不连接 | 在 Windows 设备列表里点一下设备（或重启 Windows 蓝牙）；`relay.py` 也会每 3–30 秒主动尝试 |
| 切换后本机键鼠没反应 | 正常，已切到 Windows；再按热键切回。若程序已崩溃，grab 会自动释放 |
| 切过去后 Windows 没反应 | 是不是在 RDP 里看的？见「RDP 会话与蓝牙键鼠互斥」。`--test` 能确认报告已发出 |
| 一堆 `授权服务 <uuid>` 日志 | Windows 连了这个 bluetoothd 上注册的其他服务（如 PipeWire 的 A2DP/AVRCP），无害 |
| Windows 重启后提示「此设备上的蓝牙无法使用」 | Windows 侧蓝牙固件卡死（Intel 网卡的蓝牙走 USB，「重启」不断电）。`shutdown /s /f /t 0` 完全关机后**拔电源几分钟**再开机即可恢复（已验证），不需要重新配对 |
| 某个键不生效 | `-v` 会打印 `未映射的按键 code=N`，把它加进 `relay.py` 的 `LINUX_TO_HID` |

## 手动操作（不用 start.sh）

```sh
sudo /usr/lib/bluetooth/bluetoothd -n -P input -f "$PWD/main.conf" &
sudo python3 relay.py --pair
```

也可以用 `systemd-run` 跑成临时单元（重启即消失，同样不动 /etc）：

```sh
sudo systemd-run --unit=bt-hid-btd /usr/lib/bluetooth/bluetoothd -n -P input -f "$PWD/main.conf"
sudo python3 relay.py
sudo systemctl stop bt-hid-btd      # 结束
```
