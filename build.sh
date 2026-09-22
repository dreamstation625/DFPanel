#!/usr/bin/env bash
# DFPanel 构建脚本：构建前端 -> 编译 Go 服务端（前端产物通过 go:embed 内嵌进二进制）
#
# 用法:
#   ./build.sh                       完整构建：前端 + 后端
#   ./build.sh --skip-frontend       跳过前端构建，复用现有 web/dist
#   ./build.sh --frontend-only       只构建前端
#   ./build.sh --backend-only        只编译服务端
#   ./build.sh --run                 构建完成后启动面板
#   ./build.sh --os linux --arch arm64   交叉编译
#   ./build.sh --listen :9000 --run  指定监听端口并启动
#   ./build.sh --agent              编译 Agent 程序（cmd/agent）
#   ./build.sh --agent --all-platforms   一次编译 Agent 的多平台产物到 dist/
#   ./build.sh --docker             构建面板镜像 dreamstation625/dfpanel:latest
#   ./build.sh --docker --agent     构建 Agent 镜像 dreamstation625/dfpanel-agent:latest
#   ./build.sh --docker --image my/panel:v1   指定镜像标签
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_DIR="$ROOT/web"
DIST_DIR="$WEB_DIR/dist"

TARGET_OS="${GOOS:-$(go env GOOS 2>/dev/null || echo linux)}"
TARGET_ARCH="${GOARCH:-$(go env GOARCH 2>/dev/null || echo amd64)}"

SKIP_FRONTEND=0
FRONTEND_ONLY=0
BACKEND_ONLY=0
RUN=0
AGENT=0
DOCKER=0
ALL_PLATFORMS=0
IMAGE=""
OUT_FILE=""
LISTEN=":7226"
DATA_DIR="./data"
TOKEN_EXPIRE=24

while [[ $# -gt 0 ]]; do
    case "$1" in
        --skip-frontend) SKIP_FRONTEND=1; shift ;;
        --frontend-only) FRONTEND_ONLY=1; shift ;;
        --backend-only)  BACKEND_ONLY=1; shift ;;
        --run)           RUN=1; shift ;;
        --out)           OUT_FILE="$2"; shift 2 ;;
        --os)            TARGET_OS="$2"; shift 2 ;;
        --arch)          TARGET_ARCH="$2"; shift 2 ;;
        --listen)        LISTEN="$2"; shift 2 ;;
        --data)          DATA_DIR="$2"; shift 2 ;;
        --token-expire)  TOKEN_EXPIRE="$2"; shift 2 ;;
        --agent)         AGENT=1; shift ;;
        --docker)        DOCKER=1; shift ;;
        --all-platforms) ALL_PLATFORMS=1; shift ;;
        --image)         IMAGE="$2"; shift 2 ;;
        -h|--help)       sed -n '2,26p' "${BASH_SOURCE[0]}"; exit 0 ;;
        *) echo "未知参数: $1 (使用 --help 查看用法)"; exit 1 ;;
    esac
done

step() { printf '\033[36m==> %s\033[0m\n' "$1"; }
ok()   { printf '\033[32m    %s\033[0m\n' "$1"; }
warn() { printf '\033[33m    [警告] %s\033[0m\n' "$1"; }
has()  { command -v "$1" >/dev/null 2>&1; }

build_frontend() {
    step "构建前端"
    [[ -d "$WEB_DIR" ]] || { echo "未找到前端目录: $WEB_DIR"; exit 1; }

    if ! has npm; then
        if [[ -d "$DIST_DIR" ]]; then
            warn "未检测到 npm，跳过前端构建，复用现有 web/dist"
            return
        fi
        echo "未检测到 npm，且 web/dist 不存在，无法构建前端。请先安装 Node.js (>= 18)。"
        exit 1
    fi

    if [[ ! -d "$WEB_DIR/node_modules" ]]; then
        step "安装前端依赖 (npm install)"
        (cd "$WEB_DIR" && npm install)
    fi

    step "编译前端 (npm run build)"
    (cd "$WEB_DIR" && npm run build)
    ok "前端产物已生成: $DIST_DIR"
}

