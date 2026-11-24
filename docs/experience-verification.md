# v0.5 使用体验验收

以下用例使用临时目录、独立设备身份和回环连接。下载页的浏览器回归使用本地静态服务器与可控制的发布数据，以便重现离线和缺少平台安装包的情形。

| 状态变化 | 实际检查 |
| --- | --- |
| 第一次添加文件夹 → 配对 → 继续邀请 | 三个独立桌面进程。键盘输入验证码，完成后继续原邀请。已有本地目录只选一次。 |
| 仅接收 → 开放回传 → 断开连接 | 跟随设备新建文件，在确认回传前核对预览。预览后再次修改会要求重新确认。断开单条连接后检查另外两端的字节和本地文件。 |
| 长列表刷新、滚动、切换工作区 | Chromium 深色模式下滚动 4,000 条随传记录。默认渲染 24 条，定位最老的一条记录只多渲染一个目标。150 帧采样期间触发两次刷新，检查焦点、进度更新与帧间隔。多轮 P95 为 16.7–16.8 ms，后台同时工作时观测到超过 33.34 ms 的帧。 |
| 关闭弹窗、退出引导、切回任务 | 宽屏和窄屏、键盘 Tab / Escape、浅色和深色及系统减少动态效果。取消操作后返回原控件，输入与滚动位置保留。 |
| 更换平台、键盘下载、网络失败 | 为 Windows、Linux 和 macOS 提供对应选项，区分 Android 与 iPadOS。使用键盘实际触发一次浏览器下载，检查发布 API 离线时的版本回退。 |
| 旧客户端升级与重新打开 | 校验公开 v0.4.0 Linux 二进制及安装包。新老两端互传，重启后再次同步。用 dpkg 和 pacman 在隔离根目录中升级，验证身份、文件夹配置、历史、端口释放与卸载后保留文件。 |

## 复跑浏览器回归

安装 Playwright 和对应 Chromium 后，从仓库根目录运行。指定其他 Chromium 渠道时调整环境变量。以下示例把测试程序放入项目的忽略目录。

```sh
mkdir -p .cache
go build -o .cache/ui-test-polysync ./cmd/polysync
export POLYSYNC_TEST_BINARY="$PWD/.cache/ui-test-polysync"
export POLYSYNC_TEST_BROWSER_CHANNEL=chromium
review_dir=$(mktemp -d)
"$POLYSYNC_TEST_BINARY" -data-dir "$review_dir/state" -listen 127.0.0.1:0 -ui 127.0.0.1:0 -tray=false -open=false >"$review_dir/startup.log" 2>&1 &
review_pid=$!
trap 'kill "$review_pid" 2>/dev/null || true' EXIT
for attempt in $(seq 1 100); do
  POLYSYNC_TEST_URL=$(sed -n 's/.*控制台 \(http:\/\/127\.0\.0\.1:[0-9]*\).*/\1/p' "$review_dir/startup.log")
  test -n "$POLYSYNC_TEST_URL" && break
  sleep 0.1
done
export POLYSYNC_TEST_URL
test -n "$POLYSYNC_TEST_URL" || exit 1
node --test tests/ui.test.cjs tests/site.test.cjs
node tests/ui.browser.cjs
node tests/ui-group.browser.cjs
node tests/unpair.ui.browser.cjs
node tests/transfer.browser.cjs
node tests/transfer.ui.browser.cjs
node tests/site.browser.cjs
```

Android 使用 Android 15 模拟器验证。桌面与模拟器之间完成配对、随传、同步与文件导出，另检查了手机窄屏、横屏、深浅色与减少动态效果。原生回归与构建步骤见 [Android 预览版](android-debug.md)。

QA 中实际在两个浏览器标签分别操作了两个新进程。从空状态点“添加同步文件夹”，在另一端同意请求，输入六位码，发送共同编辑邀请，在第二台选择目录后确认界面显示同步完成，并在磁盘核对两份文件。这个过程发现并修复了绑定回环地址时首页误显示局域网地址，以及已有配对后还出现首次引导的问题。

Android 硬件加速模拟器在 1,000 条历史中滚动，采样 78 帧，帧间隔 P95 为 16.67 ms，渲染耗时 P95 为 17.09 ms。另一次与编译并行的采样中，68 帧里有 10 帧间隔超过 25 ms。帧率与设备、刷新率及后台负载有关。ARM64 APK 在官方 Android 15 x86 主机转译器上能与旧版并装，但运行时读取 ARM 系统寄存器会退出；新旧版均观察到这一限制。本轮应用行为验证使用原生 x86_64 测试构建。

## 补丁升级与异常握手

从公开下载的 v0.5.0 客户端加入新版的三实例回环测试。浏览器里让两台协作设备分别修改和删除同一个文件，在修改的一端保留文件，另一端随后关闭待处理项，两边的文件字节相同。通过 `POLYSYNC_TEST_COLLABORATOR_BINARY` 指定旧客户端可复跑这条路径。Ubuntu 和 Arch 另在隔离根目录用包管理器覆盖安装，检查重开、端口释放、配对和工作文件保留。

Windows 原生测试发现，本地版本日志损坏时，过早发送就绪消息可能使对端只看到连接中断。v0.5.2 把校验提前。双端回归直接读取认证 TLS 连接的首帧，在已保存和待提交的记录中分别写入损坏的摘要，验证对端先收到具体原因。恢复日志后再次修改并同步，两端文件内容一致。
