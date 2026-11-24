# Android 预览版

当前版本为 `0.5.0-dev.5`（versionCode 5），适用于 Android 10 及以上、arm64 设备。应用名称为“PolySync 预览版”，包名为 `org.polysync.android.preview`。dev.4 与更早版本使用另一张签名证书。此版可与“PolySync 调试版”同时安装，会创建独立的设备身份和应用私有工作区。首次使用请重新配对，然后接受文件夹邀请。旧应用内的接收文件和本地修改，仍可从旧应用导出。下载与安装均通过项目发布的 APK。

## 使用

1. 手机和电脑连接同一 Wi-Fi。电脑运行 PolySync，手机打开应用。
2. 手机“设备 → 附近设备”会自动显示同一网络的 PolySync，点击“连接设备”。路由器阻断组播时，使用“输入设备地址”，输入电脑界面显示的地址。
3. 电脑批准配对，在手机输入电脑显示的六位验证码。双方保留设备身份和公钥，继续使用现有 TLS 1.3 协议。
4. “随传”在只配对一台设备时直接显示接收方，有多台时先选择设备，再通过系统文件选择器发送；电脑也可向已配对手机发送。收到后点击“导出”，用系统保存对话框选择位置。
5. 底部“同步”管理工作区：从任一端邀请已配对设备，另一端接受。同一工作区可邀请多台已配对设备。“共同编辑”的设备会传递文件修改，“跟随此文件夹”的设备从邀请方接收副本。邀请时可选择允许回传。之后开启回传，跟随设备要先核对新增、修改和删除的文件，再确认。
6. “成员与权限”可单独停止与某台设备同步，文件仍可查看与导出。能回传的工作区导入文件副本后会触发一次同步，也可以点击“立即同步”。设备接受邀请后执行首次同步。手机以按需同步为主，电脑仍遵循各自文件夹设置。

手机工作区位于应用私有目录，**导入的是副本，不会自动监控原始系统文件夹**。同名导入会拒绝覆盖；原有协议的修改、删除、冲突和历史归档语义保持不变。冲突先在电脑界面处理。卸载应用会删除私有工作区、接收文件和配对身份，请先导出重要文件。

`0.5.0-dev.2` 已接入 Android 系统 NSD 的 mDNS/DNS-SD 自动发现，使用与桌面相同的 `_polysync._tcp` 类型和 TXT 身份字段。手机和电脑可互相发现；结果只作为地址线索，配对仍验证广播公钥与 TLS 证书一致，并需要验证码。系统分享入口、整目录 SAF 同步、蓝牙及断点续传仍未实现；同步和随传分别复用现有核心，尚未对同步大文件峰值做优化。

界面根据系统切换浅色和深色，沿用绿色卡片、原生矢量图标、底部“设备 / 随传 / 同步”导航，区分发现、配对、收发和同步状态。右上角“接收中 / 已停止”可管理服务和重新发现。手机地址可复制，传输进度就地更新，普通轮询保留当前控件和焦点。没有额外网页渲染引擎或字体下载。

## 后台与资源边界

- Kotlin 原生界面 + 同进程 Go 库；没有 WebView、浏览器、额外 HTTP 监听端口或更新服务。预览版监听 TCP 45124，旧调试版使用 45123。手动连接时请复制应用内显示的地址。
- 用户打开应用时启动前台服务。Android 13 及以上可在“接收中 → 开启后台通知”授权显示接收通知。可从应用或通知停止服务。
- 前台界面每 2 秒读取本地状态，离开后停止界面轮询；服务每 10 秒检查本地任务状态。NSD 由系统负责局域网广播和浏览，通过发现 / 丢失事件更新 Go 的内存设备表；应用不再额外运行 Go mDNS 套接字或扫描定时器，停止服务时注销 NSD。未增加定时文件夹扫描。
- 后台空闲 10 分钟后停止服务；传输或同步活跃时延后。使用系统 dataSync 前台服务类型并处理系统超时，不承诺绕过 Android / vivo 后台限制实现永久在线。
- 系统文件 URI 通过 64 KiB 缓冲导入私有临时文件，再流式交给核心；导入完成后删除该临时文件。进程被系统强制终止时可能留下暂存文件，后续补充清理策略。
- 这里只完成运行基础，不把 APK 大小、Go 堆、Android PSS 混为“内存占用”。常驻内存、传输峰值、事件触发同步另行测量和优化。

## 构建

本次 Windows 通过 WSL `Ubuntu-26.04` 编译，Windows adb 连接实体手机。依赖均安装在忽略的 `.cache`，不提交 SDK、密钥、设备身份或 APK。

