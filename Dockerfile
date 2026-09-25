# DFPanel 面板镜像（一体化：面板 + 内置 frps 和 Agent 二进制）
# 构建：docker build -t dreamstation625/dfpanel:latest .
# 运行：docker run -d --network host -v dfpanel-data:/data -e DFPANEL_PUBLIC_URL=http://1.2.3.4:7226 dreamstation625/dfpanel:latest

# ---------- 1. 前端 ----------
# 多架构构建时前端在「构建机架构」上执行（--platform=$BUILDPLATFORM）：
# vite 产物是平台无关的静态文件，若放到 QEMU 模拟的目标架构里跑 npm/vite，
# 会慢到超出 Actions job 时限（曾出现 6 小时超时被取消）。
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /web
# 用 npm ci 严格按 lock 安装，构建可复现。
# 注意：package-lock.json 必须包含所有平台的 rollup / esbuild 原生包
# （@rollup/rollup-linux-x64-musl、@esbuild/linux-x64 等）。
# npm 会按当前平台裁剪可选依赖，因此不要用 --omit=optional 生成 lock，
# 否则容器内 install 会缺少原生模块，vite build 直接失败。
COPY web/package.json ./
COPY web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---------- 2. frps 二进制（供容器内的本地一体化模式使用） ----------
# 同样在构建机架构上执行下载与解包，只按目标架构选择要下载的二进制。
FROM --platform=$BUILDPLATFORM alpine:3.20 AS frp
ARG FRP_VERSION=latest
ARG TARGETARCH=amd64
RUN apk add --no-cache curl tar
RUN set -eux; \
    if [ "$FRP_VERSION" = "latest" ]; then \
      VER=$(curl -fsSL --max-time 60 --retry 3 --retry-delay 3 \
        https://api.github.com/repos/fatedier/frp/releases/latest \
        | sed -n 's/.*"tag_name":[[:space:]]*"v\{0,1\}\([^"]*\)".*/\1/p' | head -n1); \
    else VER="$FRP_VERSION"; fi; \
    case "$TARGETARCH" in amd64) ARCH=amd64;; arm64) ARCH=arm64;; *) ARCH=arm;; esac; \
    echo "frp version: $VER ($ARCH)"; \
    curl -fsSL --max-time 300 --retry 3 --retry-delay 5 -o /tmp/frp.tar.gz \
      "https://github.com/fatedier/frp/releases/download/v${VER}/frp_${VER}_linux_${ARCH}.tar.gz"; \
    mkdir -p /tmp/frp /out; \
    tar -xzf /tmp/frp.tar.gz -C /tmp/frp --strip-components=1; \
    install -m 0755 /tmp/frp/frps /out/frps

# ---------- 3. 后端 ----------
# 若本地 Go 版本与 go.mod 不一致导致镜像不存在，可把此处改成 golang:alpine
# 在构建机架构上编译，再用 GOOS/GOARCH 交叉编译到目标架构（CGO 已关闭），
# 这样 arm64 镜像不需要经过 QEMU 模拟，构建时间从小时级降到分钟级。
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS server
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./web/dist
# 版本号取自根目录 VERSION 文件，注入二进制
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-X main.version=$(tr -d '[:space:]' < VERSION)" -o /out/dfpanel .

# 面板直接分发同一源码版本的 Agent，Docker 部署无需手动上传文件或依赖 GitHub Release。
# 构建 Linux、Windows、macOS 安装脚本当前支持的平台。
RUN set -eux; \
    mkdir -p /out/agent; \
    AGENT_VERSION="$(tr -d '[:space:]' < VERSION.agent)"; \
    for platform in linux/amd64 linux/arm64 linux/arm windows/amd64 windows/386 darwin/amd64 darwin/arm64; do \
      os="${platform%/*}"; arch="${platform#*/}"; ext=""; goarm=""; \
      [ "$os" != windows ] || ext=.exe; \
      [ "$arch" != arm ] || goarm=6; \
      CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" GOARM="$goarm" go build -trimpath \
        -ldflags "-X dfpanel/internal/agent.Version=${AGENT_VERSION}" \
        -o "/out/agent/agent-${os}-${arch}${ext}" ./cmd/agent; \
    done

# ---------- 4. 运行 ----------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata curl tar
ENV DFPANEL_LISTEN=:7226 \
    DFPANEL_DATA_DIR=/data \
    TZ=Asia/Shanghai
WORKDIR /app
COPY --from=server /out/dfpanel /usr/local/bin/dfpanel
COPY --from=server /out/agent/ /usr/local/share/dfpanel/agent/
COPY VERSION.agent /usr/local/share/dfpanel/agent/VERSION.agent
COPY --from=frp /out/frps /usr/local/bin/frps
VOLUME ["/data"]
# 7226 面板 / 7000 frps / 7500 dashboard / 80,443 vhost（host 网络时无需映射）
EXPOSE 7226 7000 7500 80 443
ENTRYPOINT ["dfpanel"]
