#Requires -Version 5.1
<#
.SYNOPSIS
    DFPanel 构建脚本：构建前端 -> 编译 Go 服务端（前端产物通过 go:embed 内嵌进二进制）。

.DESCRIPTION
    1. 在 web/ 目录执行 npm install（仅首次）+ npm run build，产出 web/dist
    2. 在仓库根目录执行 go build，产出单一可执行文件，可直接部署

.EXAMPLE
    .\build.ps1
        完整构建：前端 + 后端；默认产出本机平台与 Linux amd64 两份面板，都在 .\output\ 下

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
        编译 Agent，产出 .\output\dfpanel-agent-<os>-<arch>[.exe]

.EXAMPLE
    .\build.ps1 -Agent -AllPlatforms
        一次产出 linux/amd64、linux/arm64、windows/amd64、darwin/arm64 四个 Agent

.EXAMPLE
    .\build.ps1 -AllPlatforms
        不带 -Agent 时，一次产出 linux/amd64、linux/arm64、windows/amd64、darwin/arm64 四个面板

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
    # 构建二进制面板分发用的 Agent 全平台包
    [switch]$AgentBundle,
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
# 前端产物（vite），注意和下面的 output/ 区分开
$distDir = Join-Path $webDir "dist"
# 构建产物统一输出到这里
$outDir = Join-Path $root "output"
# 宿主平台：默认构建产出「本机平台 + linux/amd64」两份
$hostOS = if ($env:OS -eq "Windows_NT") { "windows" } else { "linux" }
# 本次构建产出的文件；HostArtifact 是本机平台那份，-Run 用
$script:Outputs = @()
$script:HostArtifact = ""
# 用户显式 -OutFile 指定的文件：指定了就不再按平台自动命名
$script:UserOutFile = $OutFile

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
    # 产物统一放 output/ 并带平台后缀，每次调用都按当前平台重算 ——
    # 一次构建会产出多个平台，沿用上一个名字会把两份写成同一个文件。
    if ($script:UserOutFile) {
        $script:OutFile = $script:UserOutFile
    }
    else {
        New-Item -ItemType Directory -Force -Path $outDir | Out-Null
        $script:OutFile = Join-Path $outDir "$base-$OS-$Arch$ext"
    }

    Write-Step "编译$label ($OS/$Arch)"

    # 版本号取自根目录：面板用 VERSION，Agent 用 VERSION.agent（没有该文件时回退 VERSION）
    $verFile = Join-Path $root "VERSION"
    if ($Agent) {
        $agentVerFile = Join-Path $root "VERSION.agent"
        if (Test-Path $agentVerFile) { $verFile = $agentVerFile }
    }
    $ver = "dev"
    if (Test-Path $verFile) { $ver = (Get-Content $verFile -Raw -Encoding UTF8).Trim() }
    $ldflags = if ($Agent) { "-X dfpanel/internal/agent.Version=$ver" } else { "-X main.version=$ver" }

    $env:GOOS = $OS
    $env:GOARCH = $Arch
    $env:CGO_ENABLED = "0"
    if ($Arch -eq "arm") { $env:GOARM = "6" }

    Push-Location $root
    try {
        if ((Invoke-Native go "build" "-trimpath" "-ldflags" $ldflags "-o" $script:OutFile $pkg) -ne 0) {
            throw "go build 失败"
        }
    }
    finally {
        # 清掉交叉编译用的进程级变量。
        # 不用 Remove-Item Env:xxx：某些受管环境会把对 Env: 驱动器的删除当成文件删除来拦截。
        foreach ($envName in @("GOOS", "GOARCH", "GOARM", "CGO_ENABLED")) {
            [Environment]::SetEnvironmentVariable($envName, $null, "Process")
        }
        Pop-Location
    }

    Write-Ok "$label 已生成: $script:OutFile"
    $script:Outputs += $script:OutFile
    if (-not $Agent -and $OS -eq $hostOS) { $script:HostArtifact = $script:OutFile }
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
    if ($AgentBundle) {
        $panelVersion = (Get-Content -LiteralPath (Join-Path $root "VERSION") -Raw -Encoding UTF8).Trim()
        New-Item -ItemType Directory -Force -Path $outDir | Out-Null
        $bundlePath = Join-Path $outDir "dfpanel-agent-bundle-$panelVersion.tar.gz"
        & go run ./cmd/agentbundle -output $bundlePath
        if ($LASTEXITCODE -ne 0) { throw "构建 Agent 全平台包失败" }
        Write-Ok "Agent 全平台包已生成：$bundlePath"
        exit 0
    }
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
        Invoke-BackendBuild -OS "linux" -Arch "arm"
        Invoke-BackendBuild -OS "windows" -Arch "amd64"
        Invoke-BackendBuild -OS "windows" -Arch "386"
        Invoke-BackendBuild -OS "darwin" -Arch "amd64"
        Invoke-BackendBuild -OS "darwin" -Arch "arm64"
    }
    elseif ($PSBoundParameters.ContainsKey("TargetOS") -or $PSBoundParameters.ContainsKey("TargetArch")) {
        # 显式指定平台时只编那一个
        Invoke-BackendBuild
    }
    else {
        # 默认：本机平台 + Linux amd64（服务器上跑的那份）
        Invoke-BackendBuild -OS $hostOS -Arch "amd64"
        if ($hostOS -ne "linux") {
            Invoke-BackendBuild -OS "linux" -Arch "amd64"
        }
    }

    Write-Host ""
    Write-Host "构建完成，产物：" -ForegroundColor Green
    foreach ($artifact in $script:Outputs) {
        Write-Host "  $artifact" -ForegroundColor Green
    }

    if ($Run) {
        if (-not $script:HostArtifact) {
            Write-WarnMsg "本次没有产出本机平台的面板，已跳过启动"
            exit 0
        }
        Write-Step "启动面板: http://localhost$Listen"
        & $script:HostArtifact -listen $Listen -data $DataDir -token-expire $TokenExpire
    }
}
finally {
    Pop-Location
}
