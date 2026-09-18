#!/usr/bin/env python3
"""
bt-hid relay: 把本机键盘/鼠标的 evdev 事件通过蓝牙 HID（Classic/BR-EDR）转发给已配对的主机。

依赖: python3 + python-gobject (Gio D-Bus)。不依赖 python-dbus / python-evdev。
需要 root（绑定 L2CAP PSM 17/19 需要 CAP_NET_BIND_SERVICE）。
"""
import argparse
import fcntl
import glob
import os
import signal
import socket
import struct
import sys
import time

import warnings

import gi

gi.require_version("Gio", "2.0")
gi.require_version("GLib", "2.0")
from gi.repository import Gio, GLib  # noqa: E402

# PyGObject 3.56 把 register_object 标为 deprecated，但它仍是最简单的导出对象方式
warnings.filterwarnings("ignore", category=DeprecationWarning, module=__name__)

HERE = os.path.dirname(os.path.abspath(__file__))
STATE_DIR = os.path.join(HERE, "state")
LAST_HOST_FILE = os.path.join(STATE_DIR, "last-host")

# ---------------------------------------------------------------- Bluetooth HID 常量
HID_UUID = "00001124-0000-1000-8000-00805f9b34fb"
PSM_CTRL, PSM_INTR = 17, 19
SOL_BLUETOOTH, BT_SECURITY, BT_SECURITY_MEDIUM = 274, 4, 2

# HIDP 传输层头部: 高 4 位为消息类型, 低 4 位为参数
MSG_HANDSHAKE, MSG_HID_CONTROL = 0x0, 0x1
MSG_GET_REPORT, MSG_SET_REPORT = 0x4, 0x5
MSG_GET_PROTOCOL, MSG_SET_PROTOCOL = 0x6, 0x7
MSG_GET_IDLE, MSG_SET_IDLE = 0x8, 0x9
MSG_DATA = 0xA
HS_SUCCESS, HS_NOT_READY, HS_ERR_INVALID_REPORT_ID, HS_ERR_UNSUPPORTED, HS_ERR_INVALID_PARAM = 0, 1, 2, 3, 4
CTRL_VIRTUAL_CABLE_UNPLUG = 5
REPORT_INPUT, REPORT_OUTPUT, REPORT_FEATURE = 1, 2, 3
DATA_INPUT = (MSG_DATA << 4) | REPORT_INPUT   # 0xA1
DATA_OTHER = (MSG_DATA << 4) | 0              # 0xA0

RID_KEYBOARD, RID_MOUSE, RID_CONSUMER = 1, 2, 3

# HID Report Descriptor：键盘(ID1) + 鼠标(ID2, 5 键 + 滚轮 + 横向滚轮) + 多媒体键(ID3)
REPORT_DESC = bytes([
    # --- Keyboard, Report ID 1 ---
    0x05, 0x01, 0x09, 0x06, 0xA1, 0x01, 0x85, RID_KEYBOARD,
    0x05, 0x07, 0x19, 0xE0, 0x29, 0xE7, 0x15, 0x00, 0x25, 0x01,
    0x75, 0x01, 0x95, 0x08, 0x81, 0x02,                          # 8 个修饰键 bit
    0x95, 0x01, 0x75, 0x08, 0x81, 0x01,                          # 保留字节
    0x95, 0x05, 0x75, 0x01, 0x05, 0x08, 0x19, 0x01, 0x29, 0x05, 0x91, 0x02,  # 5 个 LED
    0x95, 0x01, 0x75, 0x03, 0x91, 0x01,                          # LED 填充
    0x95, 0x06, 0x75, 0x08, 0x15, 0x00, 0x26, 0xFF, 0x00,
    0x05, 0x07, 0x19, 0x00, 0x2A, 0xFF, 0x00, 0x81, 0x00,        # 6 键无冲数组
    0xC0,
    # --- Mouse, Report ID 2 ---
    0x05, 0x01, 0x09, 0x02, 0xA1, 0x01, 0x85, RID_MOUSE, 0x09, 0x01, 0xA1, 0x00,
    0x05, 0x09, 0x19, 0x01, 0x29, 0x05, 0x15, 0x00, 0x25, 0x01,
    0x95, 0x05, 0x75, 0x01, 0x81, 0x02,                          # 5 个按键 bit
    0x95, 0x01, 0x75, 0x03, 0x81, 0x01,                          # 填充
    0x05, 0x01, 0x09, 0x30, 0x09, 0x31, 0x09, 0x38,
    0x15, 0x81, 0x25, 0x7F, 0x75, 0x08, 0x95, 0x03, 0x81, 0x06,  # X, Y, Wheel (相对)
    0x05, 0x0C, 0x0A, 0x38, 0x02,
    0x15, 0x81, 0x25, 0x7F, 0x75, 0x08, 0x95, 0x01, 0x81, 0x06,  # AC Pan (横向滚轮)
    0xC0, 0xC0,
    # --- Consumer Control, Report ID 3 ---
    0x05, 0x0C, 0x09, 0x01, 0xA1, 0x01, 0x85, RID_CONSUMER,
    0x15, 0x00, 0x26, 0xFF, 0x03, 0x19, 0x00, 0x2A, 0xFF, 0x03,
    0x75, 0x10, 0x95, 0x01, 0x81, 0x00,                          # 一个 16 位 usage
    0xC0,
])


