param(
    [string]$Version = "0.1.0"
)

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$DistDir = Join-Path $ProjectRoot "dist"
$CacheDir = Join-Path $ProjectRoot ".cache\go-build"
$PreviousGOOS = $env:GOOS
$PreviousGOARCH = $env:GOARCH
$PreviousGOCACHE = $env:GOCACHE

New-Item -ItemType Directory -Force -Path $DistDir,$CacheDir | Out-Null
$env:GOCACHE = $CacheDir
$Targets = @(
    @{ OS = "windows"; Arch = "amd64"; Extension = ".exe" },
    @{ OS = "linux"; Arch = "amd64"; Extension = "" },
    @{ OS = "linux"; Arch = "arm64"; Extension = "" },
    @{ OS = "darwin"; Arch = "amd64"; Extension = "" },
    @{ OS = "darwin"; Arch = "arm64"; Extension = "" }
)

try {
    foreach ($Target in $Targets) {
        $env:GOOS = $Target.OS
        $env:GOARCH = $Target.Arch
        $Output = Join-Path $DistDir ("polysync-{0}-{1}{2}" -f $Target.OS,$Target.Arch,$Target.Extension)
        Write-Host "Building $Output"
        go build -trimpath -ldflags "-s -w -X main.version=$Version" -o $Output ./cmd/polysync
    }
} finally {
    $env:GOOS = $PreviousGOOS
    $env:GOARCH = $PreviousGOARCH
    $env:GOCACHE = $PreviousGOCACHE
}

Write-Host "Build complete: $DistDir"

