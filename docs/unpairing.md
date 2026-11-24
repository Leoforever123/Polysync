# 取消配对：行为与验证

## 使用方式

- 电脑端有已配对设备时，“同步”和“随传”页面顶部都会显示“管理设备”。点击它，找到对应设备卡片上的“取消配对”，阅读说明后确认。离线设备也可操作。“同步”页面下方的设备卡片提供同一入口。尚未配对时，顶部显示“添加设备”。
- Android：在“设备 → 我的设备”点击“取消配对”。本机服务需处于开启状态，对方不需要在线。
- 点击“保留配对”、关闭弹窗或电脑端按 Escape，不会修改配对。

确认后只撤销本机对该设备的信任。对方无法继续向本机发送随传、同步或同步邀请；对方自己的设备列表需要在对方单独取消。取消后仍可能在“附近设备”看到对方，那只表示发现了设备，不表示已信任。

关联同步标记为“已取消配对”，自动同步关闭。已有文件、工作区、同步基线、历史版本、冲突和随传记录全部保留。Android 工作区仍可导入、查看与导出。待接受的同步邀请和已有配对会话会清除。

正在进行设备控制操作、同步或文件任务时会提示稍后重试，不会静默中断传输。当前检查采用核心已有的全局活动计数，其他设备正在进行同步或文件操作时也可能需要等待；尚未实现按设备取消活动连接。

再次使用前，请在双方取消配对，然后重新通过六位验证码建立信任。旧同步不会随重新配对自动恢复，需重新创建邀请。电脑端可先移除已停用的同步配置（不删除文件）再选择原目录；Android 可从旧工作区导出文件，按需导入新工作区。

## 实现边界

新增 `DELETE /api/paired-devices/{id}`，网页和 Android 移动桥接共用同一核心处理。沿用现有 HTTP 同源保护，移动桥接只开放对应 DELETE 操作。

撤销信任、停用同步和移除邀请在一次配置写入中完成；写入失败恢复内存配置。配对和邀请控制操作与取消配对互斥，活动任务检查与新任务接入使用同一锁，避免已有连接在取消成功后重新保存信任。同步入口检查已取消状态，避免新配对隐式恢复旧同步关系。

本轮没有新增远端取消配对协议，也不修改同步覆盖、删除、冲突、校验或历史归档的语义。

## 2026-10-25 验证

- 先新增 API 回归用例，修复前复现 HTTP 404；修复后通过。
- Windows：`go test ./...`、`go vet ./...` 通过，修改的 Go 文件已执行 gofmt。
- WSL：`go test -race ./internal/service ./internal/store ./mobile` 通过。
- 核心回归覆盖：取消后重启仍不信任、持久化失败回滚、其他设备不受影响、待处理邀请与配对会话清理、活动任务拒绝、跨站请求拒绝、真实回环 TLS 随传及邀请拒绝、双方取消后重新验证码配对、重新配对不自动恢复旧同步、移动工作区仍可导出。
- 前端：`node --test tests/ui.test.cjs` 11 项通过；`node --check internal/webui/static/app.js` 通过。
- Chrome：`node tests/unpair.ui.browser.cjs` 通过。覆盖 1365 px 桌面、390 px 窄屏、空状态、确认/保留/取消、Enter/Escape/Tab 焦点、轮询保持焦点、忙碌错误和重试、取消后的工作区、减少动态效果、名称转义。已检查截图，存放于忽略目录 `.cache/ui-review/unpair/`。
- Windows amd64、Linux amd64/arm64、macOS amd64/arm64 核心构建通过，验证产物在 `.cache/unpair-validation/`。跨平台编译不代表各平台原生运行验收。
- Android：gomobile 绑定、Kotlin 单元测试 4 项、debug APK、instrumentation APK 和 lint 构建通过，lint 无错误、保留 4 项警告。现有 `CoreDeviceTest` 补充取消、文件导出和重启检查；`DesignDeviceTest` 补充取消入口和保留配对的弹窗检查。
- **真机未验证**：`adb devices` 列表为空。新增 Android instrumentation 断言只完成编译，没有在手机上执行；未覆盖安装，不能沿用 dev.3 的真机通过结论。

所有文件测试使用临时目录，网络测试使用回环地址，没有取消用户真实设备的配对。Windows 沙箱最初阻止系统临时目录重命名、部分回环连接及文件句柄操作；将测试临时目录设在项目缓存，并在获准的沙箱外环境运行后，完整测试通过。未改动系统安全策略。

## 复现

在项目根目录 PowerShell 执行，需已有 Go、Node、Chrome 和 `.cache/ui-tools/node_modules` 中的 Playwright：

```powershell
New-Item -ItemType Directory -Force .cache/unpair-tests | Out-Null
$env:TEMP = Join-Path $PWD '.cache/unpair-tests'
$env:TMP = $env:TEMP
$env:GOCACHE = Join-Path $PWD '.cache/go-build'
$env:GOMODCACHE = Join-Path $PWD '.cache/go-mod'
$env:GOFLAGS = '-mod=readonly'
go test ./...
go vet ./...
node --test tests/ui.test.cjs
node --check internal/webui/static/app.js
$env:NODE_PATH = Join-Path $PWD '.cache/ui-tools/node_modules'
node tests/unpair.ui.browser.cjs
```

使用 Playwright 自带 Chromium 时，可设置环境变量 `POLYSYNC_TEST_BROWSER_CHANNEL=chromium` 后运行同一浏览器测试。用例覆盖“随传 → 管理设备 → 取消配对”，包括离线设备、取消后回到原设备卡片、关闭设备列表后回到顶栏，以及任务忙碌时在原确认框按 Enter 重试。

Android 使用 `scripts/build-android.sh`，构建环境见 [Android 调试说明](android-debug.md)。本次通过 `.cache/unpair-validation/build-android.sh` 的 LF 副本运行同一构建步骤。连接并授权手机后，可运行 `scripts/install-android.ps1 -Test`；它会安装调试 APK 并运行五项 instrumentation 测试，不能仅以构建成功代替执行。

当前 APK：`dist/polysync-android-arm64-debug.apk`，`0.5.0-dev.4` / versionCode 4，11,767,872 字节。

SHA-256：`480877ca61259f6403d784bbb944372bfc4543b03cbc2504de5b7626639c2f3f`。

本轮没有提交、推送或正式发布；缓存和二进制保持 Git 忽略。