工具版本：Go 1.27.1（项目最低 1.26）、JDK 21.0.2、Gradle 8.13、AGP 8.13.2、Kotlin 2.2.21、Android platform 36、build-tools 35.0.0、NDK 28.2.13676358、`golang.org/x/mobile v0.0.0-20260908204917-8b95e45f8d3e`。Windows 平台工具放在 `.cache/android-windows/platform-tools`。

在 Linux / WSL 安装 Android SDK 与 NDK（官方 `sdkmanager`），接受 Android SDK 许可后：

```sh
sdkmanager 'platforms;android-36' 'build-tools;35.0.0' 'ndk;28.2.13676358'
go install golang.org/x/mobile/cmd/gomobile@v0.0.0-20260908204917-8b95e45f8d3e
go install golang.org/x/mobile/cmd/gobind@v0.0.0-20260908204917-8b95e45f8d3e
export ANDROID_HOME=/path/to/sdk JAVA_HOME=/path/to/jdk
export GRADLE=/path/to/gradle-8.13/bin/gradle
export PATH="$(go env GOPATH)/bin:$PATH"
sh scripts/build-android.sh
```

本仓库现成缓存布局可直接：

```powershell
wsl -d Ubuntu-26.04 -- sh /mnt/e/wjdfo/Documents/ChatGPT/Polysync/scripts/build-android.sh
./scripts/install-android.ps1
```

产物为 `dist/polysync-android-arm64-preview.apk`。同一预览包名的后续发行需沿用相同签名证书，才能用 `adb install -r` 保留预览版数据。签名密钥在开发环境或 CI 的机密存储中管理。

手机需要开启 USB 调试、允许该电脑；vivo 可能另需“USB 安装”及逐次安装确认。安装脚本不关闭系统安装保护。

## 验证

```sh
go test ./...
go vet ./...
node --test tests/ui.test.cjs
node --check internal/webui/static/app.js
```

构建脚本运行 Kotlin 单元测试和 Android lint，并产生 instrumentation APK。

```powershell
./scripts/install-android.ps1 -Test
```

`CoreDeviceTest` 在手机的隔离私有目录中运行两个真实 Go 核心，验证验证码配对、2 MiB 随传字节一致、中文文件名、工作区同步、越界拒绝和十次重启身份保持。`AppLifecycleTest` 检查应用服务三轮停止、端口释放、重启。

