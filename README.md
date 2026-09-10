# PolySync

PolySync 是一个面向个人设备的局域网双向文件同步工具。两台设备各自选择一个本地文件夹，通过 TCP 直接交换文件；它支持多个独立同步空间、自动定时同步和手动同步，不依赖云存储或中心服务器。

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

设备只需配对一次，之后可以创建任意多个互不重叠的同步文件夹。

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
./scripts/build.ps1
```

Linux 或 macOS：

```bash
./scripts/build.sh
```

脚本会在 `dist/` 生成 Windows amd64、Linux amd64/arm64、macOS amd64/arm64 五个无外部运行时依赖的可执行文件。macOS 构建产物未进行 Apple 开发者签名；首次运行可能需要在“隐私与安全性”中确认。

从 Windows 复制 Linux/macOS 原始二进制后，需要先恢复执行权限：`chmod +x polysync-*`。

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
- 同步期间文件继续变化：本轮不更新共同清单，下一轮重新计算，避免记录错误基线。

远端覆盖或删除文件前，旧内容会存入 `history/<同步空间 ID>/`。成功同步的内容按 SHA-256 去重保存到 `objects/`，用于三方文本合并。历史和对象当前不会自动清理。

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
## 产品方案与实施记录

- [新手引导与随传工作台方案](docs/onboarding-and-transfer-plan.md)
- [托盘快捷入口：现有操作、UI 规格与调整表](docs/tray-interaction-spec.md)

- [随传与新手引导：实施和验证](docs/transfer-implementation.md)
