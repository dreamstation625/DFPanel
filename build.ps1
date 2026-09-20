#Requires -Version 5.1
<#
.SYNOPSIS
    DFPanel 构建脚本：构建前端 -> 编译 Go 服务端（前端产物通过 go:embed 内嵌进二进制）。

.DESCRIPTION
    1. 在 web/ 目录执行 npm install（仅首次）+ npm run build，产出 web/dist
    2. 在仓库根目录执行 go build，产出单一可执行文件，可直接部署

.EXAMPLE
    .\build.ps1
        完整构建：前端 + 后端，产出 .\dfpanel.exe

.EXAMPLE
    .\build.ps1 -SkipFrontend
        跳过前端构建，复用现有 web/dist（没改前端时用，最快）

.EXAMPLE
    .\build.ps1 -FrontendOnly
        只构建前端产物

.EXAMPLE
    .\build.ps1 -Run -Listen ":9000"
        构建完成后立即启动面板，监听 9000 端口

.EXAMPLE
    .\build.ps1 -TargetOS linux -TargetArch amd64
        交叉编译 Linux amd64 版本
#>
[CmdletBinding()]
param(
    # 跳过前端构建，直接使用已有的 web/dist
    [switch]$SkipFrontend,
    # 只构建前端，不编译 Go
    [switch]$FrontendOnly,
    # 只编译 Go，不构建前端
    [switch]$BackendOnly,
    # 构建完成后启动面板（前台阻塞运行）
    [switch]$Run,
    # 输出文件路径
    [string]$OutFile,
    # 交叉编译目标系统：windows / linux / darwin
    [string]$TargetOS = "windows",
    # 交叉编译目标架构：amd64 / arm64
    [string]$TargetArch = "amd64",
    # -Run 时使用的监听地址
    [string]$Listen = ":8080",
    # -Run 时使用的数据目录
    [string]$DataDir = "./data",
    # -Run 时使用的登录有效期（小时）
    [int]$TokenExpire = 24,
    # 编译 Agent 程序（cmd/agent）而不是面板
    [switch]$Agent,
    # 构建 Docker 镜像而不是本地二进制
    [switch]$Docker,
    # Docker 镜像标签
    [string]$Image,
    # 一次性交叉编译常用平台（linux/windows × amd64/arm64）
    [switch]$AllPlatforms
)

$ErrorActionPreference = "Stop"

$root = $PSScriptRoot
$webDir = Join-Path $root "web"
$distDir = Join-Path $webDir "dist"

