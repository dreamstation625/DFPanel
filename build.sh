#!/usr/bin/env bash
# DFPanel 构建脚本：构建前端 -> 编译 Go 服务端（前端产物通过 go:embed 内嵌进二进制）
#
# 用法:
#   ./build.sh                       完整构建：前端 + 后端，产出本机平台与 Linux amd64 到 output/
#   ./build.sh --skip-frontend       跳过前端构建，复用现有 web/dist
#   ./build.sh --frontend-only       只构建前端
#   ./build.sh --backend-only        只编译服务端
#   ./build.sh --run                 构建完成后启动面板
#   ./build.sh --os linux --arch arm64   交叉编译
#   ./build.sh --listen :9000 --run  指定监听端口并启动
#   ./build.sh --agent              编译 Agent 程序（cmd/agent）
#   ./build.sh --agent --all-platforms   一次编译 Agent 的多平台产物到 output/
#   ./build.sh --agent-bundle   构建二进制面板分发用的 Agent 全平台包
#   ./build.sh --all-platforms       一次编译面板的多平台产物到 output/（linux/amd64+arm64、windows/amd64、darwin/arm64）
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
AGENT_BUNDLE=0
DOCKER=0
ALL_PLATFORMS=0
PLATFORM_SET=0
IMAGE=""
OUTPUTS=()
OUT_FILE=""
USER_OUT_FILE=""
LISTEN=":7226"
DATA_DIR="./data"
TOKEN_EXPIRE=24

while [[ $# -gt 0 ]]; do
    case "$1" in
        --skip-frontend) SKIP_FRONTEND=1; shift ;;
        --frontend-only) FRONTEND_ONLY=1; shift ;;
        --backend-only)  BACKEND_ONLY=1; shift ;;
        --run)           RUN=1; shift ;;
        --out)           USER_OUT_FILE="$2"; OUT_FILE="$2"; shift 2 ;;
        --os)            TARGET_OS="$2"; PLATFORM_SET=1; shift 2 ;;
        --arch)          TARGET_ARCH="$2"; PLATFORM_SET=1; shift 2 ;;
        --listen)        LISTEN="$2"; shift 2 ;;
        --data)          DATA_DIR="$2"; shift 2 ;;
        --token-expire)  TOKEN_EXPIRE="$2"; shift 2 ;;
        --agent)         AGENT=1; shift ;;
        --agent-bundle)  AGENT_BUNDLE=1; shift ;;
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

    # 产物统一放 output/ 并带平台后缀，每次调用都按当前平台重算 ——
    # 一次构建会产出多个平台，沿用上一个名字会把两份写成同一个文件
    if [[ -n "$USER_OUT_FILE" ]]; then
        OUT_FILE="$USER_OUT_FILE"
    else
        mkdir -p "$ROOT/output"
        OUT_FILE="$ROOT/output/${base}-${os}-${arch}${ext}"
    fi

    # 版本号统一取自根目录 VERSION 文件，构建时注入到二进制
    # 版本号取自根目录：面板用 VERSION，Agent 用 VERSION.agent（没有该文件时回退 VERSION）
    local ver_file="$ROOT/VERSION"
    if [[ "$AGENT" -eq 1 && -f "$ROOT/VERSION.agent" ]]; then
        ver_file="$ROOT/VERSION.agent"
    fi
    local ver="dev"
    [[ -f "$ver_file" ]] && ver="$(tr -d ' \n\r' < "$ver_file")"
    local ldflags
    if [[ "$AGENT" -eq 1 ]]; then
        ldflags="-X dfpanel/internal/agent.Version=$ver"
    else
        ldflags="-X main.version=$ver"
    fi

    # Go 是原生程序，认不出 Git Bash 的 /e/... 路径（会被当成当前盘根目录下的 e），
    # 所以传相对路径 —— CWD 已经切到 $ROOT 了
    local out_rel="$OUT_FILE"
    [[ "$OUT_FILE" == "$ROOT"/* ]] && out_rel="${OUT_FILE#"$ROOT"/}"
    local goarm=""
    [[ "$arch" != "arm" ]] || goarm=6
    (cd "$ROOT" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" GOARM="$goarm" \
        go build -trimpath -ldflags "$ldflags" -o "$out_rel" "$pkg")
    ok "$label 已生成: $OUT_FILE (版本 $ver)"
    OUTPUTS+=("$OUT_FILE")
    if [[ "$AGENT" -eq 0 && "$os" == "$(go env GOHOSTOS 2>/dev/null || echo linux)" ]]; then
        HOST_ARTIFACT="$OUT_FILE"
    fi
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

if [[ "$AGENT_BUNDLE" -eq 1 ]]; then
    has go || { echo "未检测到 go"; exit 1; }
    local_version="$(tr -d ' \n\r' < "$ROOT/VERSION")"
    mkdir -p "$ROOT/output"
    (cd "$ROOT" && go run ./cmd/agentbundle -output "output/dfpanel-agent-bundle-${local_version}.tar.gz")
    exit 0
fi

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

HOST_OS="$(go env GOHOSTOS 2>/dev/null || echo linux)"
if [[ "$ALL_PLATFORMS" -eq 1 ]]; then
    OUT_FILE=""; TARGET_OS=linux;   TARGET_ARCH=amd64; build_backend
    OUT_FILE=""; TARGET_OS=linux;   TARGET_ARCH=arm64; build_backend
    OUT_FILE=""; TARGET_OS=linux;   TARGET_ARCH=arm;   build_backend
    OUT_FILE=""; TARGET_OS=windows; TARGET_ARCH=amd64; build_backend
    OUT_FILE=""; TARGET_OS=windows; TARGET_ARCH=386;   build_backend
    OUT_FILE=""; TARGET_OS=darwin;  TARGET_ARCH=amd64; build_backend
    OUT_FILE=""; TARGET_OS=darwin;  TARGET_ARCH=arm64; build_backend
elif [[ "$PLATFORM_SET" -eq 1 || -n "${GOOS:-}" ]]; then
    # 显式指定平台时只编那一个
    build_backend
else
    # 默认：本机平台 + Linux amd64（服务器上跑的那份）
    OUT_FILE=""; TARGET_OS="$HOST_OS"; TARGET_ARCH=amd64; build_backend
    if [[ "$HOST_OS" != "linux" ]]; then
        OUT_FILE=""; TARGET_OS=linux; TARGET_ARCH=amd64; build_backend
    fi
fi

echo ""
echo "构建完成，产物:"
for f in ${OUTPUTS[@]+"${OUTPUTS[@]}"}; do
    echo "  $f"
done

if [[ "$RUN" -eq 1 ]]; then
    step "启动面板: http://localhost$LISTEN"
    exec "${HOST_ARTIFACT:-$OUT_FILE}" -listen "$LISTEN" -data "$DATA_DIR" -token-expire "$TOKEN_EXPIRE"
fi