def sdp_record(name: str) -> str:
    """HID Profile 的 SDP 记录（BlueZ ServiceRecord XML 格式）。"""
    desc_hex = REPORT_DESC.hex()
    return f"""<?xml version="1.0" encoding="UTF-8" ?>
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
  <attribute id="0x0100"><text value="{name}"/></attribute>
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
      <text encoding="hex" value="{desc_hex}"/>
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
"""


# ---------------------------------------------------------------- Linux keycode -> HID usage
# 修饰键 usage 0xE0..0xE7 会被转换为 modifier bit
LINUX_TO_HID = {
    1: 0x29,  # ESC
    2: 0x1E, 3: 0x1F, 4: 0x20, 5: 0x21, 6: 0x22, 7: 0x23, 8: 0x24, 9: 0x25, 10: 0x26, 11: 0x27,  # 1..0
    12: 0x2D, 13: 0x2E, 14: 0x2A, 15: 0x2B,  # - = Backspace Tab
    16: 0x14, 17: 0x1A, 18: 0x08, 19: 0x15, 20: 0x17, 21: 0x1C, 22: 0x18, 23: 0x0C, 24: 0x12, 25: 0x13,  # Q..P
    26: 0x2F, 27: 0x30, 28: 0x28, 29: 0xE0,  # [ ] Enter LCtrl
    30: 0x04, 31: 0x16, 32: 0x07, 33: 0x09, 34: 0x0A, 35: 0x0B, 36: 0x0D, 37: 0x0E, 38: 0x0F,  # A..L
    39: 0x33, 40: 0x34, 41: 0x35, 42: 0xE1, 43: 0x31,  # ; ' ` LShift \
    44: 0x1D, 45: 0x1B, 46: 0x06, 47: 0x19, 48: 0x05, 49: 0x11, 50: 0x10,  # Z..M
    51: 0x36, 52: 0x37, 53: 0x38, 54: 0xE5, 55: 0x55, 56: 0xE2, 57: 0x2C, 58: 0x39,  # , . / RShift KP* LAlt Space Caps
    59: 0x3A, 60: 0x3B, 61: 0x3C, 62: 0x3D, 63: 0x3E, 64: 0x3F, 65: 0x40, 66: 0x41, 67: 0x42, 68: 0x43,  # F1..F10
    69: 0x53, 70: 0x47,  # NumLock ScrollLock
    71: 0x5F, 72: 0x60, 73: 0x61, 74: 0x56, 75: 0x5C, 76: 0x5D, 77: 0x5E, 78: 0x57,  # KP7 8 9 - 4 5 6 +
    79: 0x59, 80: 0x5A, 81: 0x5B, 82: 0x62, 83: 0x63,  # KP1 2 3 0 .
    86: 0x64, 87: 0x44, 88: 0x45,  # 102ND F11 F12
    89: 0x87, 92: 0x8A, 93: 0x88, 94: 0x8B,  # RO HENKAN KATAKANAHIRAGANA MUHENKAN
    96: 0x58, 97: 0xE4, 98: 0x54, 99: 0x46, 100: 0xE6,  # KPEnter RCtrl KP/ SysRq(PrtSc) RAlt
    102: 0x4A, 103: 0x52, 104: 0x4B, 105: 0x50, 106: 0x4F, 107: 0x4D, 108: 0x51, 109: 0x4E,  # Home Up PgUp Left Right End Down PgDn
    110: 0x49, 111: 0x4C,  # Insert Delete
    117: 0x67, 119: 0x48, 121: 0x85, 122: 0x90, 123: 0x91, 124: 0x89,  # KP= Pause KP, Hangeul Hanja Yen
    125: 0xE3, 126: 0xE7, 127: 0x65,  # LMeta RMeta Compose(Menu)
    183: 0x68, 184: 0x69, 185: 0x6A, 186: 0x6B, 187: 0x6C, 188: 0x6D,
    189: 0x6E, 190: 0x6F, 191: 0x70, 192: 0x71, 193: 0x72, 194: 0x73,  # F13..F24
}

# 多媒体键 -> Consumer Page usage
LINUX_TO_CONSUMER = {
    113: 0x00E2,  # MUTE
    114: 0x00EA,  # VOLUMEDOWN
    115: 0x00E9,  # VOLUMEUP
    140: 0x0192,  # CALC
    155: 0x018A,  # MAIL
    163: 0x00B5,  # NEXTSONG
    164: 0x00CD,  # PLAYPAUSE
    165: 0x00B6,  # PREVIOUSSONG
    166: 0x00B7,  # STOPCD
    172: 0x0223,  # HOMEPAGE
}

