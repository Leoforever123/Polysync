## 下载与安装

从 [官网下载页](https://leoforever123.github.io/Polysync/#downloads) 点击自己的平台即可下载，也可直接使用本页 Assets。

- Windows x64：推荐 `polysync-windows-amd64.msi`，双击安装；保留免安装 EXE。
- Ubuntu：选择 amd64 或 arm64 的 `.deb`，双击安装，或使用 `sudo apt install ./下载的文件.deb`。
- Arch Linux x86_64：下载 `polysync-0.4.0-1-x86_64.pkg.tar.zst`，在下载目录运行下面的命令，再从应用菜单打开。无需 AUR 账号。
- macOS：选择 Apple Silicon / Intel 原始程序，恢复执行权限后启动；尚未做 Apple 开发者签名。
- Android：附带 `0.5.0-dev.4` ARM64 调试 APK（Android 10+），属于实验版，不是 Android 正式发行版。

```sh
sudo pacman -U ./polysync-0.4.0-1-x86_64.pkg.tar.zst
```

升级前等待任务结束并退出旧程序，重新下载新版安装包安装即可。使用用户服务的 Linux 用户先执行 `systemctl --user stop polysync.service`，安装后执行 `systemctl --user daemon-reload` 和 `systemctl --user start polysync.service`。配置、配对和用户文件保留；自定义 `-data-dir` 需继续使用原目录。

Arch 尚未上架 AUR，也没有自建软件源；`yay -S polysync-bin` 和常规系统升级暂不能发现本项目的新版本。另附离线 PKGBUILD 构建包。

## 本版变化

- 电脑端与 Android 支持取消配对。撤销本机信任、停用相关同步，保留本地文件、历史与工作区。对方需要单独取消配对。
- Windows 新增按用户安装的 MSI，以及控制台、托盘中的检查更新、下载安装和重启流程。从 v0.3.0 迁移需手动安装一次；v0.4.0 起可检查后续正式版本。
- 增加 Ubuntu / Arch 安装包、应用菜单与可选用户级 systemd 服务；安装不会自动启用后台服务。
- 修复大文件冲突恢复的内存使用、入站连接和配对请求的资源限制、部分发现结果更新及错误状态反馈。
- Android 实验版增加原生设备发现、工作区同步和取消配对，修复主动停止后界面重建又启动服务的问题。

## 校验与验证范围

`SHA256SUMS.txt` 覆盖本次发布的程序、安装包、APK及附带声明；Linux 安装包另有专用校验清单。项目保留所有权利，第三方组件适用各自许可证。

Go 全量测试与 vet、前端测试与浏览器桌面/窄屏/键盘/减少动画检查，以及隔离环境中的 MSI 和 dpkg/pacman 安装、升级、重启、卸载与数据保留均有验证。构建覆盖 Windows x64、Linux amd64/arm64、macOS Intel/Apple Silicon。

Windows MSI/EXE 尚未做发布者签名。Linux 验证环境为 Ubuntu WSL；完整 Arch 桌面、systemd 登录会话和 ARM64 运行未实机验收。Android dev.4 通过构建、单元测试和 lint，新增取消配对尚未做手机真机回归。macOS 尚未实机验收。详细记录见仓库 docs。
