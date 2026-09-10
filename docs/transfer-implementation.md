# 随传与新手引导：实施和验证

日期：2026-09-08。工作区版本：0.3.0-dev，尚未提交或发布。

## 使用

1. 两台设备启动支持随传的 PolySync，按原流程允许请求并输入六位验证码完成配对。
2. 打开“随传”，选择或拖入普通文件，选择在线设备，点击“发送文件”。
3. 浏览器上传期间保留页面；显示“已交给后台”后可关页，后台完成后在收发记录查看结果。
4. 接收端无需打开网页或逐次确认。收到的完整文件可在工作台“另存为”或“打开位置”。
5. “随传设置”可关闭接收或改变新任务暂存目录；已有文件留在原处。
6. “移除记录”默认保留文件；主动勾选才清理本任务暂存文件。另存副本保留。
7. “使用引导”可重看教程。Windows 托盘有发送、工作台、正在传输、最近收到、设置和使用引导入口。

## 数据和协议

- 复用已有 TLS 1.3 及已配对 Ed25519 公钥身份校验，新增 transfer 操作。两端均需支持本版功能。
- 验证配对与接收开关后接收文件清单；普通文件逐个流式传输、计算 SHA-256、写临时文件并确认。最终任务记录落盘后回传完成确认。
- 接收目录默认是数据目录/inbox；每次传输独立 polysync-任务ID 子目录，文件使用编号前缀避免同名覆盖。
- 发送缓存：数据目录/transfers/outgoing。任务记录：数据目录/transfers/records。接收设置：数据目录/transfer-settings.json。
- 暂存不会自动清理；发送完成也保留缓存供重新发送。修改源文件不会传播到已收到的文件。
- 1–128 个文件/任务，总量 ≤16 GiB，本机最多 4 个活动任务。启动新任务前检查可用空间，并保守预留其他活动任务未写入的字节；真实写入错误继续上报。
- 上传/传输期限 2 小时，连接超时 8 秒。准备任务未上传，2 分钟后取消。重发是新任务，不是续传。
- 关闭浏览器在上传结束后不取消后台发送；关闭应用取消活动工作。异常重启把未结束记录标为中断，并清理未完成文件。
- API 保持原有同源写入限制。文件名拒绝路径、控制字符、系统保留名称；文件操作通过 os.Root 限制在任务目录。

## 已执行验证

| 检查 | 结果 / 覆盖 |
| --- | --- |
| go test ./... | 通过，包括原同步/配对、端口释放、新增随传、托盘测试 |
| go vet ./... | 通过 |
| gofmt | 已对 Go 修改执行 |
| node --test tests/ui.test.cjs | 8 项通过：轮询、转义、弹窗、选择限制、表单隔离等 |
| node --check app.js / transfer.js / tour.js | 通过 |
| node tests/transfer.ui.browser.cjs | 通过；包含原 ui.browser.cjs 的同步界面回归 |
| node tests/transfer.browser.cjs | 本轮复测通过：两个独立程序、真实配对、浏览器上传、TLS 自动接收及落盘验证 |
| scripts/build.ps1 -Version 0.3.0-dev | Windows amd64、Linux amd64/arm64、macOS amd64/arm64 构建通过 |

Go 随传测试使用真实回环 TLS 连接和临时目录，覆盖自动接收、多文件/空文件、关接收后失败、重发、未配对拦截、恶意文件名、坏校验、重启记录恢复、目录切换、双向目录重叠检查、另存覆盖/保留原件、退出取消另存、文件丢失与记录移除。真实 HTTP 测试覆盖客户端停止发送时仍能取消上传；另有取消上传不能重新变成后台发送的竞态回归。

Windows Go 测试创建短暂真实托盘图标，验证双击与退出、同步菜单分发、随传路由/另存分发/取消分发、文件丢失时禁用，以及不同 DPI 下的菜单字体和尺寸。启动/停止测试验证 HTTP 和 TCP 端口释放。

Chrome 浏览器检查使用受控 API 夹具，验证桌面 1365px、窄屏 390px、空/有内容状态、教程所有步骤/上一步/跳过/重看、气泡边界、焦点循环、弹窗与教程互不抢占、设置保存、multipart 上传、同一文件重选、另存 API 分发、移除默认保留文件、轮询保留焦点、减少动态效果与服务离线。截图已在 .cache/ui-review 人工查看，不加入 Git。

## 本轮复测与未验证范围

用户要求再次测试后，重新执行了不使用测试缓存的 `go test ./... -count=1 -timeout=120s`、Go 静态检查、前端 8 项单元测试、三个脚本语法检查、两个 Chrome 浏览器测试套件及五个平台构建，全部通过，本轮未出现测试断言失败。

此前 Windows 应用程序控制阻止独立程序启动，本轮重新构建后正常启动，未修改或关闭系统安全策略。完整 `tests/transfer.browser.cjs` 已通过：使用两套隔离数据目录和回环端口，完成真实验证码配对、浏览器多文件上传、TLS 自动接收、普通文件内容和空文件大小核对、上传完成后关闭发送页面、接收端无需网页、设置持久化，以及移除记录保留暂存文件。该套件结束时终止自己创建的测试子进程。

注意：浏览器套件的“另存为”使用受控响应验证 API 分发；实际复制、校验和覆盖语义由 Go 测试验证，并不等同于原生对话框人工验收。
原生另存对话框的手工选择/覆盖/取消、系统通知点击、传输中退出确认、完整主菜单的多显示器视觉/屏幕阅读器检查仍需在允许启动的桌面环境验收。Linux/macOS 已跨平台构建，未在目标系统实机运行；Linux 另存需 zenity 或 kdialog，Windows 使用系统 PowerShell/WinForms，macOS 使用系统保存对话框。本轮没有新增 Linux/macOS 托盘。

## 复现命令

```powershell
$env:GOCACHE = "$PWD/.cache/go-build"
$env:GOMODCACHE = "$PWD/.cache/go-mod"
.cache/toolchain/go/bin/go.exe test ./...
.cache/toolchain/go/bin/go.exe vet ./...
node --test tests/ui.test.cjs
node --check internal/webui/static/app.js
node --check internal/webui/static/transfer.js
node --check internal/webui/static/tour.js
$env:NODE_PATH = "$PWD/.cache/ui-tools/node_modules"
node tests/transfer.ui.browser.cjs

# 完整独立程序联调（本轮已通过）
.cache/toolchain/go/bin/go.exe build -o .cache/polysync-transfer-test.exe ./cmd/polysync
node tests/transfer.browser.cjs
```

两个浏览器套件均使用隔离测试数据；完整套件在 .cache 下新建两套临时实例，结束时仅终止自己启动的子进程。没有使用用户真实文件夹。