function Write-Step { param($Message) Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Ok { param($Message) Write-Host "    $Message" -ForegroundColor Green }
function Write-WarnMsg { param($Message) Write-Host "    [警告] $Message" -ForegroundColor Yellow }

function Test-Tool {
    param([string]$Name)
    return [bool](Get-Command $Name -ErrorAction SilentlyContinue)
}

function Invoke-FrontendBuild {
    Write-Step "构建前端"

    if (-not (Test-Path $webDir)) {
        throw "未找到前端目录: $webDir"
    }

    if (-not (Test-Tool "npm")) {
        if (Test-Path $distDir) {
            Write-WarnMsg "未检测到 npm，跳过前端构建，复用现有 web/dist"
            return
        }
        throw "未检测到 npm，且 web/dist 不存在，无法构建前端。请先安装 Node.js (>= 18)。"
    }

    # Windows 下显式使用 npm.cmd：nvm 等工具提供的 npm.ps1 在脚本作用域中传参会出错
    $npmCmd = if ($env:OS -eq "Windows_NT") { "npm.cmd" } else { "npm" }

    Push-Location $webDir
    try {
        if (-not (Test-Path (Join-Path $webDir "node_modules"))) {
            Write-Step "安装前端依赖 (npm install)"
            & $npmCmd install
            if ($LASTEXITCODE -ne 0) { throw "npm install 失败" }
        }

        Write-Step "编译前端 (npm run build)"
        & $npmCmd run build
        if ($LASTEXITCODE -ne 0) { throw "npm run build 失败" }
    }
    finally {
        Pop-Location
    }

    Write-Ok "前端产物已生成: $distDir"
}

function Invoke-BackendBuild {
    param(
        [string]$OS = $TargetOS,
        [string]$Arch = $TargetArch
    )

    if (-not (Test-Tool "go")) {
        throw "未检测到 go，请先安装 Go (>= 1.21)。"
    }

    $pkg = if ($Agent) { "./cmd/agent" } else { "." }
    $label = if ($Agent) { "Agent" } else { "服务端" }
    $base = if ($Agent) { "dfpanel-agent" } else { "dfpanel" }

    if (-not $OutFile) {
        $ext = if ($OS -eq "windows") { ".exe" } else { "" }
        if ($Agent -or $AllPlatforms) {
            New-Item -ItemType Directory -Force -Path (Join-Path $root "dist") | Out-Null
            $script:OutFile = Join-Path $root "dist" "$base-$OS-$Arch$ext"
        }
        else {
            $script:OutFile = Join-Path $root "$base$ext"
        }
    }

    Write-Step "编译$label ($OS/$Arch)"

    $env:GOOS = $OS
    $env:GOARCH = $Arch
    $env:CGO_ENABLED = "0"

    Push-Location $root
    try {
        & go build -trimpath -o $script:OutFile $pkg
        if ($LASTEXITCODE -ne 0) { throw "go build 失败" }
    }
    finally {
        Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
        Pop-Location
    }

    Write-Ok "$label 已生成: $script:OutFile"
}

function Invoke-DockerBuild {
    if (-not (Test-Tool "docker")) {
        throw "未检测到 docker，请先安装 Docker。"
    }

    if ($Agent) {
        $tag = if ($Image) { $Image } else { "dfpanel/agent:latest" }
        Write-Step "构建 Agent 镜像 $tag"
        & docker build -f (Join-Path $root "Dockerfile.agent") -t $tag $root
    }
    else {
        $tag = if ($Image) { $Image } else { "dfpanel/panel:latest" }
        Write-Step "构建面板镜像 $tag"
        & docker build -f (Join-Path $root "Dockerfile") -t $tag $root
    }
    if ($LASTEXITCODE -ne 0) { throw "docker build 失败" }
    Write-Ok "镜像已构建: $tag"
}

Push-Location $root
try {
    if (-not $BackendOnly) {
        if ($SkipFrontend) {
            Write-Step "跳过前端构建"
            if (-not (Test-Path $distDir)) {
                throw "web/dist 不存在，无法跳过前端构建。请先执行 .\build.ps1 -FrontendOnly"
            }
        }
        else {
            Invoke-FrontendBuild
        }
    }

    if ($FrontendOnly) {
        Write-Host ""
        Write-Host "前端构建完成。" -ForegroundColor Green
        exit 0
    }

    if ($Docker) {
        Invoke-DockerBuild
        Write-Host ""
        Write-Host "镜像构建完成。" -ForegroundColor Green
        exit 0
    }

    if ($AllPlatforms) {
        Invoke-BackendBuild -OS "linux" -Arch "amd64"
        Invoke-BackendBuild -OS "linux" -Arch "arm64"
        Invoke-BackendBuild -OS "windows" -Arch "amd64"
        Invoke-BackendBuild -OS "darwin" -Arch "arm64"
    }
    else {
        Invoke-BackendBuild
    }

    Write-Host ""
    Write-Host "构建完成，产物: $script:OutFile" -ForegroundColor Green

    if ($Run) {
        if ($TargetOS -ne "windows") {
            Write-WarnMsg "-Run 仅支持运行本地 Windows 产物，已跳过启动"
            exit 0
        }
        Write-Step "启动面板: http://localhost$Listen"
        & $script:OutFile -listen $Listen -data $DataDir -token-expire $TokenExpire
    }
}
finally {
    Pop-Location
}
