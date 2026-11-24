param(
    [string]$Serial = '',
    [string]$Adb = '',
    [string]$Apk = '',
    [switch]$Test
)
$ErrorActionPreference = 'Stop'
$project = Split-Path $PSScriptRoot -Parent
if (!$Adb) {
    $Adb = Join-Path $project '.cache\android-windows\platform-tools\adb.exe'
    if (!(Test-Path -LiteralPath $Adb)) { $Adb = (Get-Command adb -ErrorAction Stop).Source }
}
if (!$Apk) { $Apk = Join-Path $project 'dist\polysync-android-arm64-preview.apk' }
if (!(Test-Path -LiteralPath $Apk)) { throw "APK 不存在，请先运行 scripts/build-android.sh：$Apk" }
$deviceArgs = @()
if ($Serial) { $deviceArgs = @('-s', $Serial) }
function Invoke-Adb([string[]]$Arguments) {
    & $Adb @deviceArgs @Arguments
    if ($LASTEXITCODE -ne 0) { throw 'adb 操作失败；检查 USB 授权、手机上的安装确认和设备序列号。' }
}
Invoke-Adb @('install', '-r', $Apk)
if ($Test) {
    $testApk = Join-Path $project 'apps\android\app\build\outputs\apk\androidTest\debug\app-debug-androidTest.apk'
    if (!(Test-Path -LiteralPath $testApk)) { throw '请先构建 :app:assembleDebugAndroidTest。' }
    Invoke-Adb @('install', '-r', '-t', $testApk)
    Write-Host '测试开始后请保持手机解锁；如果系统停留在设置页面，请手动打开 PolySync 预览版。'
    $result = & $Adb @deviceArgs shell am instrument --user 0 -w -e class 'org.polysync.android.CoreDeviceTest,org.polysync.android.AppLifecycleTest,org.polysync.android.DesignDeviceTest' 'org.polysync.android.preview.test/android.test.InstrumentationTestRunner' 2>&1
    $result | Write-Output
    if ($LASTEXITCODE -ne 0 -or ($result -join "`n") -notmatch 'OK \(8 tests\)') { throw '真机测试未通过，详见上面的输出。' }
}
Invoke-Adb @('shell', 'am', 'start', '-n', 'org.polysync.android.preview/org.polysync.android.MainActivity')