新版补充 `DesignDeviceTest` 检查底部导航、空状态与轮询控件稳定性；`DiscoveryDeviceTest` 通过 adb reverse 读取隔离桌面控制接口，验证真实 Wi-Fi 上双向 mDNS 发现，不向核心注入伪造发现结果。Android NSD 依据[官方文档](https://developer.android.com/develop/connectivity/wifi/use-nsd)实现；当前 target SDK 为 36，未来提高 target SDK 时需重新核对[局域网权限](https://developer.android.com/privacy-and-security/local-network-permission)。

`DesktopInteropTest` 必须单独运行并提供测试参数：电脑以**全新、无既有传输记录**的临时 `-data-dir`、TCP 45423、HTTP 127.0.0.1:45424 启动；临时同步目录内放 `desktop.txt`，内容为 `Windows to Android over real Wi-Fi`（无换行）。端口被占用时可换端口，并相应调整 `desktopAddress` 和 adb reverse 的电脑端端口。

```powershell
adb reverse tcp:45424 tcp:45424
adb shell am instrument -w -e class org.polysync.android.DesktopInteropTest -e desktopAddress <电脑局域网IP:45423> -e desktopRoot <电脑临时文件夹> org.polysync.android.preview.test/android.test.InstrumentationTestRunner
adb reverse --remove tcp:45424
```

Windows 的 `desktopRoot` 参数应使用正斜杠（例如 `E:/path/to/test/workspace`），避免 adb shell 吞掉反斜杠。先确认临时桌面实例启动且 `/api/status` 返回 200；如果端口被 Windows 保留，请改用可用端口并更新 adb reverse。控制接口通过 adb reverse；加密文件通信走真实 Wi-Fi。测试后检查电脑收到的随传文件和 `phone.txt` 内容，再关闭临时桌面实例。不得用用户真实同步目录代替临时目录。

界面手动检查：设备 / 随传 / 工作区空状态、添加设备输入与取消、验证码对话框、系统文件选择 / 保存、旋转与较大字体、键盘焦点、通知停止后重启。记录真机通过项与未验证项，不能用 APK 构建成功代替真机通过。

## dev.5 的原生交互与互通验证

2026-11-24 在独立的 Android 15 模拟器与临时 Linux 桌面服务上实际配对并传输了文件。手机输入桌面显示的验证码，单一接收方直接打开系统文件选择器；从手机发出的只接收邀请在桌面记录为 `follower` 且禁止回传。打开“成员与权限”选择停止同步后，取消确认会保留该关系。再次操作并确认停止同步、随后在设备页取消配对，工作区仍保留“查看与导出”入口。浅色、深色、系统动画比例为零、窄窗口及横屏均通过实际点按和滚动检查。

八个 Android instrumentation 用例覆盖双端真实 TLS、2 MiB 文件内容、只接收工作区、导出、后台启停、角色与权限、千条历史的增量更新及系统绘制。主机 GPU 加速的 Android 15 x86_64 模拟器使用 60 Hz、1080 × 2400。加载 1,000 条记录、实际显示前 20 条并连续来回滚动，单独运行测量用例采集了 78 帧。第 95 百分位的帧间隔为 16.67 ms，渲染耗时为 17.09 ms，未出现大于 25 ms 的帧间隔。同时运行 Gradle 构建和多个模拟器的样本中，68 帧有 10 次间隔超过 25 ms。本轮没有接入实体手机。

另外使用支持 ARM 翻译的 Google APIs Android 15 镜像并装了发布的 dev.4 APK 与 dev.5 ARM APK，包管理器保留了两个独立的应用 ID。该镜像的 ARM 转译器执行 Go 运行时读取 CPU 型号的 `MIDR_EL1` 指令时，对两个版本均返回 `SIGILL`。同机也尝试用官方 arm64-v8a AOSP 镜像及软件模拟启动，当前 Android Emulator 拒绝在 x86_64 主机上运行此架构。上述功能与帧间隔数据来自原生 x86_64 Android 构建，ARM APK 完成了构建、安装、签名及 16 KiB 对齐检查。最终 APK 为 11,990,612 字节，安装后读取的 APK 和本地 SHA-256 均为 `5fb3cbb18eb2adcfdeaaf5f328b50b1a4df43c923ff250d9444a41164aaf85ed`。实体 arm64 手机的运行体验待验证。

## 2026-09-21 验证记录

- 设备：vivo V2417A，Android 16，arm64-v8a；APK 已通过 USB 安装，未上架。
- Go `go test ./...`、`go vet ./...` 通过；Windows amd64、Linux amd64/arm64、macOS amd64/arm64 核心编译通过。跨平台编译不代表各系统运行验证。
- Kotlin 单元测试 2 项通过；Android lint 0 errors，保留 ChromeOS ABI、较保守的磁盘空间检查、国际化文字等提示。Gradle 构建及 APK 签名、16 KiB ZIP 对齐检查通过。
- 真机 instrumentation：`OK (4 tests)`。覆盖十次 Go 引擎重启保持身份、2 MiB 随传内容一致、中文文件名、工作区同步与导出、路径越界拦截、三轮 Android 服务启停与端口释放，以及 Windows ↔ Android 的真实 Wi-Fi 同步。手机到 Windows 的 480000 字节随传文件和同步文件另外在电脑端计算 SHA-256，与原始数据一致。
- 真机曾发现应用目录别名导致工作区导出误报“路径越界”；增加 Unix 路径别名回归测试，先复现失败，再统一解析目录与文件路径后通过，同时保留外部符号链接拦截。
- vivo 对无前台页面的测试启动有限制。测试保持应用可见，必要时在 instrumentation 启动后手动打开 PolySync；不修改手机安全策略或省电设置。
- 界面：竖屏三个页面的空状态、添加设备地址输入 / 取消、键盘 Tab 操作、停止 / 启动按钮均检查通过，并查看截图。系统文件选择器 / 保存对话框的完整流程、横屏 / 大字体、通知按钮、长时间后台 / 锁屏 / 网络切换，以及 Android 10–15 设备尚未完整验证。
- 测试使用隔离数据，手机测试目录在用例结束后清理；临时 Windows 进程及 adb reverse 已关闭。常驻内存与大文件峰值未作为本轮优化或验收指标。

上述初版 APK 的 SHA-256 为 `ee0ae76a56fd251eb7bba2d8d62d589821278dce04de742d53b9da683ca37b37`，当时确认与手机安装版本一致。

## 2026-09-22 自动发现与界面更新

- 版本：`0.5.0-dev.2`。接入系统 NSD 和原生设计组件，没有新增 WebView 或 UI 框架依赖。
- `go test ./...`、`go vet ./...` 通过。新增外部发现结果验证、失效清理、身份不可自动信任、广播公钥与 TLS 不一致时拒绝配对的测试。五个桌面目标重新编译通过。
- Android 构建、Kotlin 单元测试、lint（0 errors）、APK 签名及 16 KiB 对齐检查通过。
- vivo Android 16 真机：`DiscoveryDeviceTest` + `DesignDeviceTest` 返回 `OK (2 tests)`，验证真实 Wi-Fi 上 Windows 与手机双向自动发现、三个导航页及空状态、轮询不重建未变化的按钮。此前安装的新版主应用已具备这些行为。
- 打包脚本先清理 Android 构建产物，去除增量 APK 中旧原生库留下的空隙；最终产物 11,733,536 字节（约 11.2 MiB）。不会清理应用数据、SDK 或调试签名。

- 最终 APK 已覆盖安装到 vivo V2417A；手机 `base.apk` 与本地最终构建 SHA-256 一致。安装在 vivo 外部来源提示页完成本次确认，没有关闭系统安全策略。
- 最终包的六项 instrumentation 用例分批验证通过：两项核心协议测试、服务生命周期、原生导航与轮询稳定性、双向真实 mDNS、Windows ↔ Android 局域网互通。首轮互通失败源于 Windows 保留测试端口；更换端口后，同步测试又暴露 adb 参数反斜杠被吞的问题。确认是隔离测试目录传参问题后，改用正斜杠及全新测试目录重跑，`DesktopInteropTest` 返回 `OK (1 test)`。
- Windows 实际收到的随传文件和工作区 `phone.txt` 均为 480000 字节，磁盘 SHA-256 均与手机原始数据一致；手机读取的 `desktop.txt` 与 Windows 原始内容一致。
- 最终包界面实查通过：三个页面及空状态、附近设备卡片、手动添加地址输入 / 取消 / Tab、竖屏与横屏切换、横屏滚动到发送按钮、服务停止 / 重启。已逐张检查截图；缓存截图位于 `.cache/android-v2-*.png`，不加入 Git。屏幕方向已恢复，手机停留在设备页。
- 尚未完成：大字体与系统减少动画设置、系统文件选择 / 保存的完整界面流程、长时间后台 / 锁屏 / 网络切换、Android 10–15 真机兼容性。当前后台空闲十分钟停止的策略保持不变，常驻内存和大文件峰值仍留待后续专项验证。
- 测试使用隔离数据，临时 Windows 实例及 adb reverse 在验证后关闭，参数误生成的测试目录已确认位置并清理。

最终 APK：`dist/polysync-android-arm64-debug.apk`，11,733,536 字节（约 11.2 MiB），SHA-256：`1478f0cd7c82378aaef2cd4c7fd23bcf88828075781f8772a6e0cf6bba6913f7`。

## 2026-09-24 基础修复更新

调试包推进到 `0.5.0-dev.3`（versionCode 3）。后台每十秒改用轻量活动检查，并计入原生排队/导入/导出；用户主动停止会保存在本机，Activity 重建或重开不再自行重启，需点击启动或重新发现。十分钟后台空闲停止规则未取消。发现更新失败显示提示，单个无效发现条目不再阻塞其他设备更新。

构建包含两项新的 BackgroundPolicy 单元测试，AppLifecycleTest 增加主动停止后重建与明确重启用例，`install-android.ps1 -Test` 现在要求 5 项真机用例通过。完整修复和验证边界见 [基础风险修复记录](basic-risk-fixes-2026-09-24.md)。

最终 dev.3 已覆盖安装。2026-09-24 的 CoreDeviceTest、AppLifecycleTest、DesignDeviceTest 合计 `OK (5 tests)`，147.487 秒，包含新加的主动停止后 Activity 重建保持停止及明确重启。Kotlin 单元测试 4 项、lint、共享核心五目标构建和 Go 竞态检测通过。最终 APK 11,751,712 字节，SHA-256：`230097af64f579555062ff713f1422442c9d2260acaebb92c78f791c6364409d`。mDNS 双向真网络和 Windows/Android 双向互通没有在此轮重新执行，仍以此前记录为历史依据。

## 随桌面版本发布实验包

用本机预览签名构建、验证 APK 后，将发行文件放入仓库的 Git LFS 存储，源码中只保存指针。下面以桌面 v0.5.0 为例。保持签名密钥在原构建环境中。

```sh
mkdir -p packaging/android
git lfs clean < dist/polysync-android-arm64-preview.apk > packaging/android/preview-v0.5.0.lfs
git lfs push --object-id origin "$(sed -n 's/^oid sha256://p' packaging/android/preview-v0.5.0.lfs)"
```

正式标签的构建会通过指针取回同一份 APK，检查 Android 签名与 SHA-256 后把它放进 Release。普通源码检出不需要下载这个安装包。
