#!/usr/bin/env bash
# 启动一个独立的 bluetoothd（禁用 input 插件、使用本目录 main.conf），然后运行 relay.py。
# 不写任何 /etc 文件、不 enable 任何 systemd 单元；Ctrl-C 退出后一切恢复原状。
set -euo pipefail
cd "$(dirname "$(readlink -f "$0")")"

if systemctl is-active --quiet bluetooth; then
    echo "系统的 bluetooth.service 正在运行，会与本脚本启动的 bluetoothd 冲突。" >&2
    echo "请先执行: sudo systemctl stop bluetooth" >&2
    exit 1
fi

if [[ $EUID -ne 0 ]]; then
    exec sudo --preserve-env=BTD_DEBUG "$0" "$@"
fi

BTD=/usr/lib/bluetooth/bluetoothd
mkdir -p state
: > state/bluetoothd.log

args=(-n -P input -f "$PWD/main.conf")
[[ "${BTD_DEBUG:-}" == 1 ]] && args+=(-d)
"$BTD" "${args[@]}" >> state/bluetoothd.log 2>&1 &
BTD_PID=$!
trap 'kill "$BTD_PID" 2>/dev/null; wait "$BTD_PID" 2>/dev/null || true' EXIT

# 等待 org.bluez 出现在 system bus 上
for _ in $(seq 1 50); do
    if busctl --system status org.bluez >/dev/null 2>&1; then break; fi
    if ! kill -0 "$BTD_PID" 2>/dev/null; then
        echo "bluetoothd 启动失败，见 state/bluetoothd.log:" >&2
        cat state/bluetoothd.log >&2
        exit 1
    fi
    sleep 0.2
done

exec_status=0
python3 relay.py "$@" || exec_status=$?
exit "$exec_status"
