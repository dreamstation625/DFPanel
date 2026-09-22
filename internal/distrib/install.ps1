# DFPanel Agent 一键安装脚本（Windows）
# 用法：
#   powershell -ExecutionPolicy Bypass -Command "irm http://<panel>:7226/install.ps1 -OutFile install.ps1; .\install.ps1 -Panel http://<panel>:7226 -NodeKey <KEY> -NodeSecret <SECRET> -Roles frps,frpc"
param(
    [Parameter(Mandatory = $true)][string]$Panel,
    [Parameter(Mandatory = $true)][string]$NodeKey,
    [Parameter(Mandatory = $true)][string]$NodeSecret,
    [string]$Roles = "frpc",
    [ValidateSet("process", "docker")][string]$Runtime = "process"
)

$ErrorActionPreference = "Stop"

# ProgramData 下写入可避开 Program Files 的 UAC 限制
$BaseDir    = Join-Path $env:ProgramData "dfpanel-agent"
$DataDir    = Join-Path $BaseDir "data"
$ConfigPath = Join-Path $BaseDir "agent.json"
$ExePath    = Join-Path $BaseDir "dfpanel-agent.exe"

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
$taskName = "DFPanelAgent"
Write-Host "==> 注册计划任务 $taskName（开机自启）"
$action  = New-ScheduledTaskAction -Execute $ExePath -Argument "--config `"$ConfigPath`"" -WorkingDirectory $DataDir
$trigger = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) -StartWhenAvailable

Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force | Out-Null
Start-ScheduledTask -TaskName $taskName

Write-Host "==> 安装完成，任务状态："
Get-ScheduledTask -TaskName $taskName | Select-Object TaskName, State | Format-Table -AutoSize
