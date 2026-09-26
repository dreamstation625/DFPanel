#!/usr/bin/env bash
# DFPanel Agent 一键安装脚本（Linux / macOS）
# 用法：
#   安装：
#     curl -fsSL http://<panel>:7226/install.sh | sudo bash -s -- \
#       --panel http://<panel>:7226 --node-key <KEY> --secret <SECRET> --roles frpc
#   卸载：
#     curl -fsSL http://<panel>:7226/install.sh | sudo bash -s -- --uninstall --instance <KEY> [--purge]
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
INSTANCE=""
SERVICE_NAME=""
UNINSTALL=0
PURGE=0

usage() {
  cat <<'EOF'
用法:
  安装: install.sh --panel <面板地址> --node-key <安装令牌> --secret <密钥> [--roles frps|frpc] [--runtime process|docker] [--frp-version x.y.z]
  卸载: install.sh --uninstall --instance <安装令牌> [--purge]
    --uninstall  停止托管的 frp 实例、注销服务、删除配置与二进制；默认保留数据目录
    --purge      卸载时连数据目录一起删（frp 二进制缓存等）
EOF
}

# 卸载：Agent 退出不会带走自己拉起的 frp，所以这里要显式收拾干净。
# 顺序是先停服务（避免它又把实例拉起来），再按 pid 文件停实例，最后删文件。
do_uninstall() {
  echo "==> 停止并注销服务"

  if [[ "$OS" == "linux" ]] && command -v systemctl >/dev/null 2>&1; then
    systemctl disable --now "$SERVICE_NAME" >/dev/null 2>&1 || true
    rm -f "/etc/systemd/system/$SERVICE_NAME.service"
    systemctl daemon-reload >/dev/null 2>&1 || true
  elif [[ "$OS" == "darwin" ]]; then
    PLIST="$HOME/Library/LaunchAgents/com.dfpanel.agent.$INSTANCE.plist"
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
    local config container
    for config in "$DATA_DIR"/frps-*.json "$DATA_DIR"/frpc-*.json; do
      [[ -f "$config" ]] || continue
      container="dfpanel-$(basename "$config" .json)"
      docker rm -f "$container" >/dev/null 2>&1 || true
    done
  fi

  echo "==> 删除配置与二进制"
  rm -f "$INSTALL_DIR/dfpanel-agent" "$CONF_DIR/agent.json"
  rmdir "$CONF_DIR" >/dev/null 2>&1 || true
  rmdir "$INSTALL_DIR" >/dev/null 2>&1 || true

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
    --instance)   INSTANCE="$2"; shift 2 ;;
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
if [[ -z "$INSTANCE" ]]; then INSTANCE="$NODE_KEY"; fi
if [[ ! "$INSTANCE" =~ ^[A-Za-z0-9_-]{8,64}$ ]]; then
  echo "请提供有效的 --instance（安装时的 nodeKey），卸载不能省略" >&2
  exit 1
fi
if [[ "$UNINSTALL" -eq 0 && "$ROLES" != "frps" && "$ROLES" != "frpc" ]]; then
  echo "--roles 只能是 frps 或 frpc" >&2
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
INSTALL_DIR="/usr/local/lib/dfpanel-agent/$INSTANCE"
CONF_DIR="$CONF_DIR/$INSTANCE"
DATA_DIR="$DATA_DIR/$INSTANCE"
SERVICE_NAME="dfpanel-agent-$INSTANCE"

if [[ "$UNINSTALL" -eq 1 ]]; then
  echo "==> 卸载 DFPanel Agent（$OS）"
  do_uninstall
  exit 0
fi

echo "==> 操作系统: $OS/$ARCH，角色: $ROLES，运行时: $RUNTIME"
mkdir -p "$INSTALL_DIR" "$CONF_DIR" "$DATA_DIR"

echo "==> 下载 Agent 二进制"
AGENT_TMP="$(mktemp "$INSTALL_DIR/.dfpanel-agent.XXXXXX")"
HEADERS_TMP="$(mktemp)"
trap 'rm -f "$AGENT_TMP" "$HEADERS_TMP"' EXIT
curl -fsSL -D "$HEADERS_TMP" "$PANEL/downloads/agent/$OS/$ARCH" -o "$AGENT_TMP"
EXPECTED_SHA="$(awk 'tolower($1)=="x-agent-sha256:" { gsub("\r", "", $2); print $2 }' "$HEADERS_TMP" | tail -n 1)"
[[ "$EXPECTED_SHA" =~ ^[0-9a-fA-F]{64}$ ]] || { echo "面板未返回有效的 Agent SHA256" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL_SHA="$(sha256sum "$AGENT_TMP" | awk '{print $1}')"
else
  ACTUAL_SHA="$(shasum -a 256 "$AGENT_TMP" | awk '{print $1}')"
fi
[[ "$(printf '%s' "$ACTUAL_SHA" | tr '[:upper:]' '[:lower:]')" == "$(printf '%s' "$EXPECTED_SHA" | tr '[:upper:]' '[:lower:]')" ]] || { echo "Agent 下载文件 SHA256 不匹配" >&2; exit 1; }
chmod +x "$AGENT_TMP"
EXPECTED_VERSION="$(awk 'tolower($1)=="x-agent-version:" { gsub("\r", "", $2); print $2 }' "$HEADERS_TMP" | tail -n 1)"
if [[ -n "$EXPECTED_VERSION" ]]; then
  VERSION_OUTPUT="$("$AGENT_TMP" -version 2>&1)" || { echo "下载的 Agent 无法运行" >&2; exit 1; }
  [[ "$VERSION_OUTPUT" == *"dfpanel-agent $EXPECTED_VERSION"* ]] || { echo "Agent 程序版本与面板分发版本不一致" >&2; exit 1; }
fi
mv -f "$AGENT_TMP" "$INSTALL_DIR/dfpanel-agent"
trap - EXIT
rm -f "$HEADERS_TMP"

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
  cat > "/etc/systemd/system/$SERVICE_NAME.service" <<EOF
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
  systemctl enable "$SERVICE_NAME"
  systemctl restart "$SERVICE_NAME"
  echo "==> 安装完成，服务状态："
  systemctl --no-pager status "$SERVICE_NAME" | head -n 12 || true
elif [[ "$OS" == "darwin" ]]; then
  echo "==> 注册 launchd 服务"
  PLIST="$HOME/Library/LaunchAgents/com.dfpanel.agent.$INSTANCE.plist"
  mkdir -p "$HOME/Library/LaunchAgents"
  cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.dfpanel.agent.$INSTANCE</string>
  <key>ProgramArguments</key>
  <array>
    <string>$INSTALL_DIR/dfpanel-agent</string>
    <string>--config</string>
    <string>$CONF_DIR/agent.json</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>$DATA_DIR/agent.stdout.log</string>
  <key>StandardErrorPath</key><string>$DATA_DIR/agent.err.log</string>
</dict>
</plist>
EOF
  launchctl unload "$PLIST" 2>/dev/null || true
  launchctl load -w "$PLIST"
  echo "==> 安装完成（launchd: com.dfpanel.agent.$INSTANCE）"
else
  echo "==> 未检测到 systemd，直接后台启动"
  pkill -f "$INSTALL_DIR/dfpanel-agent" >/dev/null 2>&1 || true
  nohup "$INSTALL_DIR/dfpanel-agent" --config "$CONF_DIR/agent.json" >>"$DATA_DIR/agent.stdio.log" 2>&1 &
  echo "==> 安装完成"
fi
echo "==> Agent 日志：$DATA_DIR/agent.log"
