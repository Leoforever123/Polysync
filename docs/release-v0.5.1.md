## 已修复

- 同步文件夹的冲突处理回执会核对所属文件夹与冲突事件。多台设备仍能转交已经确认的处理结果。
- 旧版设备发出文件夹接受确认时，邀请方会更新该成员的连接地址。设备在配对后更换局域网地址，后续同步会使用新的地址。
- 组同步遇到损坏的本地版本记录时会停止这一轮，保留文件并返回错误。其他文件夹可以继续同步。

## 下载与升级

桌面端继续使用原安装渠道，等当前任务结束后退出旧版本，再安装新版。Windows 提供 MSI 和便携 EXE，macOS 提供 Apple 芯片和 Intel 版本，Linux 提供 Ubuntu、Arch 安装包和通用程序。Windows 和 macOS 程序尚无发布者签名，可使用同页 SHA-256 清单核验下载文件。

Android ARM64 预览版使用 dev.5 的应用包名和签名。已有预览版可直接更新，保留设备配对和工作区。dev.4 使用另一个包名，两个应用可以并装；需要的文件可从旧应用导出。在卸载任何一个应用之前，先导出该应用工作区内要保留的文件。

有关多台设备如何共同编辑、跟随和授权回传，以及桌面和 Android 的动效验收，见[多设备文件夹](https://github.com/Leoforever123/Polysync/blob/v0.5.1/docs/folder-groups.md)与[体验验证记录](https://github.com/Leoforever123/Polysync/blob/v0.5.1/docs/experience-verification.md)。
