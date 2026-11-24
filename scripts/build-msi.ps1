param(
 [string]$Version = "0.5.2",
 [string]$Binary = "",
 [string]$Output = "",
 [string]$Wix = ""
)
$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot
if ($Version -notmatch '^(\d{1,3})\.(\d{1,3})\.(\d{1,5})$' -or [int]$Matches[1] -gt 255 -or [int]$Matches[2] -gt 255 -or [int]$Matches[3] -gt 65535) { throw "MSI requires major.minor.patch (major/minor <=255, patch <=65535)." }
if (-not $Binary) { $Binary = Join-Path $ProjectRoot "dist/polysync-windows-amd64.exe" }
if (-not $Output) { $Output = Join-Path $ProjectRoot "dist/polysync-windows-amd64.msi" }
if (-not $Wix) { $Wix = Join-Path $ProjectRoot ".cache/wix4/wix.exe" }
if (-not (Test-Path -LiteralPath $Binary)) { throw "Build the Windows binary first: $Binary" }
if (-not (Test-Path -LiteralPath $Wix)) { throw "Install WiX: dotnet tool install wix --tool-path .cache/wix4 --version 4.0.6" }
Push-Location $ProjectRoot
try {
 & $Wix extension add WixToolset.UI.wixext/4.0.6
 if ($LASTEXITCODE -ne 0) { throw "WiX UI extension installation failed" }
 & $Wix build packaging/windows/PolySync.wxs -arch x64 -culture zh-CN -ext WixToolset.UI.wixext -d "Version=$Version" -d "Binary=$Binary" -d "Notice=$ProjectRoot/packaging/windows/notice.rtf" -o $Output
 if ($LASTEXITCODE -ne 0) { throw "MSI build failed" }
 Write-Host "MSI built: $Output"
} finally { Pop-Location }
