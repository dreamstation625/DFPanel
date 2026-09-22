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

.EXAMPLE
    .\build.ps1 -Agent
        编译 Agent，产出 .\dist\dfpanel-agent-<os>-<arch>[.exe]

.EXAMPLE
    .\build.ps1 -Agent -AllPlatforms
        一次产出 linux/amd64、linux/arm64、windows/amd64、darwin/arm64 四个 Agent

.EXAMPLE
    .\build.ps1 -Docker -Agent -Image myrepo/dfpanel-agent:v1
        构建 Agent 镜像并指定标签

.NOTES
    版本号取自根目录 VERSION 文件，构建时注入二进制（面板注入 main.version，
    Agent 注入 dfpanel/internal/agent.Version）；未注入时为 dev。
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
    [string]$Listen = ":7226",
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
    # 一次性交叉编译常用平台（linux/windows/darwin 的 amd64/arm64）
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

# 运行原生命令（npm / go / docker）。
# PowerShell 5.1 在 $ErrorActionPreference='Stop' 下会把其它程序写到 stderr 的普通日志
# 当成终止性错误（vite 的告警、go 的编译信息都会走 stderr），这里统一下降到 Continue，
# 再依据退出码判断成功与否。
function Invoke-Native {
    param(
        [Parameter(Mandatory = $true)][string]$FilePath,
        [Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments
    )

    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        & $FilePath @Arguments 2>&1 | ForEach-Object { Write-Host $_ }
    }
    finally {
        $ErrorActionPreference = $prev
    }
    return $LASTEXITCODE
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
            if ((Invoke-Native $npmCmd "install") -ne 0) { throw "npm install 失败" }
        }

        Write-Step "编译前端 (npm run build)"
        if ((Invoke-Native $npmCmd "run" "build") -ne 0) { throw "npm run build 失败" }
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

    $ext = if ($OS -eq "windows") { ".exe" } else { "" }
    # 多平台构建时每次调用都要重算产物名，否则多次构建会写到同一个文件
    if ($AllPlatforms -or -not $script:OutFile) {
        if ($Agent -or $AllPlatforms) {
            New-Item -ItemType Directory -Force -Path (Join-Path $root "dist") | Out-Null
            # PowerShell 5.1 的 Join-Path 只接受两个位置参数，需嵌套调用
            $script:OutFile = Join-Path (Join-Path $root "dist") "$base-$OS-$Arch$ext"
        }
        else {
            $script:OutFile = Join-Path $root "$base$ext"
        }
    }

    Write-Step "编译$label ($OS/$Arch)"

    # 版本号统一取自根目录 VERSION 文件，构建时注入到二进制
    $ver = "dev"
    $verFile = Join-Path $root "VERSION"
    if (Test-Path $verFile) { $ver = (Get-Content $verFile -Raw -Encoding UTF8).Trim() }
    $ldflags = if ($Agent) { "-X dfpanel/internal/agent.Version=$ver" } else { "-X main.version=$ver" }

    $env:GOOS = $OS
    $env:GOARCH = $Arch
    $env:CGO_ENABLED = "0"

    Push-Location $root
    try {
        if ((Invoke-Native go "build" "-trimpath" "-ldflags" $ldflags "-o" $script:OutFile $pkg) -ne 0) {
            throw "go build 失败"
        }
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
        $tag = if ($Image) { $Image } else { "dreamstation625/dfpanel-agent:latest" }
        Write-Step "构建 Agent 镜像 $tag"
        $code = Invoke-Native docker "build" "-f" (Join-Path $root "Dockerfile.agent") "-t" $tag $root
    }
    else {
        $tag = if ($Image) { $Image } else { "dreamstation625/dfpanel:latest" }
        Write-Step "构建面板镜像 $tag"
        $code = Invoke-Native docker "build" "-f" (Join-Path $root "Dockerfile") "-t" $tag $root
    }
    if ($code -ne 0) { throw "docker build 失败" }
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

    if ($AllPlatforms -and $OutFile) {
        throw "-OutFile 与 -AllPlatforms 不能同时使用：多平台产物需要各自独立的文件名。"
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
