# 安装与应用内更新（0.4.0）

2026-10-25 更新：Windows 构建已在独立测试产品中通过真实 MSI 安装、更新助手升级/重启、降级拒绝、卸载与数据保留验证，9 月的应用控制阻碍本次没有复现。MSI/EXE 仍未做发布者签名。Ubuntu/Arch 新增原生安装包，通过官网及 GitHub Release 下载，见 [Linux 安装说明](linux-installation.md) 和 [本轮验证记录](packaging-verification-2026-10-25.md)。v0.3.0 没有应用内安装功能，迁移需手动下载一次 MSI。

## 使用方式

Windows x64 默认提供 `polysync-windows-amd64.msi`，直接运行的 EXE 保留给便携使用。安装位置是当前用户 `%LOCALAPPDATA%\Programs\PolySync`，开始菜单可启动，Windows“已安装的应用”可卸载。无需安装 Go、.NET 或额外浏览器运行时。安装器本身不添加开机启动项。

从旧 EXE 迁移：等待任务完成，从托盘退出旧程序，安装 MSI，再从开始菜单启动。默认 `%APPDATA%\Polysync` 数据目录不变。自定义数据目录用户必须保留 `-data-dir` 参数，不能改用空配置后误以为数据丢失。

页面底部、托盘菜单均有“版本与更新”：检查正式版 → 下载安装包 → 安装并重启。更新从固定 GitHub 仓库的 latest release 获取，比较三段版本号；不会安装草稿、预发布或较旧版本。下载需连通 GitHub。官网下载页在正式 Release 存在 MSI 时优先显示 MSI，否则继续提供已发布 EXE，避免开发期出现无效链接。

MSI 主升级会移除旧版本并安装新版本，保留原安装目录，拒绝降级。配置、设备身份、配对、同步目录和历史不属于安装器文件，升级与卸载不删除这些内容。Windows Installer 的版本和升级规则见 [Microsoft 文档](https://learn.microsoft.com/en-us/windows/win32/msi/major-upgrades)。

## 更新实现与后台开销

- 检查由用户点击触发，没有自动联网检查定时器。状态窗口打开时每 1.5 秒读取本地状态，关闭即停止。
- 安装包用 64 KiB 缓冲流式写磁盘，核对声明大小与 SHA-256；下载、安装前和退出主进程后都做校验。不把整包装进内存。
- 只接受固定仓库、版本和资源名，HTTPS 重定向限制在 GitHub 下载域名。更改状态的接口要求本机连接、正确 Host 和本次进程的随机令牌。
- 安装前原子检查活动同步、上传和收发任务；忙碌时继续运行，允许任务完成后重试。进入安装后拒绝新任务，防止重启与传输竞争。
- 临时 helper 复制当前程序，确认能等待主进程后才允许主进程正常退出。helper 运行 `msiexec`，安装完成后按原数据目录及监听地址启动新版。安装失败尝试恢复旧程序，启动失败保留可读错误。
- helper 仅在安装期间存在，无第二个常驻服务。更新目录为 `%LOCALAPPDATA%\Polysync\updates`，包含下载、helper、`install.log` 和 `last-result.json`。成功下载和 helper 缓存目前保留用于排错，尚无自动磁盘清理。

SHA-256 与 HTTPS 用于下载完整性和来源限制，不等于独立的发布者签名。当前 MSI/EXE 未做 Authenticode 签名；受应用控制策略管理的 Windows 可能拒绝运行，代码不会修改或绕过该策略。Linux/macOS 可检查版本，应用内下载安装暂未实现。

## 构建和发布材料

需要 Go 1.26+；Windows 打包需要 .NET SDK 和固定 WiX 4.0.6：

```powershell
dotnet tool install wix --tool-path .cache/wix4 --version 4.0.6
./scripts/build.ps1 -Version 0.4.0
# 已有 Windows EXE 时单独打包：
./scripts/build-msi.ps1 -Version 0.4.0
```

Release 必须同时上传 `polysync-windows-amd64.msi` 与包含它的 `SHA256SUMS.txt`，且标签为 `v0.4.0` 这样的三段正式版本。校验清单应在最终签名完成后生成。不能把验证用的 0.4.1 测试包发布为正式版本。官网现有静态回退链接仍指向已发布的 v0.3.0；正式发布时再更新版本文字与回退链接。本次仅构建，没有提交、推送或发布。

## 2026-09-15 验证记录

- Linux Go 全量 `go test ./...`、`go vet ./...` 通过；本机 Windows Go 被应用控制策略拦截，采用 WSL 中官方校验过的 Go 工具链编译，不修改安全策略。
- 更新测试覆盖版本选择、来源限制、校验失败、下载取消、重复请求、本机令牌、活动任务互斥、helper 等待退出、安装失败恢复、启动失败提示与结果写入失败后继续启动。
- UI 自动化覆盖桌面、390 px 窄屏、空状态、弹窗、键盘焦点、减少动态效果、检查/下载/安装状态、忙碌错误与关闭窗口停止轮询。
- Windows 原生 update/service/tray/main 测试通过，包括托盘更新菜单分发、双击、退出和重新启动。五个系统/架构目标构建通过；真实双实例 TLS 随传浏览器回归通过。Node 单元测试 11 项通过，app.js、update.js、site/app.js 语法检查通过，官网 MSI 选择的真实浏览器检查通过。更新弹窗的桌面与窄屏截图已人工检查。
- 真实 MSI 使用独立测试 UpgradeCode、注册表键、快捷方式和临时安装目录。首次安装通过；修复自定义安装目录升级后，核对已安装 EXE 哈希与 0.4.1 夹具一致；降级返回 1603；卸载成功且配置/用户测试文件保留，测试注册清理完成。
- 完整 `node tests/msi.integration.cjs` **未通过**：真实 helper 安装成功，但 Windows 应用控制策略拒绝启动新版，错误为 `An Application Control policy has blocked this file`。不能声称该机器上的自动重启端到端通过。安装日志位于 `.cache/msi-test-DWJUmB/updates/install.log`；此后独立完成了哈希核对、降级和卸载验证。

在允许运行签名/受信任构建的 Windows 环境，重新运行 `node tests/msi.integration.cjs` 验证全链路；它需要 0.4.0 的 `dist/polysync-windows-amd64.exe` 和 0.4.1 的 `.cache/polysync-upgrade-fixture.exe`。测试只使用专用产品族，不触碰真实同步目录。
