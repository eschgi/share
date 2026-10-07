# Builds the website into the server, then the server for Windows PCs (windows/amd64) and
# Windows on ARM (windows/arm64). Output: dist\share-windows-amd64.exe,
# dist\share-windows-arm64.exe.
#
#   scripts\build-windows.ps1                    version from VERSION and git (scripts/version.sh)
#   scripts\build-windows.ps1 -Version 0.1.0
param([string]$Version = "")

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

if (-not $Version) {
    . "$PSScriptRoot\version.ps1"
    $Version = Get-ShareVersion $root
}

Write-Host "Building the website"
Push-Location "$root\web"
try {
    npm ci --no-audit --no-fund
    if ($LASTEXITCODE -ne 0) { throw "npm ci failed" }
    npm run build
    if ($LASTEXITCODE -ne 0) { throw "website build failed" }
} finally { Pop-Location }

New-Item -ItemType Directory -Force "$root\dist" | Out-Null
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
Push-Location "$root\server"
try {
    foreach ($arch in "amd64", "arm64") {
        $env:GOARCH = $arch
        $out = "$root\dist\share-windows-$arch.exe"
        Write-Host "Building $out ($Version)"
        go build -trimpath -ldflags "-s -w -X main.version=$Version" -o $out ./cmd/share
        if ($LASTEXITCODE -ne 0) { throw "go build failed for $arch" }
    }
} finally {
    Pop-Location
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
}

Get-ChildItem "$root\dist\share-*" | ForEach-Object {
    "$((Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower())  $($_.Name)"
} | Set-Content -Encoding ascii "$root\dist\SHA256SUMS"

Write-Host "Done:"
Get-ChildItem "$root\dist"
