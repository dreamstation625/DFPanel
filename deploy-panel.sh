#!/usr/bin/env bash
# Linux 面板部署入口：Docker Compose 或 systemd 管理的二进制。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="/var/lib/dfpanel"
BIN_PATH="/usr/local/bin/dfpanel"
UNIT_PATH="/etc/systemd/system/dfpanel.service"
COMPOSE_DIR="/opt/dfpanel"
COMPOSE_PATH="$COMPOSE_DIR/compose.yml"
REPOSITORY="dreamstation625/DFPanel"

MODE=""
PUBLIC_URL=""
LISTEN=":7226"
BINARY_PATH=""
AGENT_BUNDLE=""
AGENT_BUNDLE_PATH=""
AGENT_VERSION=""
IMAGE=""
VERSION=""
GITHUB_PROXY=""
WORK_DIR=""

usage() {
  cat <<'EOF'
用法：sudo ./deploy-panel.sh [--mode docker|binary] [--public-url URL] [选项]

选项：
  --mode MODE      docker 或 binary；交互运行可选择，非交互运行必须指定
  --public-url URL Agent 可访问的面板地址；交互运行可输入，非交互运行必须指定
  --binary PATH    使用本地面板二进制；不指定时先下载对应版本的 GitHub Release
  --agent-bundle PATH  使用本地 Agent 全平台包；不指定时从同一 Release 下载
  --version VER    指定 Release / Docker 镜像版本；二进制模式默认使用最新正式版
  --github-proxy URL  GitHub 加速前缀，需支持代理完整 URL（API 与 Release 文件）
  --image IMAGE    Docker 镜像；默认 dreamstation625/dfpanel:latest；显式指定版本时使用该版本标签
  --listen :PORT   面板监听端口，默认 :7226
  -h, --help       显示帮助

Release 下载失败时，二进制模式可回退到同目录中版本相符的 output/dfpanel-linux-<架构>。
数据保存在 /var/lib/dfpanel；重复执行可更新程序，部署脚本不会删除数据。
EOF
}

fail() { printf '错误：%s\n' "$*" >&2; exit 1; }
info() { printf '==> %s\n' "$*"; }

