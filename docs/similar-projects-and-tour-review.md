# 类似项目调研与引导验证

调研日期：2026-09-08。通过公开网络检索，优先查阅官方仓库及文档；不是穷尽列表，也未对这些项目做本地性能实测。

## 项目对照

| 项目 | 主要用途 | 与 PolySync 的关系、可借鉴点 |
| --- | --- | --- |
| [Syncthing](https://github.com/syncthing/syncthing) | 开源持续文件同步 | 最直接的同步对标。设备与文件夹分别配置；引导必须覆盖双方添加设备、共享文件夹和接收端路径。[入门流程](https://docs.syncthing.net/intro/getting-started) |
| [Resilio Sync](https://help.resilio.com/hc/en-us/articles/204754939-Comprehensive-guide-to-syncing-Desktop-Desktop) | 自有设备或与他人共享文件夹同步 | 重点参考分享和接收的完整流程；[选择性同步](https://help.resilio.com/hc/en-us/articles/204754389-Sync-functionality-in-detail)值得作为后续需求评估。 |
| [LocalSend](https://github.com/localsend/localsend) | 无需互联网的跨平台局域网文件、消息传输 | 最直接的随传对标；参考发现设备、选择文件、发送和接收反馈。官方说明使用 HTTPS，不需要第三方服务器。 |
| [PairDrop](https://github.com/schlagmichdoch/PairDrop) | 浏览器跨平台临时传文件 | 参考轻量入口；浏览器传输和长期后台文件夹同步的产品目标不同。 |
| [croc](https://github.com/schollz/croc) | 简洁的跨设备安全发送工具 | 参考短码发送体验和命令行自动化；需要单独评估其中继及网络模型，不能直接当作纯局域网方案。 |
| [Warpinator](https://github.com/linuxmint/warpinator) | 局域网文件分享 | 参考附近设备与收发操作，属于随传方向。 |
| [FreeFileSync](https://freefilesync.org/) | 文件夹比较、同步和备份 | 参考同步前的差异呈现与结果可读性；与自动配对设备的产品流程不同。 |
| [Unison](https://github.com/bcpierce00/unison) | 文件同步工具 | 同步方向的技术参考，适合进一步研究冲突与两端差异处理。 |
| [copyparty](https://github.com/9001/copyparty) | 支持可恢复上传及多种访问协议的便携文件服务器 | 参考浏览器上传与收件体验；服务器架构与设备间同步不同。 |

## 对 PolySync 的判断

以下是基于上述资料和当前项目界面的产品判断，不是这些项目官方评价。

最值得优先深入对照的是 Syncthing 与 LocalSend：分别覆盖持续同步与临时随传。PolySync 可将优势放在自然中文、局域网设备配对、清楚的双端文件夹配置和统一的托盘入口。仅把两类功能放进同一页面还不足以形成明显差异；完成首次同步的难度、错误是否可理解以及状态是否可信更关键。

优先改进路径：先让用户看懂“本机选文件夹 → 发送邀请 → 对方选对应文件夹 → 接受并首次同步”；后续再评估差异预览、冲突解释、断线恢复反馈。此次没有修改同步协议、覆盖、删除或归档语义。

## 本次调整与复现

- 遮罩不透明度由约 88% 降至 38%，让背景布局可辨认。
- 引导由五步扩展为八步，新增添加同步、另一端接受邀请、同步卡片管理。
- 无托盘实例的退出提示按步骤目标判断，不再依赖旧的固定第五步位置。
- 保留已有引导完成记录，可点击“使用引导”重看。
- 浏览器验证命令（PowerShell，项目目录）：
  `$env:NODE_PATH = "$PWD/.cache/ui-tools/node_modules"; node tests/transfer.ui.browser.cjs`
- 受控回环 HTTP fixtures 覆盖 1365×1050 和 390×844、八步导航与文案、空状态、有内容状态、弹窗、Escape、Tab 焦点、重看、无托盘提示和减少动态效果；不使用真实文件夹。
- 已运行并通过：`node --test tests/ui.test.cjs`、`node --check internal/webui/static/app.js`、`node --check internal/webui/static/tour.js`、上述浏览器套件、`go test ./...`、`go vet ./...`。
- 已查看桌面同步步骤及窄屏截图；截图保存在被 Git 忽略的 `.cache/ui-review/`。
- 编译结果：`dist/polysync-updated.exe`。本次未操作用户正在运行的实例，未重新测试实际双机同步及托盘生命周期。
