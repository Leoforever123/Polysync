# PolySync 开发协作规范

适用于本仓库中的代码、测试和文档。结构参考 https://agents.md/ ，按本项目实际情况维护。

## 项目与目录
- Go 1.26+；浏览器 UI 使用原生 HTML/CSS/JavaScript，通过 go:embed 打包。
- cmd/polysync：启动、退出与桌面集成；internal/tray：Windows 系统托盘。
- internal/service：配对、同步和 HTTP API；internal/store：配置与历史；internal/syncer：扫描与计划。
- internal/webui/static：前端；scripts：构建；docs：架构和验证记录。
- 保持 Windows、Linux、macOS 核心可构建。系统专属实现使用 build tags。

## 工作方式
- 修改前先阅读相关代码、现有测试与 git diff，保留用户尚未提交的修改。
- 一次解决一个明确问题，小步调整；不顺带迁移框架或大幅重构。
- 新行为更新 README 或相应文档。不要将设计稿、缓存、测试设备身份、日志或二进制加入 Git。
- 默认不提交、不推送、不发布，除非用户要求。

## 每次修改后的测试（必须执行）
- 每次修改代码都必须编写或更新与行为对应的测试，并实际运行；修复缺陷时优先添加能复现问题的回归测试。
- 不允许只写测试而不运行，也不允许通过删除或跳过失败断言来获得通过。
- 后端：运行 go test ./... 和 go vet ./...，修改过的 Go 文件执行 gofmt。
- 前端：运行 node --test tests/ui.test.cjs，并执行 node --check internal/webui/static/app.js。
- UI 改动需在浏览器检查桌面、窄屏、空状态、弹窗、键盘操作和减少动态效果设置；保留可复现步骤。
- 托盘或生命周期改动需检查双击、菜单动作、退出、端口释放及重新启动；平台代码还需跨平台构建。
- 以临时目录和回环地址测试同步，禁止使用用户真实文件夹作测试数据。
- 测试失败应定位并修复后重新运行；确实受环境阻碍时明确报告命令、错误和未验证范围，不能声称通过。
- 交付说明应写明：改了什么、为什么、运行了哪些测试、尚存限制。

## 常用命令
```text
go run ./cmd/polysync
go test ./...
go vet ./...
node --test tests/ui.test.cjs
node --check internal/webui/static/app.js
go build -o dist/polysync.exe ./cmd/polysync
```
Windows 本地工具链如未加入 PATH，可使用 .cache/toolchain/go/bin/go.exe。
缓存可设置为项目 .cache/go-build 和 .cache/go-mod。发行版使用 scripts/build.ps1 或 scripts/build.sh。

## 代码与设计约定
- Go 错误必须处理；后台工作要能取消，退出应关闭监听器、连接和图标资源。
- 托盘复用同步服务，不复制协议逻辑。菜单动作必须提供结果或失败反馈。
- 保持现有绿色品牌、简洁信息层级和自然中文。优先可读的系统无衬线字体，避免外部字体依赖。
- 动画用于状态反馈和空间过渡，不能阻塞操作；支持 prefers-reduced-motion。
- 所有可操作元素要支持键盘、清晰焦点与可访问名称。轮询不得无故重建当前聚焦的控件。
- API 输出及进入 HTML 的用户内容必须转义。保持 TLS、公钥固定、验证码确认和路径安全检查。
- 文件同步的覆盖、删除、冲突、校验和历史归档语义不能因 UI 调整而改变。
- 优先自动判断设备、路径建议和可用动作。仅在信息不足或用户需决定权限、数据去留时要求选择。相同操作跨端使用同一名称。错误反馈保留当前输入并提供重试，危险操作说明影响范围。
- 验收按用户经历的状态变化走查。按下、等待、成功、失败、关闭和撤回均应可理解；频繁操作的反馈轻且可打断。测量长列表的实际帧间隔，检查手机、宽窄屏、深浅色和系统减少动态效果。