build_backend() {
    local os="$TARGET_OS" arch="$TARGET_ARCH"
    local pkg="." label="服务端" base="dfpanel"
    if [[ "$AGENT" -eq 1 ]]; then
        pkg="./cmd/agent"; label="Agent"; base="dfpanel-agent"
    fi

    step "编译$label ($os/$arch)"
    has go || { echo "未检测到 go，请先安装 Go (>= 1.21)。"; exit 1; }

    local ext=""
    [[ "$os" == "windows" ]] && ext=".exe"

    if [[ -z "$OUT_FILE" ]]; then
        if [[ "$AGENT" -eq 1 || "$ALL_PLATFORMS" -eq 1 ]]; then
            mkdir -p "$ROOT/dist"
            OUT_FILE="$ROOT/dist/${base}-${os}-${arch}${ext}"
        else
            OUT_FILE="$ROOT/${base}${ext}"
        fi
    fi

    # 版本号统一取自根目录 VERSION 文件，构建时注入到二进制
    local ver="dev"
    [[ -f "$ROOT/VERSION" ]] && ver="$(tr -d ' \n\r' < "$ROOT/VERSION")"
    local ldflags
    if [[ "$AGENT" -eq 1 ]]; then
        ldflags="-X dfpanel/internal/agent.Version=$ver"
    else
        ldflags="-X main.version=$ver"
    fi

    (cd "$ROOT" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
        go build -trimpath -ldflags "$ldflags" -o "$OUT_FILE" "$pkg")
    ok "$label 已生成: $OUT_FILE (版本 $ver)"
}

build_docker() {
    has docker || { echo "未检测到 docker，请先安装 Docker。"; exit 1; }

    local dockerfile="$ROOT/Dockerfile" tag="${IMAGE:-dreamstation625/dfpanel:latest}"
    if [[ "$AGENT" -eq 1 ]]; then
        dockerfile="$ROOT/Dockerfile.agent"
        tag="${IMAGE:-dreamstation625/dfpanel-agent:latest}"
    fi

    step "构建镜像 $tag"
    docker build -f "$dockerfile" -t "$tag" "$ROOT"
    ok "镜像已构建: $tag"
}

if [[ "$BACKEND_ONLY" -eq 0 ]]; then
    if [[ "$SKIP_FRONTEND" -eq 1 ]]; then
        step "跳过前端构建"
        [[ -d "$DIST_DIR" ]] || { echo "web/dist 不存在，无法跳过前端构建。请先执行 ./build.sh --frontend-only"; exit 1; }
    else
        build_frontend
    fi
fi

if [[ "$FRONTEND_ONLY" -eq 1 ]]; then
    echo ""
    echo "前端构建完成。"
    exit 0
fi

if [[ "$DOCKER" -eq 1 ]]; then
    build_docker
    echo ""
    echo "镜像构建完成。"
    exit 0
fi

if [[ "$ALL_PLATFORMS" -eq 1 ]]; then
    OUT_FILE=""; TARGET_OS=linux;   TARGET_ARCH=amd64; build_backend
    OUT_FILE=""; TARGET_OS=linux;   TARGET_ARCH=arm64; build_backend
    OUT_FILE=""; TARGET_OS=windows; TARGET_ARCH=amd64; build_backend
    OUT_FILE=""; TARGET_OS=darwin;  TARGET_ARCH=arm64; build_backend
else
    build_backend
fi

echo ""
echo "构建完成，产物: $OUT_FILE"

if [[ "$RUN" -eq 1 ]]; then
    step "启动面板: http://localhost$LISTEN"
    exec "$OUT_FILE" -listen "$LISTEN" -data "$DATA_DIR" -token-expire "$TOKEN_EXPIRE"
fi
