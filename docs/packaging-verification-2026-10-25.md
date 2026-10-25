# 安装包与升级验证 · 2026-10-25

GitHub 发布构建：桌面二进制从干净源码提交 `438541a6d22a18b3b8e64eb7e5b96d7e50aac107` 构建，版本 0.4.0，后续提交仅更新发布记录和 AUR 校验元数据。重新通过 `go test -count=1 ./...`、vet、15 项前端测试、官网及取消配对浏览器回归；官网验证了真实下载事件、键盘、桌面/窄屏、减少动画和 API 失败备用链接。最终 MSI 回归日志为 `.cache/msi-test-cwqQDE`，包含随包声明文件校验；重新构建的 Linux 包通过 dpkg/pacman 生命周期与校验清单测试。Android dev.4 从同一源码重新构建，Kotlin 单元测试和 lint 通过，未连接手机做新增真机回归。官网以网页下载安装包为主入口。AUR 账号仍不可用，本次不上传 AUR。以下早期“未提交/未发布”描述保留为历史过程。

后续 AUR 准备更新：所有者选择保留所有权利，已加入根目录 `LICENSE`，AUR 和离线 Arch 包改为 `LicenseRef-Proprietary`，声明安装至对应的 `/usr/share/licenses/<包名>/LICENSE`。重新生成 Linux 包与校验清单，并重跑 AUR 构建/版权文件一致性/篡改拒绝测试、3 项 Linux 打包测试以及 dpkg/pacman 安装升级卸载测试，全部通过。下文“尚未指定许可证”描述的是此次更新前的状态。尚无 AUR 账号及公开 v0.4.0 下载，因此没有推送 AUR，也未通过在线下载/yay 安装验收。

## 本轮交付

Windows 默认提供 MSI，保留单文件 EXE。增加 Ubuntu amd64/arm64 `.deb`、Arch x86_64 `.pkg.tar.zst` 和离线 `PKGBUILD` 构建包，应用版本 0.4.0、Linux 打包修订号 1。0.4.1 仅用于升级夹具，不是正式发行版。

Linux 包拥有 `/usr/bin/polysync`、菜单启动器、桌面菜单、SVG 图标、用户级 systemd unit 和说明文档。没有修改用户目录的安装/卸载脚本，也不会自动启用服务。菜单启动器会等待本机控制台就绪再开浏览器；重复打开交给 systemd 已有服务处理。无用户 systemd 会话时可从终端直接启动程序。

构建脚本显式禁用 CGO，为 Linux 二进制写入版本与 SHA-256 的 `.build-info`。打包前验证 ELF 架构、无动态解释器和版本/哈希匹配；Windows checkout 的启动脚本会转换为 LF。Arch 通过 makepkg 生成包元数据、文件清单和离线 `.SRCINFO`。

## 验证结果

- 核心 `go test ./...`、`go vet ./...` 通过；`node --test tests/ui.test.cjs` 11 项与 `node --check internal/webui/static/app.js` 通过。本轮没有修改浏览器 UI。

- Windows `scripts/build.ps1 -Version 0.4.0` 成功：五目标二进制、MSI、`SHA256SUMS.txt` 和 Linux 构建记录。
- WSL 中运行 `scripts/build.sh 0.4.0` 的 LF 副本，五目标构建及校验清单成功；Windows 和 Unix 构建脚本均已实际执行。
- `python3 -m unittest discover -s tests -p 'test_linux_packaging.py' -v` 通过：拒绝错误架构、动态解释器、错误版本或被改动的二进制；保证 shell 脚本 LF 和执行权限；菜单重复启动、就绪等待和失败不打开浏览器。
- `python3 tests/linux-packaging.integration.py --old-dir dist --new-dir .cache/linux-upgrade-fixture` 通过：真实 dpkg 与 pacman 在用户命名空间、一次性根目录和独立数据库中安装 0.4.0，运行后升级 0.4.1，再运行、停止并卸载。验证程序版本、程序哈希变化、HTTP 就绪、SIGTERM 退出、端口重新绑定和重启，逐项比较设备身份、密钥、配对和同步配置，确认卸载后用户文件和历史仍在。
- Linux 包中的 `.desktop` 通过 desktop-file-validate；用户 unit 通过 systemd-analyze --user verify；不包含系统级服务或已启用的登录启动链接。
- 最终产物的两份 SHA-256 清单全部通过。检查两个 DEB 的架构、文件范围和 root 所有权，确认 DEB/Arch 中的程序与最终原始二进制一致；离线 Arch 构建包通过 makepkg 源码校验，篡改载荷后校验如预期失败。
- `node tests/msi.integration.cjs` 两次通过，最终一轮使用 PowerShell 构建的二进制。覆盖独立测试产品按用户安装、自定义安装目录保留、真实 helper 等待退出/升级/启动新版、设备身份和配置保留、拒绝降级、卸载保留数据。测试注册和进程已清理。最终日志：`.cache/msi-test-qC1vyc/`。

## 环境与范围

Linux 环境为 Ubuntu 26.04.1 LTS / WSL x86_64。系统没有运行中的 systemd 用户管理器；unit 已做离线检查，实际已安装程序的启停/端口释放已验证，但菜单到桌面浏览器、登录自动启动和注销驻留未做真实桌面会话验收。Arch 包使用 Ubuntu 软件源的 makepkg/pacman 7.1 工具在隔离数据库中验证，未把该结果称为完整 Arch 桌面实机验收。Ubuntu ARM64 包完成架构/内容检查，未在 ARM64 硬件执行。

打包工具以 `apt-get download` 下载后解压至 `.cache/linux-packaging-tools`；没有把测试包装进 WSL 主系统。makepkg 使用独立配置和 pacman 数据库，版本号、包校验和安装断言没有关闭。Linux 包当前未附发行者签名；本地包安装验证不代表已建立受信任的软件源。

测试最初遇到环境代理转发回环 HTTP、TIME_WAIT 被无 SO_REUSEADDR 的测试监听器误报，以及手写配置省略字段导致 JSON 比较差异。分别明确禁用测试 HTTP 代理、按 Go 监听器行为检查端口、让旧版先持久化正常配置后比较；保留全部验收断言并重跑通过。

## 产物和发布边界

`dist/` 中的 MSI、EXE、Linux/macOS 原始二进制仍由 `SHA256SUMS.txt` 校验。Linux 安装包和离线 Arch 构建包由 `SHA256SUMS-linux-0.4.0-1-all.txt` 校验。两份清单均需与对应构建产物一起分发。若随后签名，必须在签名完成后重新生成相应校验值。

本轮没有提交、推送、上传 Release、建立 APT 源或提交 AUR。线上旧版不会因此自动获得新功能；`apt install polysync`、`pacman -S polysync` 或 AUR 工具按包名安装尚不可用。仓库未提供 LICENSE，当前元数据只如实标注未指定许可证，不替项目所有者选择分发授权。正式对外发布前需明确这一点并处理 Windows 发布者签名。

安装/升级/卸载命令见 [Linux 安装与更新](linux-installation.md)。手动生成升级夹具时，使用同一源码但分别嵌入 `main.version=0.4.0`、`0.4.1`，每次打包使用对应 `.build-info`，将 0.4.1 放入独立缓存目录。测试只使用临时目录和回环地址，不使用用户真实同步数据。
