#requires -Version 7.0
param([switch]$Offline)
. (Join-Path $PSScriptRoot 'common.ps1')
Initialize-Task
if($Offline){$env:GOPROXY='off';$env:GOSUMDB='off'}
Checked node @('scripts/frontend-build.mjs')
Checked node @('--check','frontend/app.js')
Checked node @('--test','--test-isolation=none','frontend/format.test.mjs','frontend/app.test.mjs')
Checked go @('vet','./...')
Checked go @('test','-count=1','-timeout=2m','./...')
