#!/usr/bin/env bash
# DFPanel Agent 一键安装脚本（Linux / macOS）
# 用法：
#   安装：
#     curl -fsSL http://<panel>:7226/install.sh | sudo bash -s -- \
#       --panel http://<panel>:7226 --node-key <KEY> --secret <SECRET> --roles frps,frpc
#   卸载：
#     curl -fsSL http://<panel>:7226/install.sh | sudo bash -s -- --uninstall [--purge]
set -euo pipefail

PANEL=""
NODE_KEY=""
NODE_SECRET=""
ROLES="frpc"
RUNTIME="process"
# 期望的 frp 版本，由面板在安装命令里带上；留空表示不指定，Agent 启动时取面板的 latest
FRP_VERSION=""
INSTALL_DIR="/usr/local/bin"
CONF_DIR="/etc/dfpanel-agent"
DATA_DIR="/var/lib/dfpanel-agent"
UNINSTALL=0
PURGE=0

usage() {
  cat <<'EOF'
用法:
  安装: install.sh --panel <面板地址> --node-key <安装令牌> --secret <密钥> [--roles frps,frpc] [--runtime process|docker] [--frp-version x.y.z]
  卸载: install.sh --uninstall [--purge]
    --uninstall  停止托管的 frp 实例、注销服务、删除配置与二进制；默认保留数据目录
    --purge      卸载时连数据目录一起删（frp 二进制缓存等）
EOF
}