# --toggle 可用的按键名
KEY_NAMES = {
    "KEY_SCROLLLOCK": 70, "KEY_PAUSE": 119, "KEY_SYSRQ": 99, "KEY_COMPOSE": 127,
    "KEY_RIGHTCTRL": 97, "KEY_RIGHTALT": 100, "KEY_RIGHTMETA": 126,
    "KEY_F12": 88, "KEY_KPASTERISK": 55, "KEY_INSERT": 110,
}

# ---------------------------------------------------------------- evdev (纯 Python)
EV_SYN, EV_KEY, EV_REL = 0, 1, 2
SYN_REPORT = 0
REL_X, REL_Y, REL_HWHEEL, REL_WHEEL = 0, 1, 6, 8
BTN_LEFT, BTN_EXTRA = 0x110, 0x114
EVENT_FMT = "llHHi"
EVENT_SIZE = struct.calcsize(EVENT_FMT)
EVIOCGRAB = 0x40044590


def EVIOCGNAME(n):
    return 0x80004506 | (n << 16)


class InputDev:
    def __init__(self, path, forward=True):
        self.path = path
        self.forward = forward          # False: 只用来监听热键，不独占、不转发
        self.real = os.path.realpath(path)
        self.fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK)
        buf = bytearray(256)
        fcntl.ioctl(self.fd, EVIOCGNAME(256), buf)
        self.name = buf.split(b"\0", 1)[0].decode(errors="replace")
        self.grabbed = False
        self.watch = None

    def grab(self, on: bool):
        if on != self.grabbed:
            fcntl.ioctl(self.fd, EVIOCGRAB, 1 if on else 0)
            self.grabbed = on

    def read_events(self):
        try:
            data = os.read(self.fd, EVENT_SIZE * 64)
        except BlockingIOError:
            return []
        n = len(data) - len(data) % EVENT_SIZE
        return [struct.unpack_from(EVENT_FMT, data, off)[2:] for off in range(0, n, EVENT_SIZE)]

    def close(self):
        try:
            self.grab(False)
        except OSError:
            pass
        if self.watch is not None:
            GLib.source_remove(self.watch)
            self.watch = None
        os.close(self.fd)


# ---------------------------------------------------------------- D-Bus 接口描述
AGENT_PATH = "/bt_hid/agent"
PROFILE_PATH = "/bt_hid/profile"
AGENT_XML = """
<node><interface name="org.bluez.Agent1">
  <method name="Release"/>
  <method name="RequestPinCode"><arg type="o" direction="in"/><arg type="s" direction="out"/></method>
  <method name="DisplayPinCode"><arg type="o" direction="in"/><arg type="s" direction="in"/></method>
  <method name="RequestPasskey"><arg type="o" direction="in"/><arg type="u" direction="out"/></method>
  <method name="DisplayPasskey"><arg type="o" direction="in"/><arg type="u" direction="in"/><arg type="q" direction="in"/></method>
  <method name="RequestConfirmation"><arg type="o" direction="in"/><arg type="u" direction="in"/></method>
  <method name="RequestAuthorization"><arg type="o" direction="in"/></method>
  <method name="AuthorizeService"><arg type="o" direction="in"/><arg type="s" direction="in"/></method>
  <method name="Cancel"/>
</interface></node>"""
PROFILE_XML = """
<node><interface name="org.bluez.Profile1">
  <method name="Release"/>
  <method name="NewConnection"><arg type="o" direction="in"/><arg type="h" direction="in"/><arg type="a{sv}" direction="in"/></method>
  <method name="RequestDisconnection"><arg type="o" direction="in"/></method>
</interface></node>"""


def log(msg):
    print(time.strftime("%H:%M:%S"), msg, flush=True)


def clamp8(v):
    return max(-127, min(127, v))


def new_l2cap():
    s = socket.socket(socket.AF_BLUETOOTH, socket.SOCK_SEQPACKET, socket.BTPROTO_L2CAP)
    s.setsockopt(SOL_BLUETOOTH, BT_SECURITY, struct.pack("BB", BT_SECURITY_MEDIUM, 0))
    return s


