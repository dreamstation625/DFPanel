#Requires -Version 5.1
<#
.SYNOPSIS
    Windows 面板部署入口：二进制注册为 Windows 服务；Docker 模式提示手动部署。
.EXAMPLE
    .\deploy-panel.ps1 -Mode Binary -PublicUrl http://192.168.1.10:7226
.EXAMPLE
    .\deploy-panel.ps1 -Mode Binary -PublicUrl http://192.168.1.10:7226 -BinaryPath .\output\dfpanel-windows-amd64.exe
#>
[CmdletBinding()]
param(
    [string]$Mode,
    [string]$PublicUrl,
    [string]$Listen = ":7226",
    [string]$BinaryPath,
    [string]$AgentBundle,
    [string]$Version
)

$ErrorActionPreference = "Stop"
$repoRoot = $PSScriptRoot
$repository = "dreamstation625/DFPanel"

if (-not $Mode) {
    if ([Console]::IsInputRedirected) {
        throw "非交互运行时请指定 -Mode Docker 或 -Mode Binary"
    }
    Write-Host "选择部署方式：1) Docker  2) 二进制 + Windows 服务"
    $choice = Read-Host "请输入 1 或 2"
    $Mode = switch ($choice) {
        "1" { "Docker" }
        "2" { "Binary" }
        default { throw "无效选项" }
    }
}
if ($Mode -notin @("Docker", "Binary")) {
    throw "部署方式只能是 Docker 或 Binary"
}

if ($Mode -eq "Docker") {
    Write-Host "Windows 下请手动部署 Docker 版本。" -ForegroundColor Yellow
    Write-Host "操作方法：修改 docker-compose.yml，注释 network_mode: host，启用所需 ports，"
    Write-Host "设置 DFPANEL_PUBLIC_URL 后运行 docker compose up -d。"
    Write-Host "面板端口默认为 7226；frps 和隧道端口也需按实际配置映射。"
    exit 0
}

if (-not [Environment]::Is64BitOperatingSystem) {
    throw "目前的 Windows 二进制仅发布 amd64 版本"
}
$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "注册 Windows 服务需要管理员权限，请以管理员身份运行 PowerShell"
}
if (-not $PublicUrl) {
    if ([Console]::IsInputRedirected) {
        throw "非交互运行时请指定 -PublicUrl"
    }
    $PublicUrl = Read-Host "Agent 可访问的面板地址（例如 http://192.168.1.10:7226）"
}

$uri = $null
if (-not [Uri]::TryCreate($PublicUrl, [UriKind]::Absolute, [ref]$uri) -or
    $uri.Scheme -notin @("http", "https") -or
    -not $uri.Host -or
    $uri.AbsolutePath -ne "/" -or
    $uri.Query -or $uri.Fragment -or $uri.UserInfo -or
    $PublicUrl -match '[\s"\x00-\x1f]') {
    throw "-PublicUrl 应为 http(s)://主机[:端口]，不能包含路径或凭据"
}
$PublicUrl = $PublicUrl.TrimEnd("/")
if ($Listen -notmatch '^:[0-9]{1,5}$') {
    throw "-Listen 应为 :端口，例如 :7226"
}
if (-not $Version -and (Test-Path -LiteralPath (Join-Path $repoRoot "VERSION"))) {
    $Version = (Get-Content -LiteralPath (Join-Path $repoRoot "VERSION") -Raw -Encoding UTF8).Trim()
}
if ($Version -and $Version -notmatch '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$') {
    throw "版本号格式不合法：$Version"
}

