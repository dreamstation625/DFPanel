#!/usr/bin/env bash
# DFPanel Agent 一键安装脚本（Linux / macOS）
# 用法：
#   curl -fsSL http://<panel>:8080/install.sh | sudo bash -s -- \
#     --panel http://<panel>:8080 --node-key <KEY> --secret <SECRET> --roles frps,frpc
set -euo pipefail

PANEL=""
NODE_KEY=""
NODE_SECRET=""
ROLES="frpc"
RUNTIME="process"
INSTALL_DIR="/usr/local/bin"
CONF_DIR="/etc/dfpanel-agent"
DATA_DIR="/var/lib/dfpanel-agent"

usage() {
  cat <<'EOF'
用法: install.sh --panel <面板地址> --node-key <安装令牌> --secret <密钥> [--roles frps,frpc] [--runtime process|docker]
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --panel)      PANEL="$2"; shift 2 ;;
    --node-key)   NODE_KEY="$2"; shift 2 ;;
    --secret)     NODE_SECRET="$2"; shift 2 ;;
    --roles)      ROLES="$2"; shift 2 ;;
    --runtime)    RUNTIME="$2"; shift 2 ;;
    -h | --help)  usage; exit 0 ;;
    *) echo "未知参数: $1" >&2; usage; exit 1 ;;
  esac
done

if [[ -z "$PANEL" || -z "$NODE_KEY" || -z "$NODE_SECRET" ]]; then
  echo "--panel / --node-key / --secret 均为必填" >&2
  usage
  exit 1
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
  "data_dir": "$DATA_DIR"
}
EOF
chmod 600 "$CONF_DIR/agent.json"

# 预置 frpc / frps 二进制（缺失时 Agent 运行时会自行从面板补拉，此处失败不阻断）
if echo "$ROLES" | grep -q "frpc"; then
  curl -fsSL "$PANEL/downloads/frpc/latest/$OS/$ARCH" -o "$DATA_DIR/frpc" 2>/dev/null && chmod +x "$DATA_DIR/frpc" || true
fi
if echo "$ROLES" | grep -q "frps"; then
  curl -fsSL "$PANEL/downloads/frps/latest/$OS/$ARCH" -o "$DATA_DIR/frps" 2>/dev/null && chmod +x "$DATA_DIR/frps" || true
fi

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
