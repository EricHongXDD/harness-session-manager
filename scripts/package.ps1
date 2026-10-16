#requires -Version 7.0
. (Join-Path $PSScriptRoot 'common.ps1')
Initialize-Task
$version=(Get-Content -LiteralPath (Join-Path $script:TaskRoot 'wails.json') -Encoding UTF8 | ConvertFrom-Json).info.productVersion
if($version -notmatch '^\d+\.\d+\.\d+$'){throw '无效的发行版本号'}
$exeName="HarnessSessionManager-$version.exe"
$exe=Join-Path $script:TaskRoot "build/bin/$exeName"
if(!(Test-Path -LiteralPath $exe -PathType Leaf)){throw '请先运行 scripts/build.ps1'}
$stage=Join-Path $script:TaskRoot ('.cache/package-'+[Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path (Join-Path $stage 'docs') -Force > $null
Copy-Item -LiteralPath $exe -Destination $stage
Copy-Item -LiteralPath (Join-Path $script:TaskRoot 'README.md'),(Join-Path $script:TaskRoot 'THIRD_PARTY_NOTICES.txt') -Destination $stage
Copy-Item -LiteralPath (Join-Path $script:TaskRoot 'docs/verification.md') -Destination (Join-Path $stage 'docs')
$dist=Join-Path $script:TaskRoot 'dist'
New-Item -ItemType Directory -Path $dist -Force > $null
$zipName="HarnessSessionManager-$version-windows-amd64.zip"
$zip=Join-Path $dist $zipName
Compress-Archive -Path (Join-Path $stage '*') -DestinationPath $zip -CompressionLevel Optimal -Force
Copy-Item -LiteralPath $exe -Destination (Join-Path $dist $exeName) -Force
# 发布前核对压缩包内的 EXE 与单文件下载完全一致。
Add-Type -AssemblyName System.IO.Compression.FileSystem
$archive=[IO.Compression.ZipFile]::OpenRead($zip)
try{
 $entry=$archive.GetEntry($exeName)
 if(!$entry){throw 'ZIP 缺少程序'}
 $stream=$entry.Open()
 try{$digest=[Security.Cryptography.SHA256]::Create();try{$zipExeHash=[Convert]::ToHexString($digest.ComputeHash($stream)).ToLowerInvariant()}finally{$digest.Dispose()}}finally{$stream.Dispose()}
}finally{$archive.Dispose()}
$exeHash=(Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash.ToLowerInvariant()
if($zipExeHash -ne $exeHash){throw 'ZIP 内程序与发布程序不一致'}
$lines=@("$exeHash  $exeName",((Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLowerInvariant()+"  $zipName"))
[IO.File]::WriteAllText((Join-Path $dist 'SHA256SUMS.txt'),(($lines -join "`n")+"`n"),[Text.UTF8Encoding]::new($false))
Write-Output (Join-Path $dist $exeName)
Write-Output $zip
Write-Output (Join-Path $dist 'SHA256SUMS.txt')