$installDir = Join-Path $env:ProgramData "DFPanel"
$dataDir = Join-Path $installDir "data"
$exePath = Join-Path $installDir "dfpanel.exe"
$localDefault = Join-Path $repoRoot "output\dfpanel-windows-amd64.exe"
$tempDir = Join-Path ([IO.Path]::GetTempPath()) ("dfpanel-deploy-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tempDir -Force | Out-Null

function Get-ReleaseBinary {
    param([string]$ReleaseVersion, [string]$WorkDir)
    if (-not $ReleaseVersion) { return $null }

    $archiveName = "dfpanel-$ReleaseVersion-windows-amd64.zip"
    $archive = Join-Path $WorkDir $archiveName
    $checksums = Join-Path $WorkDir "checksums.txt"
    $base = "https://github.com/$repository/releases/download/v$ReleaseVersion"

    # gh 可读取已经登录的私有仓库；未安装或未登录时尝试公开 Release。
    if (Get-Command gh -ErrorAction SilentlyContinue) {
        try {
            & gh release download "v$ReleaseVersion" -R $repository -p $archiveName -p checksums.txt -D $WorkDir 2>$null | Out-Null
        }
        catch {}
    }
    if (-not (Test-Path -LiteralPath $archive)) {
        try { Invoke-WebRequest -Uri "$base/$archiveName" -OutFile $archive -UseBasicParsing | Out-Null }
        catch { return $null }
    }
    if (-not (Test-Path -LiteralPath $checksums)) {
        try { Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile $checksums -UseBasicParsing | Out-Null }
        catch { throw "Release 归档已下载，但无法获取校验文件 checksums.txt" }
    }
    $line = Get-Content -LiteralPath $checksums | Where-Object { $_ -match ('^[0-9a-fA-F]{64}\s+\*?' + [regex]::Escape($archiveName) + '$') } | Select-Object -First 1
    if (-not $line) { throw "校验文件中没有 $archiveName 的 SHA256" }
    $expected = ($line -split '\s+')[0].ToLowerInvariant()
    $actual = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { throw "Release 归档 SHA256 校验失败" }

    $extractDir = Join-Path $WorkDir "extract"
    Expand-Archive -LiteralPath $archive -DestinationPath $extractDir -Force
    $extracted = Join-Path $extractDir "dfpanel-$ReleaseVersion-windows-amd64\dfpanel.exe"
    if (-not (Test-Path -LiteralPath $extracted)) { throw "Release 归档中没有 dfpanel.exe" }
    Write-Host "==> 已下载并校验 Release：$archiveName"
    return $extracted
}

function Get-ReleaseAgentBundle {
    param([string]$ReleaseVersion, [string]$WorkDir)
    if (-not $ReleaseVersion) { return $null }
    $archiveName = "dfpanel-agent-bundle-$ReleaseVersion.tar.gz"
    $archive = Join-Path $WorkDir $archiveName
    $checksums = Join-Path $WorkDir "checksums.txt"
    $base = "https://github.com/$repository/releases/download/v$ReleaseVersion"
    if (Get-Command gh -ErrorAction SilentlyContinue) {
        try {
            if (Test-Path -LiteralPath $checksums) {
                & gh release download "v$ReleaseVersion" -R $repository -p $archiveName -D $WorkDir 2>$null | Out-Null
            }
            else {
                & gh release download "v$ReleaseVersion" -R $repository -p $archiveName -p checksums.txt -D $WorkDir 2>$null | Out-Null
            }
        }
        catch {}
    }
    if (-not (Test-Path -LiteralPath $archive)) {
        try { Invoke-WebRequest -Uri "$base/$archiveName" -OutFile $archive -UseBasicParsing | Out-Null }
        catch { return $null }
    }
    if (-not (Test-Path -LiteralPath $checksums)) {
        try { Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile $checksums -UseBasicParsing | Out-Null }
        catch { throw "Agent 包已下载，但无法获取 checksums.txt" }
    }
    $line = Get-Content -LiteralPath $checksums | Where-Object { $_ -match ('^[0-9a-fA-F]{64}\s+\*?' + [regex]::Escape($archiveName) + '$') } | Select-Object -First 1
    if (-not $line) { throw "校验文件中没有 $archiveName 的 SHA256" }
    $expected = ($line -split '\s+')[0].ToLowerInvariant()
    $actual = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { throw "Agent 全平台包 SHA256 校验失败" }
    Write-Host "==> 已下载并校验 Agent 全平台包：$archiveName"
    return $archive
}

function Test-ServiceBinary {
    param([string]$Path)
    try {
        & $Path -service-ready 2>$null | Out-Null
        return ($LASTEXITCODE -eq 0)
    }
    catch { return $false }
}

try {
    $source = $null
    if ($BinaryPath) {
        $source = (Resolve-Path -LiteralPath $BinaryPath -ErrorAction Stop).ProviderPath
        Write-Host "==> 使用指定的本地二进制：$source"
    }
    else {
        $source = Get-ReleaseBinary -ReleaseVersion $Version -WorkDir $tempDir
        if ($source -and -not (Test-ServiceBinary -Path $source)) {
            Write-Warning "Release 二进制不支持 Windows 服务，尝试本地构建产物"
            $source = $null
        }
        if (-not $source) {
            if (-not (Test-Path -LiteralPath $localDefault)) {
                throw "没有可用的 Windows 服务版二进制；请先构建当前源码，再使用 -BinaryPath，或发布新版 Release"
            }
            $source = $localDefault
            Write-Host "==> 改用本地构建产物：$source"
        }
    }
    if (-not (Test-Path -LiteralPath $source -PathType Leaf) -or (Get-Item -LiteralPath $source).Length -eq 0) {
        throw "二进制不存在或为空：$source"
    }
    & $source -version | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "二进制无法在当前机器运行：$source" }
    if (-not (Test-ServiceBinary -Path $source)) {
        throw "二进制不支持 Windows 服务：$source；请构建当前源码或使用新版 Release"
    }

    $bundleSource = $null
    if ($AgentBundle) {
        $bundleSource = (Resolve-Path -LiteralPath $AgentBundle -ErrorAction Stop).ProviderPath
    }
    else {
        $bundleSource = Get-ReleaseAgentBundle -ReleaseVersion $Version -WorkDir $tempDir
        if (-not $bundleSource) {
            $localBundle = Join-Path $repoRoot "output\dfpanel-agent-bundle-$Version.tar.gz"
            if (Test-Path -LiteralPath $localBundle) {
                $bundleSource = $localBundle
            }
            else {
                if (-not (Get-Command go -ErrorAction SilentlyContinue) -or -not (Test-Path -LiteralPath (Join-Path $repoRoot "go.mod"))) {
                    throw "没有 Agent 全平台包；请发布 v$Version 的 Pre-release，或用 -AgentBundle 指定本地包"
                }
                $bundleSource = Join-Path $tempDir "dfpanel-agent-bundle-$Version.tar.gz"
                Write-Host "==> Release 不可用，从当前源码构建 Agent 全平台包"
                Push-Location $repoRoot
                try {
                    & go run ./cmd/agentbundle -output $bundleSource
                    if ($LASTEXITCODE -ne 0) { throw "构建 Agent 全平台包失败" }
                }
                finally { Pop-Location }
            }
        }
    }
    if (-not (Test-Path -LiteralPath $bundleSource -PathType Leaf) -or (Get-Item -LiteralPath $bundleSource).Length -eq 0) {
        throw "Agent 全平台包不存在或为空：$bundleSource"
    }

    New-Item -ItemType Directory -Force -Path $installDir, $dataDir | Out-Null
    $agentVersion = (& $source -prepare-agent-bundle $bundleSource $dataDir | Select-Object -Last 1).Trim()
    if ($LASTEXITCODE -ne 0 -or -not $agentVersion) { throw "Agent 全平台包校验失败，请确认它与面板版本一致" }
    $service = Get-Service -Name "DFPanel" -ErrorAction SilentlyContinue
    $oldCommand = $null
    $backup = Join-Path $tempDir "old-dfpanel.exe"
    if ($service) {
        $oldCommand = (Get-CimInstance Win32_Service -Filter "Name='DFPanel'").PathName
        $expectedPrefix = '"' + $exePath + '"'
        if (-not $oldCommand -or -not $oldCommand.StartsWith($expectedPrefix, [StringComparison]::OrdinalIgnoreCase)) {
            throw "已存在同名 Windows 服务，且程序路径不是 $exePath；请手动检查"
        }
    }
    if (Test-Path -LiteralPath $exePath) { Copy-Item -LiteralPath $exePath -Destination $backup -Force }
    if ($service -and $service.Status -ne "Stopped") {
        Stop-Service -Name "DFPanel" -Force
        (Get-Service -Name "DFPanel").WaitForStatus("Stopped", [TimeSpan]::FromSeconds(30))
    }

    $commandLine = '"' + $exePath + '" -listen ' + $Listen + ' -data "' + $dataDir + '" -public-url ' + $PublicUrl
    $created = $false
    try {
        if (-not [string]::Equals($source, $exePath, [StringComparison]::OrdinalIgnoreCase)) {
            Copy-Item -LiteralPath $source -Destination $exePath -Force
        }
        if ($service) {
            & sc.exe config DFPanel binPath= $commandLine start= auto | Out-Null
            if ($LASTEXITCODE -ne 0) { throw "更新 DFPanel 服务配置失败" }
        }
        else {
            New-Service -Name "DFPanel" -BinaryPathName $commandLine -DisplayName "DFPanel" -StartupType Automatic | Out-Null
            $created = $true
        }
        Start-Service -Name "DFPanel"
        (Get-Service -Name "DFPanel").WaitForStatus("Running", [TimeSpan]::FromSeconds(30))
        $healthUri = "http://127.0.0.1:$($Listen.Substring(1))/api/init-status"
        $healthy = $false
        for ($attempt = 0; $attempt -lt 15; $attempt++) {
            if ((Get-Service -Name "DFPanel").Status -ne "Running") { break }
            try {
                Invoke-WebRequest -Uri $healthUri -TimeoutSec 2 -UseBasicParsing | Out-Null
                $healthy = $true
                break
            }
            catch { Start-Sleep -Seconds 1 }
        }
        if (-not $healthy) { throw "Windows 服务未通过面板健康检查，请查看 $(Join-Path $dataDir 'panel.log')" }
        & $exePath -activate-agent-bundle $dataDir $Version
        if ($LASTEXITCODE -ne 0) { throw "激活 Agent 全平台包失败" }
        Write-Host "==> Windows 服务 DFPanel 已启动；面板地址：$PublicUrl"
        Write-Host "==> 数据目录：$dataDir；Agent 版本：$agentVersion；服务日志：$(Join-Path $dataDir 'panel.log')"
    }
    catch {
        $reason = $_
        Write-Warning "部署失败，正在恢复原有二进制和服务配置"
        Stop-Service -Name "DFPanel" -Force -ErrorAction SilentlyContinue
        if ($created) {
            & sc.exe delete DFPanel | Out-Null
        }
        elseif ($oldCommand) {
            & sc.exe config DFPanel binPath= $oldCommand | Out-Null
        }
        if (Test-Path -LiteralPath $backup) {
            Copy-Item -LiteralPath $backup -Destination $exePath -Force
            if ($service) { Start-Service -Name "DFPanel" -ErrorAction SilentlyContinue }
        }
        elseif (Test-Path -LiteralPath $exePath) {
            Remove-Item -LiteralPath $exePath -Force
        }
        throw $reason
    }
}
finally {
    if (Test-Path -LiteralPath $tempDir) {
        Remove-Item -LiteralPath $tempDir -Recurse -Force
    }
}
