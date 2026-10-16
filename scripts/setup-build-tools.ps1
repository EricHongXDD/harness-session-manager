#requires -Version 7.0
param([switch]$Offline)
. (Join-Path $PSScriptRoot 'common.ps1')
Initialize-Task
if($Offline){$env:GOPROXY='off';$env:GOSUMDB='off'}
# 从仓库中固定的 Wails 修订分支编译 CLI，不依赖开发者机器已有的可执行文件。
$cli=Join-Path $script:TaskRoot '.tools/wails.exe'
Push-Location -LiteralPath (Join-Path $script:TaskRoot 'third_party/wails-v2')
try{Checked go @('build','-trimpath','-o',$cli,'./cmd/wails')}finally{Pop-Location}
Checked $cli @('version')
