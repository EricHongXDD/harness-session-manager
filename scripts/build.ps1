#requires -Version 7.0
param([switch]$Offline)
. (Join-Path $PSScriptRoot 'common.ps1')
Initialize-Task
if($env:OS -ne 'Windows_NT'){throw '此构建入口用于 Windows 本地版'}
if($Offline){$env:GOPROXY='off';$env:GOSUMDB='off'}
Checked node @('scripts/frontend-build.mjs')
$cli=Join-Path $script:TaskRoot '.tools/wails.exe'
if(!(Test-Path -LiteralPath $cli)){throw '请将 Wails CLI v2.15.0 放到 .tools/wails.exe；详见 README'}
$version=(Get-Content -LiteralPath (Join-Path $script:TaskRoot 'wails.json') -Encoding UTF8 | ConvertFrom-Json).info.productVersion
if($version -notmatch '^\d+\.\d+\.\d+$'){throw 'wails.json 必须包含明确的三段版本号'}
$outputName="HarnessSessionManager-$version.exe"
Checked $cli @('build','-platform','windows/amd64','-s','-skipbindings','-webview2','error','-installscope','user','-trimpath','-m','-nosyncgomod','-o',$outputName)
Write-Output (Join-Path $script:TaskRoot "build/bin/$outputName")