# 卸载：Agent 退出不会带走自己拉起的 frp，所以这里要显式收拾干净。
# 顺序是先停服务（避免它又把实例拉起来），再按 pid 文件停实例，最后删文件。
do_uninstall() {
  echo "==> 停止并注销服务"

  if [[ "$OS" == "linux" ]] && command -v systemctl >/dev/null 2>&1; then
    systemctl disable --now dfpanel-agent >/dev/null 2>&1 || true
    rm -f /etc/systemd/system/dfpanel-agent.service
    systemctl daemon-reload >/dev/null 2>&1 || true
  elif [[ "$OS" == "darwin" ]]; then
    PLIST="$HOME/Library/LaunchAgents/com.dfpanel.agent.plist"
    launchctl unload -w "$PLIST" >/dev/null 2>&1 || true
    rm -f "$PLIST"
  else
    # 无 systemd 时脚本是 nohup 拉起来的，按命令行匹配收拾
    pkill -f "$INSTALL_DIR/dfpanel-agent" >/dev/null 2>&1 || true
  fi

  echo "==> 停止托管的 frp 实例"
  local pidfile pid cmdline
  for pidfile in "$DATA_DIR"/*.pid; do
    [ -f "$pidfile" ] || continue
    pid="$(tr -d ' \n\r' < "$pidfile" 2>/dev/null || true)"
    rm -f "$pidfile"
    [ -n "$pid" ] || continue
    # 先确认这个 pid 确实还是 frp，避免 pid 被复用后误杀别的进程
    cmdline="$(ps -p "$pid" -o command= 2>/dev/null || true)"
    case "$cmdline" in
      *frps* | *frpc*) kill "$pid" 2>/dev/null || true ;;
    esac
  done

  if command -v docker >/dev/null 2>&1; then
    ids="$(docker ps -aq --filter 'name=dfpanel-frps-' --filter 'name=dfpanel-frpc-' 2>/dev/null || true)"
    if [[ -n "$ids" ]]; then
      # shellcheck disable=SC2086
      docker rm -f $ids >/dev/null 2>&1 || true
    fi
  fi

  echo "==> 删除配置与二进制"
  rm -f "$INSTALL_DIR/dfpanel-agent" "$CONF_DIR/agent.json"
  rmdir "$CONF_DIR" >/dev/null 2>&1 || true

  if [[ "$PURGE" -eq 1 ]]; then
    rm -rf "$DATA_DIR"
    echo "==> 数据目录已删除：$DATA_DIR"
  else
    echo "==> 数据目录保留：$DATA_DIR（要一起删就加 --purge）"
  fi

  echo "==> 卸载完成（记得在面板「Agent 管理」里把这条记录删掉）"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --panel)      PANEL="$2"; shift 2 ;;
    --node-key)   NODE_KEY="$2"; shift 2 ;;
    --secret)     NODE_SECRET="$2"; shift 2 ;;
    --roles)      ROLES="$2"; shift 2 ;;
    --runtime)    RUNTIME="$2"; shift 2 ;;
    --frp-version) FRP_VERSION="$2"; shift 2 ;;
    --uninstall)  UNINSTALL=1; shift ;;
    --purge)      PURGE=1; shift ;;
    -h | --help)  usage; exit 0 ;;
    *) echo "未知参数: $1" >&2; usage; exit 1 ;;
  esac
done

if [[ "$UNINSTALL" -eq 0 ]]; then
  if [[ -z "$PANEL" || -z "$NODE_KEY" || -z "$NODE_SECRET" ]]; then
    echo "--panel / --node-key / --secret 均为必填" >&2
    usage
    exit 1
  fi
fi
PANEL="${PANEL%/}"

if [[ "$(id -u)" -ne 0 ]]; then
  echo "请使用 root 或 sudo 运行本脚本" >&2
  exit 1
fi

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH_RAW="$(uname -m)"
case "$ARCH_RAW" in
  x86_64 | amd64)   ARCH="amd64" ;;
  aarch64 | arm64)  ARCH="arm64" ;;
  armv7l | armv6l)  ARCH="arm" ;;
  *)                ARCH="$ARCH_RAW" ;;
esac

# macOS 无 /etc 下写配置的惯例，改用 /usr/local/etc
if [[ "$OS" == "darwin" ]]; then
  CONF_DIR="/usr/local/etc/dfpanel-agent"
  DATA_DIR="/usr/local/var/dfpanel-agent"
fi

if [[ "$UNINSTALL" -eq 1 ]]; then
  echo "==> 卸载 DFPanel Agent（$OS）"
  do_uninstall
  exit 0
fi

echo "==> 操作系统: $OS/$ARCH，角色: $ROLES，运行时: $RUNTIME"
mkdir -p "$INSTALL_DIR" "$CONF_DIR" "$DATA_DIR"

echo "==> 下载 Agent 二进制"
curl -fsSL "$PANEL/downloads/agent/$OS/$ARCH" -o "$INSTALL_DIR/dfpanel-agent"
chmod +x "$INSTALL_DIR/dfpanel-agent"

echo "==> 写入 Agent 配置 $CONF_DIR/agent.json"
cat > "$CONF_DIR/agent.json" <<EOF
{
  "panel_url": "$PANEL",
  "node_key": "$NODE_KEY",
  "secret": "$NODE_SECRET",
  "roles": "$ROLES",
  "runtime": "$RUNTIME",
  "frp_version": "$FRP_VERSION",
  "data_dir": "$DATA_DIR"
}
EOF
chmod 600 "$CONF_DIR/agent.json"

# frp 二进制不在这里预置：一律由 Agent 启动时按上面的版本从面板取，
# 面板没备好会明确报错提示去「设置 → frp 二进制」下载，不在这里静默失败。

if [[ "$OS" == "linux" ]] && command -v systemctl >/dev/null 2>&1; then
  echo "==> 注册 systemd 服务"
  cat > /etc/systemd/system/dfpanel-agent.service <<EOF
[Unit]
Description=DFPanel Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/dfpanel-agent --config $CONF_DIR/agent.json
Restart=always
RestartSec=5
WorkingDirectory=$DATA_DIR

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now dfpanel-agent
  echo "==> 安装完成，服务状态："
  systemctl --no-pager status dfpanel-agent | head -n 12 || true
elif [[ "$OS" == "darwin" ]]; then
  echo "==> 注册 launchd 服务"
  PLIST="$HOME/Library/LaunchAgents/com.dfpanel.agent.plist"
  mkdir -p "$HOME/Library/LaunchAgents"
  cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.dfpanel.agent</string>
  <key>ProgramArguments</key>
  <array>
    <string>$INSTALL_DIR/dfpanel-agent</string>
    <string>--config</string>
    <string>$CONF_DIR/agent.json</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>$DATA_DIR/agent.log</string>
  <key>StandardErrorPath</key><string>$DATA_DIR/agent.err.log</string>
</dict>
</plist>
EOF
  launchctl unload "$PLIST" 2>/dev/null || true
  launchctl load -w "$PLIST"
  echo "==> 安装完成（launchd: com.dfpanel.agent）"
else
  echo "==> 未检测到 systemd，直接后台启动"
  nohup "$INSTALL_DIR/dfpanel-agent" --config "$CONF_DIR/agent.json" >>"$DATA_DIR/agent.log" 2>&1 &
  echo "==> 安装完成"
fi
