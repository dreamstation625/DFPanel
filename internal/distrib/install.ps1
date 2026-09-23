# DFPanel Agent 一键安装 / 卸载脚本（Windows）
# 用法：
#   安装：
#     powershell -ExecutionPolicy Bypass -Command "irm http://<panel>:7226/install.ps1 -OutFile install.ps1; .\install.ps1 -Panel http://<panel>:7226 -NodeKey <KEY> -NodeSecret <SECRET> -Roles frps,frpc"
#   卸载：
#     .\install.ps1 -Uninstall [-Purge]
#       -Uninstall  停止托管的 frp 实例、注销计划任务、删除配置与二进制；默认保留数据目录
#       -Purge      连数据目录一起删（frp 二进制缓存等）
param(
    [string]$Panel,
    [string]$NodeKey,
    [string]$NodeSecret,
    [string]$Roles = "frpc",
    [ValidateSet("process", "docker")][string]$Runtime = "process",
    [switch]$Uninstall,
    [switch]$Purge
)

$ErrorActionPreference = "Stop"

# ProgramData 下写入可避开 Program Files 的 UAC 限制
$BaseDir    = Join-Path $env:ProgramData "dfpanel-agent"
$DataDir    = Join-Path $BaseDir "data"
$ConfigPath = Join-Path $BaseDir "agent.json"
$ExePath    = Join-Path $BaseDir "dfpanel-agent.exe"
$TaskName   = "DFPanelAgent"

# ---------------- 卸载 ----------------
# Agent 退出不会带走自己拉起的 frp（这样重启时隧道不中断），所以这里要按 pid 文件显式收干净。
if ($Uninstall) {
    Write-Host "==> 停止并注销计划任务 $TaskName"
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue

    Write-Host "==> 停止托管的 frp 实例"
    foreach ($f in (Get-ChildItem -Path $DataDir -Filter "*.pid" -ErrorAction SilentlyContinue)) {
        $raw = Get-Content $f.FullName -Raw -ErrorAction SilentlyContinue
        Remove-Item $f.FullName -Force -ErrorAction SilentlyContinue

        $procId = 0
        if (-not [int]::TryParse(("$raw").Trim(), [ref]$procId)) { continue }

        # 先确认这个 pid 确实还是 frp，避免 pid 被复用后误杀别的进程
        $proc = Get-Process -Id $procId -ErrorAction SilentlyContinue
        if ($proc -and $proc.ProcessName -match '^frp[sc]$') {
            Stop-Process -Id $procId -Force -ErrorAction SilentlyContinue
            Write-Host "    已停止 $($proc.ProcessName)（pid $procId）"
        }
    }

    if (Get-Command docker -ErrorAction SilentlyContinue) {
        $containers = docker ps -aq --filter "name=dfpanel-frps-" --filter "name=dfpanel-frpc-" 2>$null
        if ($containers) {
            docker rm -f $containers > $null 2>&1
            Write-Host "==> 已清理 frp 容器"
        }
    }

    Write-Host "==> 删除配置与二进制"
    foreach ($f in @($ExePath, $ConfigPath)) {
        if (Test-Path -LiteralPath $f) { Remove-Item -LiteralPath $f -Force -ErrorAction SilentlyContinue }
    }

    if ($Purge) {
        if (Test-Path -LiteralPath $DataDir) { Remove-Item -LiteralPath $DataDir -Recurse -Force -ErrorAction SilentlyContinue }
        Write-Host "==> 数据目录已删除：$DataDir"
    }
    else {
        Write-Host "==> 数据目录保留：$DataDir（要一起删就加 -Purge）"
    }

    # 目录已经空了就顺手删掉
    if ((Test-Path -LiteralPath $BaseDir) -and -not (Get-ChildItem -LiteralPath $BaseDir -ErrorAction SilentlyContinue)) {
        Remove-Item -LiteralPath $BaseDir -Force -ErrorAction SilentlyContinue
    }

    Write-Host "==> 卸载完成（记得在面板「Agent 管理」里把这条记录删掉）"
    exit 0
}

# ---------------- 安装 ----------------
if (-not $Panel -or -not $NodeKey -or -not $NodeSecret) {
    Write-Host "-Panel / -NodeKey / -NodeSecret 均为必填；只卸载请用 -Uninstall" -ForegroundColor Red
    exit 1
}

$Panel = $Panel.TrimEnd('/')

Write-Host "==> 安装目录: $BaseDir (角色: $Roles, 运行时: $Runtime)"

New-Item -ItemType Directory -Force -Path $BaseDir | Out-Null
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null

$arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }

Write-Host "==> 下载 Agent 二进制"
Invoke-WebRequest -Uri "$Panel/downloads/agent/windows/$arch" -OutFile $ExePath -UseBasicParsing

Write-Host "==> 写入 Agent 配置"
$config = [ordered]@{
    panel_url = $Panel
    node_key  = $NodeKey
    secret    = $NodeSecret
    roles     = $Roles
    runtime   = $Runtime
    data_dir  = $DataDir
} | ConvertTo-Json -Depth 4

[System.IO.File]::WriteAllText($ConfigPath, $config, (New-Object System.Text.UTF8Encoding $false))

if ($Roles -match "frpc") {
    try { Invoke-WebRequest -Uri "$Panel/downloads/frpc/latest/windows/$arch" -OutFile (Join-Path $DataDir "frpc.exe") -UseBasicParsing } catch { }
}
if ($Roles -match "frps") {
    try { Invoke-WebRequest -Uri "$Panel/downloads/frps/latest/windows/$arch" -OutFile (Join-Path $DataDir "frps.exe") -UseBasicParsing } catch { }
}

# 未引入第三方服务包装器，用计划任务实现开机自启与后台常驻
Write-Host "==> 注册计划任务 $TaskName（开机自启）"
$action  = New-ScheduledTaskAction -Execute $ExePath -Argument "--config `"$ConfigPath`"" -WorkingDirectory $DataDir
$trigger = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) -StartWhenAvailable

Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force | Out-Null
Start-ScheduledTask -TaskName $TaskName

Write-Host "==> 安装完成，任务状态："
Get-ScheduledTask -TaskName $TaskName | Select-Object TaskName, State | Format-Table -AutoSize
