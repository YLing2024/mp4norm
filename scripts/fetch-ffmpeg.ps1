# Downloads a static ffmpeg build for Windows into third_party/ffmpeg/bin/.
#
# The build used here (gyan.dev "essentials") is GPL. Redistributing it imposes
# GPL terms on the bundle; if that is a problem, ship an LGPL build instead or
# have users provide their own ffmpeg (mp4norm also reads MP4NORM_FFMPEG and
# PATH).
$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
$dest = Join-Path $root 'third_party\ffmpeg\bin'
New-Item -ItemType Directory -Force -Path $dest | Out-Null

$url = 'https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip'
$zip = Join-Path $env:TEMP 'ffmpeg-release-essentials.zip'
$tmp = Join-Path $env:TEMP ('ffmpeg-extract-' + [guid]::NewGuid().ToString('N'))

Write-Host "downloading $url"
Invoke-WebRequest -Uri $url -OutFile $zip
Expand-Archive -Path $zip -DestinationPath $tmp -Force

$exe = Get-ChildItem -Path $tmp -Recurse -Filter ffmpeg.exe | Select-Object -First 1
if (-not $exe) { throw 'ffmpeg.exe not found in archive' }
Copy-Item $exe.FullName (Join-Path $dest 'ffmpeg.exe') -Force

Remove-Item -Recurse -Force $tmp
Write-Host "installed $dest\ffmpeg.exe"
