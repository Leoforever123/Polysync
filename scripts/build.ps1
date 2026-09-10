param([string]$Version = "0.3.0")
$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$DistDir = Join-Path $ProjectRoot "dist"
$GoCommand = Get-Command go -ErrorAction SilentlyContinue
$Go = if ($GoCommand) { $GoCommand.Source } else { Join-Path $ProjectRoot ".cache/toolchain/go/bin/go.exe" }
if (-not (Test-Path -LiteralPath $Go)) { throw "Install Go 1.26+ or place it in .cache/toolchain/go." }
$Previous = @{ GOOS=$env:GOOS; GOARCH=$env:GOARCH; GOCACHE=$env:GOCACHE; GOMODCACHE=$env:GOMODCACHE }
$Targets = @(
 @{ OS="windows"; Arch="amd64"; Extension=".exe" },
 @{ OS="linux"; Arch="amd64"; Extension="" },
 @{ OS="linux"; Arch="arm64"; Extension="" },
 @{ OS="darwin"; Arch="amd64"; Extension="" },
 @{ OS="darwin"; Arch="arm64"; Extension="" }
)
New-Item -ItemType Directory -Force $DistDir | Out-Null
Push-Location $ProjectRoot
try {
 $env:GOCACHE = Join-Path $ProjectRoot ".cache/go-build"
 $env:GOMODCACHE = Join-Path $ProjectRoot ".cache/go-mod"
 foreach ($Target in $Targets) {
  $env:GOOS=$Target.OS
  $env:GOARCH=$Target.Arch
  $Output=Join-Path $DistDir ("polysync-{0}-{1}{2}" -f $Target.OS,$Target.Arch,$Target.Extension)
  $Flags="-s -w -X main.version=$Version"
  if ($Target.OS -eq "windows") { $Flags += " -H=windowsgui" }
  Write-Host "Building $Output"
  & $Go build -trimpath -ldflags $Flags -o $Output ./cmd/polysync
  if ($LASTEXITCODE -ne 0) { throw "Build failed: $($Target.OS)/$($Target.Arch)" }
 }
} finally {
 $env:GOOS=$Previous.GOOS
 $env:GOARCH=$Previous.GOARCH
 $env:GOCACHE=$Previous.GOCACHE
 $env:GOMODCACHE=$Previous.GOMODCACHE
 Pop-Location
}
Write-Host "Build complete: $DistDir"
