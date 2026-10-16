#requires -Version 7.0
if ($env:OS -eq 'Windows_NT') { chcp 65001 > $null }
[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false)
$OutputEncoding=[Console]::OutputEncoding
$ErrorActionPreference='Stop'
$script:TaskRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
function Initialize-Task {
 Set-Location -LiteralPath $script:TaskRoot
 $env:GOTOOLCHAIN='local'
 $env:GOTELEMETRY='off'
 $env:GOCACHE=Join-Path $script:TaskRoot '.cache/go-build'
 if(!$env:GOMODCACHE){$env:GOMODCACHE=Join-Path $script:TaskRoot '.cache/go-mod'}
 $env:TEMP=Join-Path $script:TaskRoot '.cache/test-tmp'
 $env:TMP=$env:TEMP
 foreach($dir in @($env:GOCACHE,$env:GOMODCACHE,$env:TEMP,(Join-Path $script:TaskRoot '.tools'))){New-Item -ItemType Directory -Force $dir > $null}
}
function Checked([string]$Tool,[string[]]$Arguments){& $Tool @Arguments;if($LASTEXITCODE -ne 0){throw ($Tool+' 失败，退出码 '+$LASTEXITCODE)}}