wait_for_panel() {
  local port="${LISTEN#:}"
  local attempt
  for attempt in {1..15}; do
    if curl -fsS --max-time 2 "http://127.0.0.1:$port/api/init-status" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode)       [[ $# -ge 2 ]] || fail "--mode 缺少值"; MODE="$2"; shift 2 ;;
    --public-url) [[ $# -ge 2 ]] || fail "--public-url 缺少值"; PUBLIC_URL="$2"; shift 2 ;;
    --listen)     [[ $# -ge 2 ]] || fail "--listen 缺少值"; LISTEN="$2"; shift 2 ;;
    --binary)     [[ $# -ge 2 ]] || fail "--binary 缺少值"; BINARY_PATH="$2"; shift 2 ;;
    --agent-bundle) [[ $# -ge 2 ]] || fail "--agent-bundle 缺少值"; AGENT_BUNDLE="$2"; shift 2 ;;
    --image)      [[ $# -ge 2 ]] || fail "--image 缺少值"; IMAGE="$2"; shift 2 ;;
    --version)    [[ $# -ge 2 ]] || fail "--version 缺少值"; VERSION="$2"; shift 2 ;;
    --github-proxy) [[ $# -ge 2 ]] || fail "--github-proxy 缺少值"; GITHUB_PROXY="$2"; shift 2 ;;
    -h|--help)    usage; exit 0 ;;
    *)            fail "未知参数：$1" ;;
  esac
done

[[ "$(uname -s)" == "Linux" ]] || fail "此脚本仅适用于 Linux；Windows 请运行 deploy-panel.ps1"
if [[ -z "$MODE" ]]; then
  [[ -t 0 ]] || fail "非交互运行时请指定 --mode docker 或 --mode binary"
  printf '选择部署方式：1) Docker  2) 二进制 + systemd\n'
  read -r -p '请输入 1 或 2：' choice
  case "$choice" in 1) MODE="docker" ;; 2) MODE="binary" ;; *) fail "无效选项" ;; esac
fi
[[ "$MODE" == "docker" || "$MODE" == "binary" ]] || fail "部署方式只能是 docker 或 binary"

if [[ -z "$PUBLIC_URL" ]]; then
  [[ -t 0 ]] || fail "非交互运行时请指定 --public-url"
  read -r -p 'Agent 可访问的面板地址（例如 http://192.168.1.10:7226）：' PUBLIC_URL
fi
# URL 将写入 systemd 或 Compose 配置，只接受无路径、凭据与控制字符的 HTTP 地址。
[[ "$PUBLIC_URL" =~ ^https?://([a-zA-Z0-9._-]+|\[[0-9a-fA-F:]+\])(:[0-9]{1,5})?/?$ ]] ||
  fail "--public-url 应为 http(s)://主机[:端口]，不能包含路径或凭据"
PUBLIC_URL="${PUBLIC_URL%/}"
[[ "$LISTEN" =~ ^:[0-9]{1,5}$ ]] || fail "--listen 应为 :端口，例如 :7226"
if [[ -n "$GITHUB_PROXY" ]]; then
  [[ "$GITHUB_PROXY" =~ ^https?://[^/?#@[:space:]]+(/[^?#[:space:]]*)?$ ]] ||
    fail "--github-proxy 应为 http(s)://主机[:端口][/路径]，不能包含凭据、查询参数或片段"
  GITHUB_PROXY="${GITHUB_PROXY%/}"
fi
command -v curl >/dev/null 2>&1 || fail "需要 curl 执行下载和启动检查"
[[ "$(id -u)" -eq 0 ]] || fail "需要 root 权限，请使用 sudo 运行"

if [[ -n "$VERSION" ]]; then
  [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$ ]] ||
    fail "版本号格式不合法：$VERSION"
fi

case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) fail "不支持的架构：$(uname -m)" ;;
esac

prepare_work_dir() {
  WORK_DIR="$(mktemp -d)"
  trap '[[ -z "$WORK_DIR" ]] || rm -rf -- "$WORK_DIR"' EXIT
}

github_url() {
  if [[ -n "$GITHUB_PROXY" ]]; then
    printf '%s/%s' "$GITHUB_PROXY" "$1"
  else
    printf '%s' "$1"
  fi
}

latest_stable_version() {
  local response version api_url
  api_url="https://api.github.com/repos/$REPOSITORY/releases/latest"
  if ! response="$(curl -fsSL --retry 2 --connect-timeout 10 --max-time 30 \
    -H 'Accept: application/vnd.github+json' -H 'User-Agent: DFPanel-deploy' \
    "$(github_url "$api_url")" 2>/dev/null)"; then
    [[ -n "$GITHUB_PROXY" ]] || return 1
    printf '提示：加速源查询最新版本失败，尝试直接访问 GitHub API；也可用 --version 跳过查询。\n' >&2
    response="$(curl -fsSL --retry 1 --connect-timeout 5 --max-time 15 \
      -H 'Accept: application/vnd.github+json' -H 'User-Agent: DFPanel-deploy' \
      "$api_url")" || return 1
  fi
  version="$(printf '%s\n' "$response" | sed -nE 's/.*"tag_name"[[:space:]]*:[[:space:]]*"v([^"]+)".*/\1/p')"
  [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || return 1
  printf '%s' "$version"
}

binary_version() {
  local output
  output="$("$1" -version 2>/dev/null)" || return 1
  [[ "$output" =~ ^dfpanel[[:space:]]+([0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?)[[:space:]]*$ ]] || return 1
  printf '%s' "${BASH_REMATCH[1]}"
}

download_release() {
  local archive="dfpanel-${VERSION}-linux-${ARCH}.tar.gz"
  local base="$(github_url "https://github.com/$REPOSITORY/releases/download/v$VERSION")"
  local archive_path="$WORK_DIR/$archive"
  local checksums="$WORK_DIR/checksums.txt"
  [[ -n "$VERSION" ]] || return 1

  # gh 支持已登录的私有仓库；未安装或未登录时尝试公开 Release 地址。
  if [[ -z "$GITHUB_PROXY" ]] && command -v gh >/dev/null 2>&1; then
    if [[ -s "$checksums" ]]; then
      gh release download "v$VERSION" -R "$REPOSITORY" -p "$archive" -D "$WORK_DIR" >/dev/null 2>&1 || true
    else
      gh release download "v$VERSION" -R "$REPOSITORY" -p "$archive" -p checksums.txt -D "$WORK_DIR" >/dev/null 2>&1 || true
    fi
  fi
  if [[ ! -s "$archive_path" ]] &&
    ! curl -fsSL --retry 2 --connect-timeout 10 --max-time 180 "$base/$archive" -o "$archive_path"; then
    return 1
  fi
  if [[ ! -s "$checksums" ]] &&
    ! curl -fsSL --retry 2 --connect-timeout 10 --max-time 60 "$base/checksums.txt" -o "$checksums"; then
    fail "Release 归档已下载，但无法获取校验文件 checksums.txt"
  fi
  local expected
  expected="$(awk -v name="$archive" '$2 == name { print $1; exit }' "$checksums")"
  [[ "$expected" =~ ^[0-9a-fA-F]{64}$ ]] || fail "校验文件中没有 $archive 的 SHA256"
  local actual
  actual="$(sha256sum "$archive_path" | awk '{ print $1 }')"
  [[ "${actual,,}" == "${expected,,}" ]] || fail "Release 归档 SHA256 校验失败"

  tar -xzf "$archive_path" -C "$WORK_DIR" "dfpanel-${VERSION}-linux-${ARCH}/dfpanel"
  BINARY_PATH="$WORK_DIR/dfpanel-${VERSION}-linux-${ARCH}/dfpanel"
  info "已下载并校验 Release：$archive"
}

download_agent_bundle() {
  local archive="dfpanel-agent-bundle-${VERSION}.tar.gz"
  local base="$(github_url "https://github.com/$REPOSITORY/releases/download/v$VERSION")"
  local archive_path="$WORK_DIR/$archive"
  local checksums="$WORK_DIR/checksums.txt"
  [[ -n "$VERSION" ]] || return 1
  if [[ -z "$GITHUB_PROXY" ]] && command -v gh >/dev/null 2>&1; then
    if [[ -s "$checksums" ]]; then
      gh release download "v$VERSION" -R "$REPOSITORY" -p "$archive" -D "$WORK_DIR" >/dev/null 2>&1 || true
    else
      gh release download "v$VERSION" -R "$REPOSITORY" -p "$archive" -p checksums.txt -D "$WORK_DIR" >/dev/null 2>&1 || true
    fi
  fi
  if [[ ! -s "$archive_path" ]] &&
    ! curl -fsSL --retry 2 --connect-timeout 10 --max-time 300 "$base/$archive" -o "$archive_path"; then
    return 1
  fi
  if [[ ! -s "$checksums" ]] &&
    ! curl -fsSL --retry 2 --connect-timeout 10 --max-time 60 "$base/checksums.txt" -o "$checksums"; then
    fail "Agent 包已下载，但无法获取 checksums.txt"
  fi
  local expected actual
  expected="$(awk -v name="$archive" '$2 == name { print $1; exit }' "$checksums")"
  [[ "$expected" =~ ^[0-9a-fA-F]{64}$ ]] || fail "校验文件中没有 $archive 的 SHA256"
  actual="$(sha256sum "$archive_path" | awk '{ print $1 }')"
  [[ "${actual,,}" == "${expected,,}" ]] || fail "Agent 包 SHA256 校验失败"
  AGENT_BUNDLE_PATH="$archive_path"
  info "已下载并校验 Agent 全平台包：$archive"
}

if [[ "$MODE" == "docker" ]]; then
  [[ -z "$BINARY_PATH" ]] || fail "--binary 仅适用于二进制模式"
  [[ -z "$AGENT_BUNDLE" ]] || fail "--agent-bundle 仅适用于二进制模式"
  command -v docker >/dev/null 2>&1 || fail "未安装 Docker"
  docker compose version >/dev/null 2>&1 || fail "未安装 Docker Compose 插件"
  if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet dfpanel; then
    fail "检测到运行中的 dfpanel.service，请先停止二进制部署，避免端口冲突"
  fi
  if [[ -z "$IMAGE" ]]; then
    IMAGE="dreamstation625/dfpanel:${VERSION:-latest}"
  fi
  if [[ -n "$GITHUB_PROXY" ]]; then
    info "Docker 模式从 Docker Hub 拉取镜像，--github-proxy 不影响镜像下载"
  fi
  [[ "$IMAGE" =~ ^[a-zA-Z0-9._/:@-]+$ ]] || fail "镜像名称包含不支持的字符"
  mkdir -p "$DATA_DIR" "$COMPOSE_DIR"
  chmod 700 "$DATA_DIR"
  if [[ -e "$COMPOSE_PATH" ]] && ! head -n 1 "$COMPOSE_PATH" | grep -qx '# Generated by deploy-panel.sh'; then
    fail "$COMPOSE_PATH 已存在且不是本脚本生成的配置，请手动处理"
  fi
  cat > "$COMPOSE_PATH.tmp" <<EOF
# Generated by deploy-panel.sh
services:
  dfpanel:
    image: "$IMAGE"
    container_name: dfpanel
    restart: unless-stopped
    network_mode: host
    volumes:
      - "$DATA_DIR:/data"
      - /etc/machine-id:/host/etc/machine-id:ro
    environment:
      DFPANEL_LISTEN: "$LISTEN"
      DFPANEL_DATA_DIR: "/data"
      DFPANEL_PUBLIC_URL: "$PUBLIC_URL"
EOF
  info "拉取镜像并启动面板：$IMAGE"
  docker compose -f "$COMPOSE_PATH.tmp" pull
  mv -f "$COMPOSE_PATH.tmp" "$COMPOSE_PATH"
  docker compose -f "$COMPOSE_PATH" up -d
  wait_for_panel || fail "面板未通过启动检查，请查看 docker logs dfpanel"
  docker compose -f "$COMPOSE_PATH" ps
  info "面板地址：$PUBLIC_URL；数据目录：$DATA_DIR"
  exit 0
fi

command -v systemctl >/dev/null 2>&1 || fail "二进制模式需要 systemd"
if command -v docker >/dev/null 2>&1 &&
  docker ps --format '{{.Names}}' 2>/dev/null | grep -qx dfpanel; then
  fail "检测到运行中的 dfpanel 容器，请先停止容器部署，避免端口冲突"
fi
prepare_work_dir
if [[ -z "$VERSION" ]]; then
  if [[ -n "$BINARY_PATH" ]]; then
    [[ -s "$BINARY_PATH" ]] || fail "本地二进制不存在或为空：$BINARY_PATH"
    VERSION="$(binary_version "$BINARY_PATH")" || fail "无法读取本地面板二进制版本，请使用 --version 指定版本"
    info "使用本地二进制版本：$VERSION"
  else
    VERSION="$(latest_stable_version)" || fail "无法获取 GitHub 最新正式版；可使用 --version 指定版本，或使用 --binary 指定本地二进制"
    info "最新正式版：$VERSION"
  fi
fi
if [[ -n "$BINARY_PATH" ]]; then
  [[ -f "$BINARY_PATH" ]] || fail "本地二进制不存在：$BINARY_PATH"
  info "使用指定的本地二进制：$BINARY_PATH"
else
  if ! download_release; then
    BINARY_PATH="$ROOT/output/dfpanel-linux-$ARCH"
    [[ -f "$BINARY_PATH" ]] ||
      fail "未找到 v$VERSION 的 Release，且本地不存在 $BINARY_PATH；请先发布 Release 或使用 --binary"
    info "Release 不可用，改用本地构建产物：$BINARY_PATH"
  fi
fi
[[ -s "$BINARY_PATH" ]] || fail "二进制文件为空：$BINARY_PATH"
ACTUAL_VERSION="$(binary_version "$BINARY_PATH")" || fail "二进制无法在当前机器运行或版本号无效：$BINARY_PATH"
[[ "$ACTUAL_VERSION" == "$VERSION" ]] || fail "面板二进制版本 $ACTUAL_VERSION 与所选版本 $VERSION 不一致"

if [[ -n "$AGENT_BUNDLE" ]]; then
  [[ -s "$AGENT_BUNDLE" ]] || fail "本地 Agent 包不存在或为空：$AGENT_BUNDLE"
  AGENT_BUNDLE_PATH="$AGENT_BUNDLE"
elif ! download_agent_bundle; then
  AGENT_BUNDLE_PATH="$ROOT/output/dfpanel-agent-bundle-${VERSION}.tar.gz"
  if [[ ! -s "$AGENT_BUNDLE_PATH" ]]; then
    command -v go >/dev/null 2>&1 && [[ -f "$ROOT/go.mod" ]] ||
      fail "没有 Agent 全平台包；请发布 v$VERSION 的 Release，或用 --agent-bundle 指定本地包"
    [[ -f "$ROOT/VERSION" ]] && [[ "$(tr -d ' \n\r' < "$ROOT/VERSION")" == "$VERSION" ]] ||
      fail "当前源码版本与面板版本 $VERSION 不一致，不能从当前源码构建 Agent 包；请使用 --agent-bundle"
    AGENT_BUNDLE_PATH="$WORK_DIR/dfpanel-agent-bundle-${VERSION}.tar.gz"
    info "Release 不可用，从当前源码构建 Agent 全平台包"
    (cd "$ROOT" && go run ./cmd/agentbundle -output "$AGENT_BUNDLE_PATH")
  fi
fi

mkdir -p "$DATA_DIR"
chmod 700 "$DATA_DIR"
AGENT_VERSION="$("$BINARY_PATH" -prepare-agent-bundle "$AGENT_BUNDLE_PATH" "$DATA_DIR")" ||
  fail "Agent 全平台包校验失败，请确认它与面板版本一致"
[[ -n "$AGENT_VERSION" ]] || fail "Agent 全平台包未返回版本号"
if [[ -f "$BIN_PATH" ]]; then cp -p "$BIN_PATH" "$WORK_DIR/old-binary"; fi
if [[ -f "$UNIT_PATH" ]]; then cp -p "$UNIT_PATH" "$WORK_DIR/old-unit"; fi
install -m 0755 "$BINARY_PATH" "$BIN_PATH.new"
mv -f "$BIN_PATH.new" "$BIN_PATH"
cat > "$UNIT_PATH.tmp" <<EOF
[Unit]
Description=DFPanel frp 管理面板
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$DATA_DIR
ExecStart=$BIN_PATH -listen $LISTEN -data $DATA_DIR -public-url $PUBLIC_URL
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
mv -f "$UNIT_PATH.tmp" "$UNIT_PATH"
systemctl daemon-reload
systemctl enable dfpanel >/dev/null
if ! systemctl restart dfpanel || ! systemctl is-active --quiet dfpanel || ! wait_for_panel ||
   ! "$BIN_PATH" -activate-agent-bundle "$DATA_DIR" "$VERSION"; then
  info "启动失败，恢复部署前的二进制与服务文件"
  if [[ -f "$WORK_DIR/old-binary" ]]; then
    cp -p "$WORK_DIR/old-binary" "$BIN_PATH"
  else
    rm -f "$BIN_PATH"
  fi
  if [[ -f "$WORK_DIR/old-unit" ]]; then
    cp -p "$WORK_DIR/old-unit" "$UNIT_PATH"
    systemctl daemon-reload
    systemctl restart dfpanel || true
  else
    systemctl disable --now dfpanel >/dev/null 2>&1 || true
    rm -f "$UNIT_PATH"
    systemctl daemon-reload
  fi
  fail "面板服务未能启动，请查看 journalctl -u dfpanel -n 50"
fi
systemctl --no-pager --full status dfpanel | head -n 12 || true
info "面板地址：$PUBLIC_URL；数据目录：$DATA_DIR；Agent 版本：$AGENT_VERSION"
