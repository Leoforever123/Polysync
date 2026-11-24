# Linux 安装与更新

提供 Ubuntu amd64/arm64 的 `.deb`、Arch x86_64 的 `.pkg.tar.zst` 和离线 `PKGBUILD` 构建包。主入口是 [官网下载页](https://leoforever123.github.io/Polysync/#downloads) 或 [GitHub Release](https://github.com/Leoforever123/Polysync/releases/latest)，点击对应平台即可下载。尚未发布到 APT 软件源、AUR 或发行版官方仓库。下面以 0.5.2、打包修订号 1 为例。

## Ubuntu

下载与你的架构匹配的包及同批 `SHA256SUMS-linux-0.5.2-1-all.txt`，核对对应文件的 SHA-256，然后安装：

```sh
sudo apt install ./polysync_0.5.2-1_amd64.deb
# ARM64 使用 polysync_0.5.2-1_arm64.deb
```

程序本身为静态 Go 可执行文件。菜单启动需要 systemd 用户会话、`xdg-utils` 和用于等待本地控制台就绪的 `curl`，包以 Recommends 声明；Arch 中列为可选依赖。原生文件选择器可选安装 `zenity` 或 `kdialog`。无图形界面也能直接运行后台核心。

## Arch

在网页点击 **Arch Linux** 下载 `.pkg.tar.zst`，不需要 AUR 账号或 yay。在下载目录打开终端，核对同批校验值后安装：

```sh
sudo pacman -U ./polysync-0.5.2-1-x86_64.pkg.tar.zst
```

也可将 `polysync-0.5.2-1-arch-build.tar.gz` 解压到一个新目录，检查 `PKGBUILD` 后以普通用户构建安装：

```sh
makepkg -si
```

构建包包含带 SHA-256 的本地二进制载荷，可离线构建。它不是已经发布的 AUR 包；`yay -S polysync-bin` 或 `pacman -S polysync` 尚不可用。Arch ARM 不在本轮 Arch 包支持范围。通过网页下载的包由 pacman 管理，卸载可用 `sudo pacman -R polysync`，更新时再次从网页下载新版并安装。

### AUR 发布准备

后续 AUR 包名预留为 `polysync-bin`，上架后才可使用 `yay -S polysync-bin` 和 `yay -Syu`。2026-10-25 查询时包名尚未注册，所有者无法注册账号，因此本轮先使用 GitHub Release 分发。

候选配方在 `packaging/aur/polysync-bin/`，包含 `PKGBUILD`、makepkg 生成的 `.SRCINFO`、菜单启动器、用户服务和图标。仅支持 x86_64，提供 `polysync` 并声明与现有同名包冲突。程序从固定版本 GitHub Release 下载，以 SHA-256 校验；辅助文件直接随 AUR 配方提交。不会把二进制或本地缓存提交到 AUR。

配方固定为 0.5.2 二进制。项目所有者选择保留所有权利，包标注为 `LicenseRef-Proprietary`，声明随包安装至 `/usr/share/licenses/polysync-bin/LICENSE`，不表示项目采用了开源许可证。AUR 推送仍需维护账户绑定 SSH 公钥。更换发布二进制后必须更新校验值并重测，不能使用 SKIP。

本地构建验收（需要上一节所述的 makepkg 工具环境）：

```sh
python3 tests/aur-packaging.integration.py
```

该测试在临时目录使用本地的固定哈希二进制，验证实际构建、元数据、版本、桌面入口、AUR 源码包不含二进制，以及载荷被篡改时构建校验失败。已在 Ubuntu WSL 的 makepkg 7.1 环境通过，不代表已在 AUR 上线或已做完整 Arch 桌面验收。

更新配方时重新生成 `.SRCINFO`，从公开 Release 下载二进制并构建验收：

```sh
(cd packaging/aur/polysync-bin && makepkg --printsrcinfo > .SRCINFO)
python3 tests/aur-packaging.integration.py --download
```

v0.5.2 已在 Arch Linux 从空缓存下载公开二进制，完成构建、桌面入口验证与损坏文件拒绝测试。正式发布到 AUR 时，再将上述候选目录的文件提交至 `ssh://aur@aur.archlinux.org/polysync-bin.git`，并再次查询 AUR、在干净 Arch 环境通过 yay 验证安装。目前所有者没有 AUR 账号，测试没有上传或创建包。

账号入口为 https://aur.archlinux.org/register 。2026-10-25 自动检查只返回 Anubis 验证页面，未能确认注册是否重新开放；官方邮件列表此前宣布暂停注册且没有人工开户通道。请本人在浏览器查看实际状态，注册可用时完成邮箱验证，再配置 SSH 公钥。若仍关闭，只能等待恢复或由已有账户的维护者自愿接手，不应把尚未发布的候选配方描述为可通过 yay 安装。

## 启动和后台运行

安装后从应用菜单打开 PolySync，会启动当前用户的 `polysync.service` 并打开 `http://127.0.0.1:45124`。重复打开菜单不会重复启动服务。菜单入口使用默认端口；自定义服务端口时请自行打开对应地址。

命令行直接运行：

```sh
polysync -tray=false
# 无浏览器或 SSH 会话：
polysync -tray=false -open=false
```

可选的用户级后台服务：

```sh
systemctl --user daemon-reload
systemctl --user enable --now polysync.service
systemctl --user status polysync.service
journalctl --user -u polysync.service
```

安装不会自动启动服务，也不自动设置登录启动或注销后驻留。用户服务使用当前用户权限，配置沿用 `$XDG_CONFIG_HOME/Polysync`（通常为 `~/.config/Polysync`）。不要使用 `sudo polysync`；不要同时运行手动实例和相同端口的用户服务。

后台服务不需要浏览器一直打开。当前仍通过本地网页完成配对和同步配置，并非所有功能都提供 CLI 子命令。无桌面机器可使用 SSH 本地端口转发访问控制台，不需要将控制台暴露到局域网。

## 更新与卸载

先等待同步和随传任务完成，再停止当前实例。升级使用下载的新 `.deb` 或 Arch 包，命令与安装相同；应用不会自行覆盖包管理器管理的文件。

```sh
systemctl --user stop polysync.service
# 在此安装新版包
systemctl --user daemon-reload
systemctl --user start polysync.service
```

尚未建立 APT 软件源或 AUR 发布通道，因此当前不能依靠系统常规升级发现 PolySync 新版本。已启动的进程需要重启后才会使用新程序；包不会强制重启所有用户的同步服务。

卸载前停止服务并取消登录启动：

```sh
systemctl --user disable --now polysync.service
sudo apt remove polysync       # Ubuntu
# sudo pacman -R polysync      # Arch
systemctl --user daemon-reload
```

卸载只移除安装包拥有的程序、菜单和服务文件。设备身份、配对、工作区、随传暂存及用户同步文件不会被包删除；清理个人数据是独立操作。若曾用自定义 `-data-dir`，新实例必须继续使用同一目录。

## 本地构建

先运行 `scripts/build.sh 0.5.2` 或 PowerShell 的 `scripts/build.ps1` 构建带版本号的静态二进制，然后在 Linux/WSL 中执行：

```sh
python3 scripts/package-linux.py --version 0.5.2 --format deb
python3 scripts/package-linux.py --version 0.5.2 --format arch
# 两套工具均已准备好时：--format all
```

需要 Python 3.11+、`dpkg-deb` 和/或 `makepkg`。构建脚本生成的 `.build-info` 记录版本和二进制 SHA-256，打包时必须与输入匹配。Arch 构建需普通用户以及 makepkg 的常规依赖（fakeroot、bsdtar、zstd 等）。可以用 `--binary-dir`、`--output-dir` 指定独立测试目录；`--revision` 用于同应用版本的打包修订。

包不会内置开发设备身份、缓存或测试数据。项目按所有者选择保留所有权利，见仓库 `LICENSE`；第三方组件继续适用各自许可证。早期本地产物中的“未指定许可证”说明属于此前状态，重新打包后使用当前声明。