# ---------------------------------------------------------------- 主体
class Relay:
    def __init__(self, args):
        self.args = args
        self.verbose = args.verbose
        self.toggle_code = args.toggle
        self.loop = GLib.MainLoop()

        # 蓝牙连接状态
        self.ctrl = self.intr = None
        self.ctrl_watch = self.intr_watch = None
        self.peer = None                # 当前连接的主机 MAC
        self.host = args.host           # 主动重连目标
        self.outbound = None            # (socket, which, watch) 进行中的主动连接
        self.retry_timer = None
        self.retry_delay = 3

        # 输入设备与 HID 状态
        self.devices = {}               # realpath -> InputDev
        self.forwarding = False
        self.reset_hid_state()
        self.last_reports = {
            RID_KEYBOARD: bytes([RID_KEYBOARD] + [0] * 8),
            RID_MOUSE: bytes([RID_MOUSE] + [0] * 5),
            RID_CONSUMER: bytes([RID_CONSUMER, 0, 0]),
        }

        self.bus = None
        self.adapter_path = None
        self.addr = None

    # ---------------- HID 状态
    def reset_hid_state(self):
        self.kb_mods = 0
        self.kb_keys = []
        self.kb_dirty = False
        self.btns = 0
        self.rel = {REL_X: 0, REL_Y: 0, REL_WHEEL: 0, REL_HWHEEL: 0}
        self.mouse_dirty = False

    @property
    def connected(self):
        return self.ctrl is not None and self.intr is not None

    # ---------------- D-Bus / BlueZ 初始化
    def setup_bluez(self):
        self.bus = Gio.bus_get_sync(Gio.BusType.SYSTEM, None)
        try:
            self.adapter_path, self.addr = self.find_adapter(self.args.adapter)
        except GLib.Error as e:
            sys.exit(f"无法访问 org.bluez（bluetoothd 没在运行？）: {e.message}")
        if self.adapter_path is None:
            sys.exit(f"找不到蓝牙适配器 {self.args.adapter or ''}")
        log(f"适配器 {self.adapter_path} ({self.addr})")

        # Agent：接受配对请求（仅在 --pair 窗口期内 Pairable=true 时会被触发）
        agent_iface = Gio.DBusNodeInfo.new_for_xml(AGENT_XML).interfaces[0]
        self.bus.register_object(AGENT_PATH, agent_iface, self.on_agent_call, None, None)
        self.call("/org/bluez", "org.bluez.AgentManager1", "RegisterAgent",
                  GLib.Variant("(os)", (AGENT_PATH, "NoInputNoOutput")))
        self.call("/org/bluez", "org.bluez.AgentManager1", "RequestDefaultAgent",
                  GLib.Variant("(o)", (AGENT_PATH,)))

        # HID Profile：只借 BlueZ 发布 SDP 记录；L2CAP 17/19 由本程序自己监听
        profile_iface = Gio.DBusNodeInfo.new_for_xml(PROFILE_XML).interfaces[0]
        self.bus.register_object(PROFILE_PATH, profile_iface, self.on_profile_call, None, None)
        opts = {
            "ServiceRecord": GLib.Variant("s", sdp_record(self.args.name)),
            "Role": GLib.Variant("s", "server"),
            "RequireAuthentication": GLib.Variant("b", False),
            "RequireAuthorization": GLib.Variant("b", False),
        }
        self.call("/org/bluez", "org.bluez.ProfileManager1", "RegisterProfile",
                  GLib.Variant("(osa{sv})", (PROFILE_PATH, HID_UUID, opts)))

        # 适配器属性
        self.set_adapter("Alias", GLib.Variant("s", self.args.name))
        self.set_adapter("Powered", GLib.Variant("b", True))
        pair = self.args.pair
        if pair is not None:
            self.set_adapter("DiscoverableTimeout", GLib.Variant("u", pair))
            self.set_adapter("PairableTimeout", GLib.Variant("u", pair))
            self.set_adapter("Pairable", GLib.Variant("b", True))
            self.set_adapter("Discoverable", GLib.Variant("b", True))
            log(f"*** 配对窗口已打开{'（不限时）' if pair == 0 else f'（{pair} 秒）'}，"
                f"请在 Windows「添加蓝牙设备」中选择 “{self.args.name}”")
        else:
            self.set_adapter("Discoverable", GLib.Variant("b", False))
            self.set_adapter("Pairable", GLib.Variant("b", False))

        # 监听设备属性变化（Paired / Connected），用于日志和记录主机 MAC
        self.bus.signal_subscribe("org.bluez", "org.freedesktop.DBus.Properties", "PropertiesChanged",
                                  None, "org.bluez.Device1", Gio.DBusSignalFlags.NONE, self.on_props_changed)

    def call(self, path, iface, method, params=None, reply_type=None):
        return self.bus.call_sync("org.bluez", path, iface, method, params,
                                  GLib.VariantType(reply_type) if reply_type else None,
                                  Gio.DBusCallFlags.NONE, -1, None)

    def get_adapter(self, prop):
        res = self.call(self.adapter_path, "org.freedesktop.DBus.Properties", "Get",
                        GLib.Variant("(ss)", ("org.bluez.Adapter1", prop)), "(v)")
        return res.unpack()[0]

    def set_adapter(self, prop, value):
        # bluetoothd 刚启动时（AutoEnable）适配器可能正在上电，此时 Set 会返回 Busy，稍等重试
        for attempt in range(20):
            if self.get_adapter(prop) == value.unpack():
                return
            try:
                self.call(self.adapter_path, "org.freedesktop.DBus.Properties", "Set",
                          GLib.Variant("(ssv)", ("org.bluez.Adapter1", prop, value)))
                return
            except GLib.Error as e:
                if "Busy" not in e.message or attempt == 19:
                    raise
                time.sleep(0.25)

    def find_adapter(self, want):
        res = self.call("/", "org.freedesktop.DBus.ObjectManager", "GetManagedObjects",
                        None, "(a{oa{sa{sv}}})")
        for path, ifaces in sorted(res.unpack()[0].items()):
            if "org.bluez.Adapter1" in ifaces and (not want or path.endswith("/" + want)):
                return path, ifaces["org.bluez.Adapter1"]["Address"]
        return None, None

    def on_agent_call(self, conn, sender, path, iface, method, params, invocation):
        dev = params.unpack()[0] if params.n_children() else ""
        if method == "RequestPinCode":
            invocation.return_value(GLib.Variant("(s)", ("0000",)))
        elif method == "RequestPasskey":
            invocation.return_value(GLib.Variant("(u)", (0,)))
        elif method in ("DisplayPinCode", "DisplayPasskey"):
            log(f"配对码 {params.unpack()[1]} （设备 {dev}）")
            invocation.return_value(None)
        elif method == "RequestConfirmation":
            log(f"确认配对码 {params.unpack()[1]} （设备 {dev}）→ 接受")
            invocation.return_value(None)
        else:  # Release / RequestAuthorization / AuthorizeService / Cancel
            if method == "AuthorizeService":
                log(f"授权服务 {params.unpack()[1]} 给 {dev} → 接受")
            elif method == "RequestAuthorization":
                log(f"授权 {dev} → 接受")
            invocation.return_value(None)

    def on_profile_call(self, conn, sender, path, iface, method, params, invocation):
        if method == "NewConnection":
            # 我们不用 BlueZ 分配的 socket；关掉传来的 fd
            fdlist = invocation.get_message().get_unix_fd_list()
            if fdlist is not None:
                for i in range(fdlist.get_length()):
                    try:
                        os.close(fdlist.get(i))
                    except OSError:
                        pass
        invocation.return_value(None)

    def on_props_changed(self, conn, sender, path, iface, signal_name, params):
        _, changed, _ = params.unpack()
        mac = path.rsplit("/dev_", 1)[-1].replace("_", ":") if "/dev_" in path else path
        if "Paired" in changed:
            log(f"设备 {mac} Paired={changed['Paired']}")
            if changed["Paired"]:
                self.remember_host(mac)
        if "Connected" in changed and self.verbose:
            log(f"设备 {mac} ACL Connected={changed['Connected']}")

    # ---------------- 主机记录 / 主动重连
    def remember_host(self, mac):
        mac = mac.upper()
        if self.host != mac:
            self.host = mac
            log(f"记录主机 {mac} → {LAST_HOST_FILE}")
            os.makedirs(STATE_DIR, exist_ok=True)
            with open(LAST_HOST_FILE, "w") as f:
                f.write(mac + "\n")
        self.schedule_connect(1)

    def schedule_connect(self, delay=None):
        if self.retry_timer is not None or self.connected or self.outbound or not self.host:
            return
        delay = self.retry_delay if delay is None else delay
        self.retry_timer = GLib.timeout_add_seconds(delay, self.try_connect)

    def try_connect(self):
        self.retry_timer = None
        if self.connected or self.outbound or not self.host:
            return False
        self.start_outbound(PSM_CTRL)
        return False

    def start_outbound(self, psm):
        s = new_l2cap()
        s.bind((self.addr, 0))
        s.setblocking(False)
        try:
            s.connect((self.host, psm))
        except BlockingIOError:
            pass
        except OSError as e:
            s.close()
            self.outbound_failed(f"connect psm {psm}: {e}")
            return
        which = "ctrl" if psm == PSM_CTRL else "intr"
        w = GLib.io_add_watch(s.fileno(), GLib.PRIORITY_DEFAULT,
                              GLib.IO_OUT | GLib.IO_ERR | GLib.IO_HUP, self.on_outbound_ready, s, which)
        self.outbound = (s, which, w)
        if self.verbose:
            log(f"主动连接 {self.host} psm {psm} ...")

    def on_outbound_ready(self, fd, cond, s, which):
        self.outbound = None
        err = s.getsockopt(socket.SOL_SOCKET, socket.SO_ERROR)
        if err:
            s.close()
            if which == "intr":
                self.drop_connection("interrupt 通道连接失败")
            self.outbound_failed(f"{which}: {os.strerror(err)}")
            return False
        s.setblocking(True)
        if which == "ctrl":
            self.attach(s, "ctrl", self.host)
            self.start_outbound(PSM_INTR)
        else:
            self.attach(s, "intr", self.host)
        return False

    def outbound_failed(self, why):
        if self.verbose:
            log(f"主动连接失败: {why}，{self.retry_delay}s 后重试")
        self.schedule_connect()
        self.retry_delay = min(self.retry_delay * 2, 30)

    def abort_outbound(self):
        if self.outbound:
            s, _, w = self.outbound
            GLib.source_remove(w)
            s.close()
            self.outbound = None

    # ---------------- 被动监听
    def setup_listeners(self):
        self.listeners = []
        for psm in (PSM_CTRL, PSM_INTR):
            s = new_l2cap()
            try:
                s.bind((self.addr, psm))
            except PermissionError:
                sys.exit("绑定 L2CAP PSM 需要 root 权限（请通过 start.sh 或 sudo 运行）")
            except OSError as e:
                if e.errno == 98:  # EADDRINUSE
                    sys.exit(f"PSM {psm} 已被占用：bluetoothd 的 input 插件仍在运行？"
                             "需要以 `bluetoothd -P input` 启动（start.sh 已处理）")
                raise
            s.listen(1)
            which = "ctrl" if psm == PSM_CTRL else "intr"
            GLib.io_add_watch(s.fileno(), GLib.PRIORITY_DEFAULT, GLib.IO_IN, self.on_accept, s, which)
            self.listeners.append(s)
        log(f"监听 L2CAP PSM {PSM_CTRL}/{PSM_INTR}，等待主机连接"
            + (f"（也会主动重连 {self.host}）" if self.host else ""))

    def on_accept(self, fd, cond, lsock, which):
        conn, peer = lsock.accept()  # peer = (mac, psm) 或新版 Python 的 (mac, psm, cid)
        mac = peer[0].upper()
        if which == "ctrl":
            # 新的 control 连接意味着主机重新开始了一次 HID 会话
            self.abort_outbound()
            self.drop_connection("主机发起新连接", reconnect=False)
        elif self.ctrl is None or self.peer != mac:
            log(f"忽略来自 {mac} 的 interrupt 连接（没有对应的 control 通道）")
            conn.close()
            return True
        self.attach(conn, which, mac)
        return True

    # ---------------- 连接管理
    def attach(self, s, which, mac):
        mac = mac.upper()
        self.peer = mac
        w = GLib.io_add_watch(s.fileno(), GLib.PRIORITY_DEFAULT,
                              GLib.IO_IN | GLib.IO_ERR | GLib.IO_HUP, self.on_sock_data, s, which)
        if which == "ctrl":
            self.ctrl, self.ctrl_watch = s, w
        else:
            self.intr, self.intr_watch = s, w
        if self.connected:
            self.retry_delay = 3
            log(f"=== 已连接到主机 {mac}，按 toggle 键 ({self.args.toggle_name}) 切换键鼠 ===")
            self.remember_host(mac)
            if self.args.test:
                GLib.timeout_add(1500, self.self_test)

    def drop_connection(self, why, reconnect=True):
        had = self.ctrl is not None or self.intr is not None
        for attr, wattr in (("ctrl", "ctrl_watch"), ("intr", "intr_watch")):
            s, w = getattr(self, attr), getattr(self, wattr)
            if w is not None:
                GLib.source_remove(w)
            if s is not None:
                try:
                    s.close()
                except OSError:
                    pass
            setattr(self, attr, None)
            setattr(self, wattr, None)
        if had:
            log(f"--- 与主机 {self.peer} 断开：{why}")
        if self.forwarding:
            self.set_forwarding(False, notify_host=False)
        self.peer = None
        if reconnect:
            self.schedule_connect()

    def on_sock_data(self, fd, cond, s, which):
        if cond & (GLib.IO_ERR | GLib.IO_HUP):
            self.drop_connection(f"{which} 通道关闭")
            return False
        try:
            data = s.recv(1024)
        except OSError as e:
            self.drop_connection(f"{which} recv: {e}")
            return False
        if not data:
            self.drop_connection(f"{which} 通道被对端关闭")
            return False
        if which == "ctrl":
            self.handle_control(data)
        elif self.verbose:
            log(f"interrupt <- {data.hex()}")  # 一般是 LED 输出报告
        return True

    def handle_control(self, data):
        msg, param = data[0] >> 4, data[0] & 0x0F
        if self.verbose:
            log(f"control <- {data.hex()}")
        if msg == MSG_HID_CONTROL:
            if param == CTRL_VIRTUAL_CABLE_UNPLUG:
                log("主机发送 VIRTUAL_CABLE_UNPLUG（在 Windows 侧删除了设备）")
                self.host = None
                self.drop_connection("virtual cable unplug", reconnect=False)
            return
        if msg == MSG_GET_REPORT:
            rtype = param & 0x3
            rid = data[1] if len(data) > 1 else 0
            if rtype == REPORT_INPUT and rid in self.last_reports:
                self.ctrl_send(bytes([DATA_INPUT]) + self.last_reports[rid])
            else:
                self.handshake(HS_ERR_INVALID_REPORT_ID if rtype == REPORT_INPUT else HS_ERR_UNSUPPORTED)
        elif msg == MSG_SET_REPORT:
            self.handshake(HS_SUCCESS)
        elif msg == MSG_GET_PROTOCOL:
            self.ctrl_send(bytes([DATA_OTHER, 1]))  # 1 = Report Protocol
        elif msg == MSG_SET_PROTOCOL:
            self.handshake(HS_SUCCESS if (param & 1) == 1 else HS_ERR_UNSUPPORTED)
        elif msg == MSG_GET_IDLE:
            self.ctrl_send(bytes([DATA_OTHER, 0]))
        elif msg == MSG_SET_IDLE:
            self.handshake(HS_SUCCESS)
        elif msg == MSG_DATA:
            pass  # 通过 control 通道发来的输出报告，忽略
        else:
            self.handshake(HS_ERR_UNSUPPORTED)

    def handshake(self, result):
        self.ctrl_send(bytes([(MSG_HANDSHAKE << 4) | result]))

    def ctrl_send(self, payload):
        try:
            self.ctrl.send(payload)
        except OSError as e:
            self.drop_connection(f"control send: {e}")

    def send_report(self, rid, body: bytes):
        report = bytes([rid]) + body
        self.last_reports[rid] = report
        if not self.connected:
            return
        pkt = bytes([DATA_INPUT]) + report
        try:
            n = self.intr.send(pkt)
        except OSError as e:
            self.drop_connection(f"interrupt send: {e}")
            return
        if self.verbose:
            log(f"interrupt -> {pkt.hex()} ({n} bytes)")

    def send_release_all(self):
        self.send_report(RID_KEYBOARD, bytes(8))
        self.send_report(RID_MOUSE, bytes(5))
        self.send_report(RID_CONSUMER, bytes(2))

    def self_test(self):
        """连上后用合成事件让主机鼠标画一个方框，并按一下 ScrollLock 以外的无害键（右 Shift）。"""
        if not self.connected:
            return False
        log("自检：发送合成鼠标/键盘报告")
        for dx, dy in ((40, 0), (0, 40), (-40, 0), (0, -40)):
            for _ in range(5):
                self.send_report(RID_MOUSE, struct.pack("<Bbbbb", 0, dx // 5, dy // 5, 0, 0))
                time.sleep(0.02)
        self.send_report(RID_KEYBOARD, bytes([0x20, 0, 0, 0, 0, 0, 0, 0]))  # 右 Shift 按下
        time.sleep(0.05)
        self.send_report(RID_KEYBOARD, bytes(8))
        return False

    # ---------------- 输入设备
    def discover_devices(self):
        only = self.args.only
        if self.args.dev:
            paths = [(p, True) for p in self.args.dev]
        else:
            kbds = sorted(glob.glob("/dev/input/by-id/*-event-kbd"))
            mice = sorted(glob.glob("/dev/input/by-id/*-event-mouse"))
            # 键盘总是要打开（热键在上面），但 --only mouse 时不转发它
            paths = [(p, only != "mouse") for p in kbds]
            if only != "keyboard":
                paths += [(p, True) for p in mice]
        for p, forward in paths:
            real = os.path.realpath(p)
            if real in self.devices:
                continue
            try:
                dev = InputDev(p, forward)
            except OSError as e:
                log(f"打开 {p} 失败: {e}")
                continue
            dev.watch = GLib.io_add_watch(dev.fd, GLib.PRIORITY_DEFAULT,
                                          GLib.IO_IN | GLib.IO_ERR | GLib.IO_HUP, self.on_input, dev)
            self.devices[real] = dev
            log(f"输入设备 {dev.name}  ({os.path.basename(p)})" + ("" if forward else "  [仅监听热键，不转发]"))
            if self.forwarding and dev.forward:
                try:
                    dev.grab(True)
                except OSError as e:
                    log(f"grab {dev.name} 失败: {e}")
        return True

    def on_input(self, fd, cond, dev):
        if cond & (GLib.IO_ERR | GLib.IO_HUP):
            self.remove_device(dev)
            return False
        try:
            events = dev.read_events()
        except OSError:
            self.remove_device(dev)
            return False
        for type_, code, value in events:
            if dev.forward or (type_ == EV_KEY and code == self.toggle_code):
                self.handle_event(type_, code, value)
        return True

    def remove_device(self, dev):
        log(f"输入设备移除 {dev.name}")
        self.devices.pop(dev.real, None)
        dev.watch = None  # 由 on_input 返回 False 移除
        try:
            os.close(dev.fd)
        except OSError:
            pass

    def set_forwarding(self, on, notify_host=True):
        if on:
            if not self.connected:
                log("尚未连接到主机，无法切换")
                return
            try:
                for d in self.devices.values():
                    if d.forward:
                        d.grab(True)
            except OSError as e:
                for d in self.devices.values():
                    try:
                        d.grab(False)
                    except OSError:
                        pass
                log(f"独占输入设备失败（{e}），保持在本机")
                return
            self.reset_hid_state()
            self.forwarding = True
            what = {"keyboard": "键盘", "mouse": "鼠标"}.get(self.args.only, "键鼠")
            log(f">>> {what}已切到主机 {self.peer}（再按 {self.args.toggle_name} 切回）")
        else:
            for d in self.devices.values():
                try:
                    d.grab(False)
                except OSError:
                    pass
            self.forwarding = False
            self.reset_hid_state()
            if notify_host and self.connected:
                self.send_release_all()
            log("<<< 键鼠已切回本机")

    def handle_event(self, type_, code, value):
        if type_ == EV_KEY and code == self.toggle_code:
            if value == 0:  # 在松开时切换，避免另一侧收到不成对的按下/松开
                self.set_forwarding(not self.forwarding)
            return
        if not self.forwarding:
            return
        if type_ == EV_KEY:
            if value == 2:  # 自动重复由主机自己做
                return
            if BTN_LEFT <= code <= BTN_EXTRA:
                bit = 1 << (code - BTN_LEFT)
                self.btns = (self.btns | bit) if value else (self.btns & ~bit)
                self.mouse_dirty = True
            elif code in LINUX_TO_CONSUMER:
                usage = LINUX_TO_CONSUMER[code] if value else 0
                self.send_report(RID_CONSUMER, struct.pack("<H", usage))
            elif code in LINUX_TO_HID:
                u = LINUX_TO_HID[code]
                if 0xE0 <= u <= 0xE7:
                    bit = 1 << (u - 0xE0)
                    self.kb_mods = (self.kb_mods | bit) if value else (self.kb_mods & ~bit)
                elif value:
                    if u not in self.kb_keys and len(self.kb_keys) < 6:
                        self.kb_keys.append(u)
                elif u in self.kb_keys:
                    self.kb_keys.remove(u)
                self.kb_dirty = True
            elif self.verbose:
                log(f"未映射的按键 code={code}")
        elif type_ == EV_REL:
            if code in self.rel:
                self.rel[code] += value
                self.mouse_dirty = True
        elif type_ == EV_SYN and code == SYN_REPORT:
            self.flush()

    def flush(self):
        if self.kb_dirty:
            self.kb_dirty = False
            keys = self.kb_keys + [0] * (6 - len(self.kb_keys))
            self.send_report(RID_KEYBOARD, bytes([self.kb_mods, 0] + keys))
        if self.mouse_dirty:
            self.mouse_dirty = False
            r = self.rel
            while True:
                x, y, w, h = clamp8(r[REL_X]), clamp8(r[REL_Y]), clamp8(r[REL_WHEEL]), clamp8(r[REL_HWHEEL])
                self.send_report(RID_MOUSE, struct.pack("<Bbbbb", self.btns, x, y, w, h))
                r[REL_X] -= x
                r[REL_Y] -= y
                r[REL_WHEEL] -= w
                r[REL_HWHEEL] -= h
                if not any(r.values()):
                    break

    # ---------------- 生命周期
    def run(self):
        self.setup_bluez()
        self.setup_listeners()
        self.discover_devices()
        if not self.devices:
            log("警告：没有找到任何输入设备（/dev/input/by-id/*-event-kbd|mouse），可用 --dev 指定")
        if not self.args.dev:
            GLib.timeout_add_seconds(5, self.discover_devices)  # 热插拔
        if self.host:
            self.schedule_connect(1)
        for sig in (signal.SIGINT, signal.SIGTERM):
            GLib.unix_signal_add(GLib.PRIORITY_HIGH, sig, self.quit)
        try:
            self.loop.run()
        finally:
            self.cleanup()

    def quit(self):
        log("退出")
        self.loop.quit()
        return False

    def cleanup(self):
        if self.forwarding:
            self.set_forwarding(False)
        for d in list(self.devices.values()):
            d.close()
        self.abort_outbound()
        self.drop_connection("程序退出", reconnect=False)
        for s in getattr(self, "listeners", []):
            s.close()
        # Agent / Profile 会随本进程的 D-Bus 连接断开而被 BlueZ 自动注销


def parse_args():
    last_host = None
    try:
        with open(LAST_HOST_FILE) as f:
            last_host = f.read().strip() or None
    except OSError:
        pass

    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--pair", nargs="?", const=120, type=int, metavar="SECONDS",
                   help="打开配对窗口（可发现+可配对），默认 120 秒，0 = 不限时")
    p.add_argument("--host", metavar="MAC", default=last_host,
                   help=f"主动重连的主机 MAC（默认读取 state/last-host{': ' + last_host if last_host else ''}）")
    p.add_argument("--adapter", metavar="hciN", help="使用的蓝牙适配器（默认第一个）")
    p.add_argument("--name", default="Linux HID Relay", help="对外显示的设备名")
    p.add_argument("--toggle", default="KEY_SCROLLLOCK",
                   help="切换键鼠归属的按键，按键名或 evdev 数字码（默认 KEY_SCROLLLOCK）")
    p.add_argument("--dev", action="append", default=[], metavar="PATH",
                   help="要转发的 /dev/input/eventN（可多次；默认自动选择 by-id 下的 *-event-kbd/*-event-mouse）")
    p.add_argument("--only", choices=["keyboard", "mouse"],
                   help="只转发键盘或只转发鼠标；只转发鼠标时键盘仍会被读取以响应热键，但不独占")
    p.add_argument("-v", "--verbose", action="store_true", help="打印协议细节")
    p.add_argument("--test", action="store_true", help="连上主机后自动发送一段合成鼠标移动，验证链路")
    a = p.parse_args()

    a.toggle_name = a.toggle
    if a.toggle.isdigit():
        a.toggle = int(a.toggle)
    elif a.toggle.upper() in KEY_NAMES:
        a.toggle = KEY_NAMES[a.toggle.upper()]
    else:
        p.error(f"未知的 --toggle 按键 {a.toggle}，可用: {', '.join(KEY_NAMES)} 或数字码")
    if a.host:
        a.host = a.host.upper()
    return a


if __name__ == "__main__":
    Relay(parse_args()).run()
