# PolySync

PolySync 是一个面向个人设备的局域网双向文件同步工具。每台设备各选一个本地文件夹，通过加密连接直接交换文件。一个文件夹可由多台设备共同编辑，也可以让另一台设备跟随其中一台接收更新。支持自动与手动同步。

[官方网站与下载](https://leoforever123.github.io/Polysync/) · [最新版本](https://github.com/Leoforever123/Polysync/releases/latest)

## 已实现

- Windows、Linux、macOS 使用同一套 Go 核心
- mDNS 自动发现附近设备，支持手动地址后备
- 接收方批准后显示六位一次性验证码
- Ed25519 长期设备身份、TLS 1.3 加密和配对公钥固定
- 已配对设备列表、在线状态和免重复验证的文件夹邀请
- 多文件夹并行配置，自动识别新增、修改和删除
- Add/Add、Modify/Modify、Modify/Delete 冲突队列和文本三方 Merge UI
- 文件流式传输、SHA-256 校验、旧版本归档和内容去重对象库
- 在线自动重试；可按文件夹关闭自动同步或手动立即同步
- 浏览器本地控制台和系统原生文件夹选择器
- Windows x64 MSI 安装包，以及控制台、托盘中的“版本与更新”入口
- Ubuntu amd64/arm64 `.deb`、Arch x86_64 安装包与离线 PKGBUILD、可选的 Linux 用户级后台服务
- Android arm64 原生调试版：mDNS 自动发现、绿色原生界面、USB 安装、配对、单文件随传与应用内工作区同步，见 [Android 真机调试](docs/android-debug.md)

## 快速开始

需要允许设备间访问 TCP `45123` 和 mDNS UDP `5353`。控制台默认只监听本机 `127.0.0.1:45124`，不会暴露给局域网。

从源码运行：

```bash
go run ./cmd/polysync
```

程序启动后会打开 `http://127.0.0.1:45124`。

1. 两台设备启动后会通过 mDNS 出现在彼此的“附近设备”列表；发现被阻断时可手动填写地址。
2. 在第一台设备点击“配对”，第二台设备确认请求后显示六位验证码。
3. 在第一台设备输入验证码。双方保存 Ed25519 公钥，以后不再重复验证身份。
4. 选择“添加同步”、已配对设备和本地文件夹，向另一台设备发送同步邀请。
5. 另一台设备接受邀请并选择对应本地文件夹，随后通过 TLS 1.3 开始首次同步。

设备只需配对一次，之后可以创建多个互不重叠的同步文件夹。在已有文件夹上选择“添加设备”，可以邀请第三台设备共同编辑或跟随接收。直连范围和回传权限见[多设备文件夹](docs/folder-groups.md)。

取消配对：电脑端从“管理设备”进入已配对设备卡片，点击“取消配对”；Android 在“设备 → 我的设备”中选择“取消配对”，确认后撤销本机信任，离线设备也可移除。与该设备的连接停用，同一文件夹中的其他设备可以继续同步。本地文件、历史和随传记录保留；Android 工作区仍可查看与导出。有设备操作或文件任务进行时请先完成或取消，再重试。

取消配对仅在本机生效，对方需单独取消。重新连接前，先在双方取消配对，再重新验证并创建同步邀请；旧同步不会自动恢复。电脑端如需继续使用原文件夹，可先移除已停用的同步配置（不删除文件）再重新邀请。详见 [取消配对：行为与验证](docs/unpairing.md)。

Windows 安装版：打开 `polysync-windows-amd64.msi`，安装后从开始菜单启动 PolySync。安装到当前用户目录，通常不需要管理员权限。旧版直接运行 EXE 的用户，先从托盘退出旧程序，再安装 MSI；默认配置目录不变，无需重新配对。使用过自定义 `-data-dir` 的用户需继续使用该参数启动。

在页面底部或 Windows 托盘打开“版本与更新”，依次检查更新、下载安装包、安装并重启。正在同步或随传时会暂缓安装；升级保留配置和数据。该功能按需检查，不新增定时联网检查或常驻更新服务。当前仅 Windows x64 支持应用内安装；详见 [安装与更新](docs/installation-and-updates.md)。

Linux 用户可从 [官网下载页](https://leoforever123.github.io/Polysync/#downloads) 或 [GitHub Release](https://github.com/Leoforever123/Polysync/releases/latest) 点击下载 Ubuntu / Arch 安装包。下载后在所在目录打开终端：

```sh
sudo apt install ./polysync_0.5.0-1_amd64.deb           # Ubuntu x86_64
sudo pacman -U ./polysync-0.5.0-1-x86_64.pkg.tar.zst   # Arch x86_64
```

Ubuntu ARM64 使用对应的 `_arm64.deb`。安装后可从应用菜单打开，或运行 `polysync -tray=false`。后台服务可选用 `systemctl --user enable --now polysync.service`，安装过程不会自动启用。Linux 更新使用新版安装包，卸载保留个人数据；完整命令、依赖与验证范围见 [Linux 安装与更新](docs/linux-installation.md)。

## 命令行参数

```text
-data-dir string   配置、同步状态和历史版本目录
-listen string     TCP 同步监听地址（默认 :45123）
-ui string         本地控制台地址（默认 127.0.0.1:45124）
-open              启动时打开浏览器（默认 true）
-tray              Windows 系统托盘（默认 true）
-version           显示版本
```

例如运行同一台电脑上的第二个测试实例：

```bash
polysync -data-dir ./device-b -listen 127.0.0.1:45223 -ui 127.0.0.1:45224 -open=false
```

## 构建发行版

Windows PowerShell：

```powershell
dotnet tool install wix --tool-path .cache/wix4 --version 4.0.6
./scripts/build.ps1
```

Linux 或 macOS：

```bash
./scripts/build.sh
```

脚本会在 `dist/` 生成 Windows amd64、Linux amd64/arm64、macOS amd64/arm64 五个无外部运行时依赖的可执行文件。PowerShell 构建还生成 Windows MSI 和包含 MSI 的 `SHA256SUMS.txt`；打包需 .NET SDK 与 WiX 4.0.6，`-SkipMSI` 可仅构建可执行文件。Linux/macOS 构建脚本不生成 MSI。MSI 和 EXE 尚未做发布者代码签名；macOS 构建也未做 Apple 开发者签名。

从 Windows 复制 Linux/macOS 原始二进制后，需要先恢复执行权限：`chmod +x polysync-*`。

在 Linux/WSL 中对上述二进制打包：`python3 scripts/package-linux.py --version 0.5.0 --format all`。需要 Python 3.11+、dpkg-deb 和 makepkg；也可单独指定 `--format deb` 或 `--format arch`。脚本校验 ELF 架构、静态链接以及构建生成的 `.build-info` 版本/哈希，输出安装包、离线 Arch 构建材料和独立的 `SHA256SUMS-linux-0.5.0-1-all.txt`。安装升级与交互验收见 [v0.5 验证记录](docs/experience-verification.md)。

## 随传与新手引导

“随传”用于临时发送文件：两台设备配对后，选择文件和在线设备即可发送；对方默认自动接收到暂存区，无需每次确认。浏览器显示“已交给后台”后可关页，接收方只需保持应用运行。

- 工作台查看收发记录、进度和失败原因；已收文件支持本机“另存为”和“打开位置”。
- 设置里可关闭接收、改变新任务的暂存目录；旧文件留在原处。
- 移除记录默认保留文件；额外勾选才删除该任务的暂存。发送缓存和收件都不自动清理。
- 第一版支持每批最多 128 个普通文件、合计 16 GiB，最多 4 个活动任务；暂不支持文件夹、离线排队、断点续传。
- 首次进入页面可开始八步教程（含添加同步文件夹、另一端接受邀请与同步状态管理），也可跳过；点击“使用引导”随时重看。
- Windows 托盘增加发送、工作台、活动任务取消、最近收件、设置和引导入口；有随传任务时退出会确认。

两端均需使用支持随传的版本。Linux 另存对话框需 zenity 或 kdialog。详细数据位置、验证范围与当前环境限制见 [实施验证](docs/transfer-implementation.md)。

## 同步语义

PolySync 为每一对设备保存上一轮成功同步后的共同清单：

- 只有一端变化：把新增、修改或删除传播到另一端。
- 两端内容相同：不传输。
- 两端都修改同一文件：安全保留两个版本，在控制台进入待解决冲突；文本文件可以查看 base/local/remote 并编辑合并结果。
- 一边删除、另一边修改：作为 Modify/Delete 冲突交给用户选择，不静默删除或恢复。
- 初次连接时同一路径已有不同内容：按冲突处理。
- 接收端在扫描后又保存了待覆盖或待删除的文件，或在待新增的位置创建了同名文件时，PolySync 保留这次保存并中止旧计划。本轮不推进共同清单。可点击「立即同步」重算，开启自动同步的空间也会在下次检查时重试。两端均升级后，任一端接收文件时都具备这项保护。

远端覆盖或删除文件前，旧内容会存入 `history/<同步空间 ID>/`。成功同步的内容按 SHA-256 去重保存到 `objects/`，用于三方文本合并。历史和对象当前不会自动清理。

如果程序在文件替换过程中被强制关闭，下一次扫描可能提示原文件的恢复目录。先将提示目录中的原文件复制到同步空间外，对照工作目录选择要保留的版本，确认已保存所有内容后再移走恢复目录并重新同步。

## 数据目录

未指定 `-data-dir` 时使用系统用户配置目录：

- Windows：`%AppData%\Polysync`
- macOS：`~/Library/Application Support/Polysync`
- Linux：`$XDG_CONFIG_HOME/Polysync` 或 `~/.config/Polysync`

其中 `config.json` 包含设备私钥、已配对设备公钥和同步设置，文件权限会尽量限制为当前用户。请勿公开分享配置目录。

## 当前安全边界与限制

- v2 使用 TLS 1.3 加密并固定已配对 Ed25519 公钥；mDNS 结果只用于发现，不能绕过验证码配对。
- 仅同步普通文件；符号链接、设备文件和 Unix socket 会被跳过。
- 单文件上限为 16 GiB。
- 不允许配置彼此嵌套或重叠的同步根目录。
- 大小写敏感文件系统中仅大小写不同的多个文件，传到默认大小写不敏感的 Windows/macOS 文件系统时可能冲突，应避免此类命名。
- 当前按配置间隔扫描完整目录，不是操作系统级实时文件监听；最低检查间隔为 5 秒。
- mDNS 通常只跨越同一二层局域网；企业 VLAN、访客 Wi-Fi 或防火墙可能阻止发现，此时可使用手动地址。

协议和模块说明见 [docs/architecture.md](docs/architecture.md)。

## 开发验证

```bash
go test ./...
go vet ./...
```

测试包含路径安全、TLS 双向同步、接收方批准的验证码配对、文件夹邀请、新建/删除状态矩阵、冲突对象和文本合并结果传播。

## Windows 后台与系统托盘

Windows 版本启动后在任务栏通知区域显示绿色 PolySync 图标（可能收纳在右下角“^”内）。关闭浏览器页面会继续同步。

- 双击图标：打开本机网页控制台。
- 右键图标：显示最近同步的最多 8 个文件夹；按成功同步时间排序，未同步文件夹列在后面。
- 展开文件夹子菜单：查看最近同步时间，选择“立即同步”或“打开文件夹”。子菜单也接受右键选择。
- 正在同步或尚未接受邀请的文件夹会禁用“立即同步”；同步结果通过系统通知反馈，详细情况可在网页同步记录中查看。
- “退出 PolySync”：停止后台服务、取消进行中的连接、移除托盘图标并释放端口。
- 近期排序会读取保存的同步基线，重启后仍可显示上次同步时间。

Windows 发行版为 GUI 程序，不额外显示命令行窗口。排错时使用 `go run ./cmd/polysync -tray=false -open=false`，在启动它的终端按 Ctrl+C 退出。Linux/macOS 本轮仍使用终端生命周期，没有新增托盘实现。

### 前端与桌面回归测试

```text
go test ./...
go vet ./...
node --check internal/webui/static/app.js
node --test tests/ui.test.cjs
```

真实浏览器检查使用本机 Chrome 与 Playwright；先准备测试专用实例：

```powershell
go run ./cmd/polysync -data-dir .cache/device-preview -listen 127.0.0.1:45323 -ui 127.0.0.1:45324 -tray=false -open=false
# 在另一个终端：
npm install --prefix .cache/ui-tools --no-save --package-lock=false playwright
$env:NODE_PATH = "$PWD/.cache/ui-tools/node_modules"
node tests/ui.browser.cjs
```

截图保存在 `.cache/ui-review/`。浏览器测试中的设备和文件夹是测试夹具，不会创建真实同步配置。Windows Go 测试还会创建短暂托盘图标，验证双击和退出，需要可访问的桌面会话。

项目协作规范见 [AGENTS.md](AGENTS.md)。

项目目前保留所有权利，未采用开源许可证，见 [LICENSE](LICENSE)。第三方组件继续适用各自许可证，Go 核心及构建工具声明见 [THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt)。AUR 注册暂不可用，当前通过网页下载 Arch 包后使用 pacman 安装；尚不能通过 `yay -S polysync-bin` 安装。
## 产品方案与实施记录

- [跨平台技术设计与实现核对报告：后台、内存、UI 与待决策项](docs/cross-platform-design-review-2026-09-22.md)

- [新手引导与随传工作台方案](docs/onboarding-and-transfer-plan.md)
- [托盘快捷入口：现有操作、UI 规格与调整表](docs/tray-interaction-spec.md)

- [随传与新手引导：实施和验证](docs/transfer-implementation.md)
- [安装与应用内更新：构建、验证和限制](docs/installation-and-updates.md)
- [轻量常驻与事件触发同步：后续思路](docs/background-roadmap.md)
- [手机端与商店发布规划：Android / iOS、国内 / 海外](docs/mobile-platform-and-release-plan-2026-09-21.md)
