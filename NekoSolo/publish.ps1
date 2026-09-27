# NekoSolo 安装器构建与发布脚本：构建 stub 并复制到 NekoSolo/build/
# （启动器导出 exe 时默认在此查找 NekoSolo.Installer.exe）
$ErrorActionPreference = "Stop"
$root = $PSScriptRoot

Write-Host "[1/2] 构建 NekoSolo.Installer (net48, WPF)…" -ForegroundColor Cyan
dotnet build (Join-Path $root "NekoSolo.Installer\NekoSolo.Installer.csproj") -c Release
if ($LASTEXITCODE -ne 0) { throw "构建失败" }

$source = Join-Path $root "NekoSolo.Installer\bin\Release\net48\NekoSolo.Installer.exe"
$target = Join-Path $root "build"
New-Item -ItemType Directory -Force -Path $target | Out-Null
Copy-Item -Force -Path $source -Destination (Join-Path $target "NekoSolo.Installer.exe")

Write-Host "[2/2] 已发布到 $target\NekoSolo.Installer.exe" -ForegroundColor Green
Write-Host "现在可以在 NekoLauncher 的「创作中心 → 整合包制作」中选择 NekoSolo（.exe）格式导出。"
