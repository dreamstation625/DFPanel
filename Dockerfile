# DFPanel 面板镜像（一体化：面板 + 内置 frps 二进制）
# 构建：docker build -t dreamstation625/dfpanel:latest .
# 运行：docker run -d --network host -v dfpanel-data:/data -e DFPANEL_PUBLIC_URL=http://1.2.3.4:8080 dreamstation625/dfpanel:latest

# ---------- 1. 前端 ----------
FROM node:22-alpine AS web
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
FROM alpine:3.20 AS frp
ARG FRP_VERSION=latest
ARG TARGETARCH=amd64
RUN apk add --no-cache curl tar
RUN set -eux; \
    if [ "$FRP_VERSION" = "latest" ]; then \
      VER=$(curl -fsSL https://api.github.com/repos/fatedier/frp/releases/latest \
        | sed -n 's/.*"tag_name":[[:space:]]*"v\{0,1\}\([^"]*\)".*/\1/p' | head -n1); \
    else VER="$FRP_VERSION"; fi; \
    case "$TARGETARCH" in amd64) ARCH=amd64;; arm64) ARCH=arm64;; *) ARCH=arm;; esac; \
    echo "frp version: $VER ($ARCH)"; \
    curl -fsSL -o /tmp/frp.tar.gz \
      "https://github.com/fatedier/frp/releases/download/v${VER}/frp_${VER}_linux_${ARCH}.tar.gz"; \
    mkdir -p /tmp/frp /out; \
    tar -xzf /tmp/frp.tar.gz -C /tmp/frp --strip-components=1; \
    install -m 0755 /tmp/frp/frps /out/frps

# ---------- 3. 后端 ----------
# 若本地 Go 版本与 go.mod 不一致导致镜像不存在，可把此处改成 golang:alpine
FROM golang:1.26-alpine AS server
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./web/dist
# 版本号取自根目录 VERSION 文件，注入二进制
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=$(cat VERSION)" -o /out/dfpanel .

# ---------- 4. 运行 ----------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata curl tar
ENV DFPANEL_LISTEN=:8080 \
    DFPANEL_DATA_DIR=/data \
    TZ=Asia/Shanghai
WORKDIR /app
COPY --from=server /out/dfpanel /usr/local/bin/dfpanel
COPY --from=frp /out/frps /usr/local/bin/frps
VOLUME ["/data"]
# 8080 面板 / 7000 frps / 7500 dashboard / 80,443 vhost（host 网络时无需映射）
EXPOSE 8080 7000 7500 80 443
ENTRYPOINT ["dfpanel"]
