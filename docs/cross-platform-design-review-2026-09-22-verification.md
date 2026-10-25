# 跨平台评审报告核对补充（2026-09-22）

本文是对 [跨平台技术设计与实现核对报告](cross-platform-design-review-2026-09-22.md) 的**独立复核结果**，用于回答一个问题：那份报告的各类技术细节，哪些已经成立、哪些写法需要调整、哪些事实它没有覆盖。

用途：供后续执行者（人或代理）据此修改原报告，或据此排期修复代码缺陷。**本文不修改任何运行代码，也不改动原报告正文。**

## 0. 核对方法与口径

- 逐条读取原报告 §16.2 源码索引、§3 平台矩阵、§5/§6/§7/§8/§9/§10/§12 中可代码化验证的断言，与当前工作区源码对照。
- 在当前工作区实跑 `go test ./...` 与 `go vet ./...`（结果见 §4）。
- 复核原报告自带原始记录 `.cache/design-audit-2026-09-22/`（内存快照、services 输出、时间戳）。
- 工作区状态：Git HEAD `9a5f94e2e5bb6617ce6a970542d2b294af06c446`，其上仍有未提交改动（`apps/`、`mobile/`、`internal/update/`、`packaging/` 等）。

**可以复核的**：源码事实、行号锚点、构建与测试命令、报告自带原始记录。
**本轮无法复核的**：Android 真机内存快照（`adb` 不在 PATH，无设备连接）、历史 instrument 用例与浏览器测试记录。这些仍只能维持原报告"单点记录"的口径，不应升级为"已验证"。

## 1. 核对结论

原报告的事实准确性很高，可以直接使用：

- §16.2 的 27 条源码索引 **0 条漂移**（27/27 均指向所述主题）。
- §9.3 Windows 旧数据与 `docs/memory-baseline-2026-09-14.md` **逐项吻合**（含 12.9/13.0、19.1/19.1、23.3/23.8、35.7/42.3、46.1/49.2、48.4、7.54 秒、322 MiB 等）。
- §9.4 安卓快照与原始文件**完全一致**：TOTAL PSS 90531 KiB、TOTAL RSS 274656、TOTAL SWAP PSS 24、Java Heap 19788、Native Heap 26484、Graphics 9588，时间戳 `2026-09-22T19:40:59.8309821+08:00`，`dumpsys activity services` 返回 `(nothing)`。
- Android 十分钟停止、`START_NOT_STICKY`、`onTimeout()`、无 `android:process`、`dataSync` 类型、`minSdk 29 / targetSdk 36`、`0.5.0-dev.2`、`DisableAutoSync: true`、工作区 `autoSync: false`、`onCreate` 自动 `startSync`、三线程池无 shutdown、128/16/128 KiB 各级上限、2000 文件列表上限、64 KiB 导入循环与独占创建——**全部与代码一致**。
- Go 核心：128 MiB 帧、每 Framer 256 KiB×2、256 KiB 扫描缓冲、16 GiB/128 文件、2 秒调度 tick、间隔归一化 30 秒、90 秒在线、60 秒配对过期与 3 次尝试、5 MiB 合并上限与 `os.ReadFile` 整文件读取、随传历史全量载入、活动日志 200 条、8 秒优雅关闭、2 小时会话——**全部与代码一致**。
- 桌面：`perUser` MSI、无服务/无自启/无右键扩展、更新链路（固定仓库、HTTPS 白名单、三段版本、512 MiB、64 KiB 流式、SHA-256、无 Authenticode、helper 短时进程、失败重启旧路径）、构建脚本默认版本 `0.4.0`、非 Windows 托盘占位——**全部与代码一致**。
- Web UI：主状态 2.5 秒无防重入、随传状态 2.5 秒有 `loading` 门闩、更新弹窗 1.5 秒、`tour.js` 恰为 8 步、`index.html` 写"5 个小提示"、焦点/Tab/Esc/减少动态效果——**全部与代码一致**。

## 2. A 类：需要改写的表述

以下写法虽不全是"错"，但会让读者得出与代码不符的结论。**建议改为**一列是可直接采用的成文措辞。

| # | 位置 | 现状 | 问题 | 建议改为 | 依据 |
| --- | --- | --- | --- | --- | --- |
| A1 | §6.3 安全边界 | 只强调"控制台默认只绑定回环地址"，未提同步端口 | 读者会低估局域网暴露面：同步端口默认是**所有网卡** | 补一句："控制台默认仅回环，但同步端口默认监听所有网卡（桌面 `-listen` 默认 `:45123`，安卓 `:45123`）；该端口的暴露面完全由 TLS 1.3 与配对公钥固定承担，可用 `-listen` 收窄。" | `cmd/polysync/main.go:29`；`apps/android/.../SyncService.kt:61` → `mobile/engine.go:37,51` |
| A2 | §4.2 | "`mobile.Engine` 不启动 HTTP 监听器" | 易被读成"没有监听端口" | 补："不启动 HTTP 监听器，但仍会打开真实的 TLS/TCP 同步监听（所有网卡 `:45123`）。" | 同上 |
| A3 | §8.1 | "这个'四个'限制不涵盖所有同步空间、TLS 连接、UI 查询和文件导入" | 方向正确，但没说清 4 的范围与可被占满 | 补："该计数为**进程级**：`transferManager` 内所有任务（发送与接收、所有对端）共享 4 个名额；接收侧由对端发起也会创建任务，因此单个已配对设备即可占满全部名额。" | `internal/service/transfer_store.go:227-235`；`internal/service/transfers.go:359-360` |
| A4 | §5.4 第 2 条、§13 R09 | "后台长时间导入/导出与自动停止可能发生竞争" | 未限定可见性条件，与代码语义不符 | 改为："导入/导出发生在 Activity 处于 resumed 状态时不会被判定为可停止；真正的风险窗口是导入/导出进行中用户切到后台或锁屏。" | `apps/android/.../SyncService.kt:40-41`；`MainActivity.kt:307-333` |
| A5 | §8.3 表格、§11.2 | "没有对应的删除历史/清缓存 UI，也没有接收总开关 UI" | 弱化了实际状态：不是"缺 UI"，而是"默认开且完全不可关" | 改为："AutoReceive 默认为 true，且安卓端 API 白名单不含 `/api/transfer-settings`，因此手机端**既无开关也无接收目录选择**，只要服务在跑就自动收文件。" | `internal/service/transfer_store.go:60`；`mobile/engine.go:141-159` |
| A6 | §6.2 | "有两小时会话截止时间" | 未说明是绝对截止还是空闲超时 | 补："这 2 小时是会话开始时一次性设置的绝对截止（`SetDeadline`），不是空闲超时：大目录同步可能被硬切断重来，空闲连接也会一直占用到 2 小时。" | `internal/service/tcp.go:81,190`；`internal/service/transfers.go:259` |
| A7 | §9.2 控制帧行、§13 R05 | 只写了 128 MiB 帧上限 | 正文通道与帧通道边界不同，容易混淆 | 补："128 MiB 只约束 JSON 控制帧；文件正文走 `Framer.CopyFrom/CopyTo`，不受该上限约束，仅受 16 GiB 扫描/随传限制。" | `internal/protocol/protocol.go:172-188` |
| A8 | §10 表格"原生界面状态" | "各调用一次状态与随传 API" | 实际是同批次内串行调用，且带门闩 | 改为："每 2 秒在同一批次内**先后**调用 `/api/status` 与 `/api/transfers`（串行），有 `refreshing` 门闩保证最多一批在途。" | `MainActivity.kt:39-53` |
| A9 | §16.2 E11 / E04 / E12 | 锚点分别为 conflicts.go:157、engine.go:37、transfer_store.go:69 | 主题成立（±15 行内），但"整文件读取""白名单""List()"分别在别处 | 可选优化锚点：整文件读取 → `conflicts.go:169`；API 白名单 → `mobile/engine.go:141`；`List()` → `transfer_store.go:292`。也可保留现锚点。 | 见左列 |
| A10 | §7.3 | "文本对象和合并输入的上限是 5 MiB，但'选择本地/远端版本'不受该上限保护" | **该表述本身准确**，无需改 | 仅建议补一句：界面不显示该数字，只用"文件过大"这类模糊措辞（预览与合并两条路径的文案还不相同）。 | `internal/service/conflicts.go:19,169,177,199-211`；`internal/webui/static/app.js:190` |

## 3. B 类：建议新增的风险条目

以下缺陷原报告未覆盖，但属于同一主题（资源防护、静默失败、安全边界），建议按 §13 表格的列格式追加为 R17–R22。**严重度为本次评估，不是已复现的攻击。**

| 优先级 / ID | 问题 | 依据与影响 | 建议处理 |
| --- | --- | --- | --- |
| 中 R17 | 发现快照"全有或全无"，失败被静默吞掉 | `ReplaceExternal` 只要发现任一台设备地址数 >16 或地址非法就整份返回错误、不更新 map（`internal/discovery/mdns.go:47-48`）；安卓侧 `catch (_: Exception)` 无任何提示（`NearbyDiscovery.kt:31-33`）。一台异常设备即可让手机发现列表**冻结在最后一次成功快照**，新设备不出现、离线设备不消失 | 快照校验改为逐条丢弃并计数，或在 Go 侧返回被拒条目数；安卓侧至少记录/展示"发现结果被拒绝" |
| 中 R18 | 未配对连接即可触达 `identify` / `pair_request` / `share_invite` | 鉴权检查排在操作分发之后（`internal/service/tcp.go:200-229` 的 `switch`，配对校验在 `:230-235`）。局域网内任意能完成 TLS 握手的客户端均可获取设备 ID、名称与公钥，并反复在对方控制台生成待确认配对请求 | 为 `pair_request` 增加来源限速/去重与来源标注；评估 `identify` 是否需要最小信息量 |
| 中 R19 | `/api/status` 丢弃冲突读取错误，失败表现为"没有冲突" | `internal/service/api.go:91` 用 `_` 丢弃错误；`internal/store/store.go:282-287` 在任一冲突 JSON 解析失败时直接 `return err`，整份列表作废。单个损坏文件会让控制台显示"无待解决冲突" | 改为逐条跳过并计数上报；接口返回"部分冲突不可读取"状态 |
| 低-中 R20 | 随传名额可被单一已配对设备占满 | 接收侧同样走 `create()`（`internal/service/transfers.go:359-360`），与发送共享 4 个名额（`transfer_store.go:227-235`） | 按对端划分配额，或对接收侧单独计数与提示 |
| 低 R21 | `learnPeer` 用对端自报端口覆写已保存地址 | `internal/service/tcp.go:386-405` 直接以 `RemoteAddr + hello.ListenPort` 覆写已配对设备地址与 `share.PeerAddress`。因公钥固定无法冒充身份，但可把后续连接指向对端指定地址 | 校验端口是否在预期集合内，或仅在地址变化时记录待确认 |
| 低 R22 | 对象库与归档每次扫描都会写入 | `objects/`、`history/` 无淘汰（原报告 §8.3 已述），但未指出**每轮同步的每一遍扫描都会 `cacheObjects`**（`internal/service/tcp.go:111,163,284,346`），磁盘只增不减 | 与 R11 的保留策略一并设计，纳入引用检查 |

补充观察（不必单列条目，可在正文一句带过）：手机服务启动失败仅置 `error` 并 `stopSelf()`，无重试或退避（`SyncService.kt:73`）；发现列表 128 台上限为先到先得（`NearbyDiscovery.kt:44`）。

## 4. C 类：建议补入的报告验证记录

原报告 §16.1 声明"既有 `go test ./...`、`go vet ./...` 通过"。本轮在当前工作区**重新实跑并确认成立**：

```text
go version go1.27.1 windows/amd64
go test ./...   → 全部 ok（cmd/polysync、internal/discovery、protocol、service、store、syncer、tray、update、mobile）
go vet ./...    → 无输出，退出码 0
```

但有一个**必须先设置的前置条件**：直连 `proxy.golang.org` 会失败（本轮首次尝试即因 3 个模块拉取失败导致 `setup failed`）。可用的调用方式：

```bash
GOFLAGS=-mod=mod GOPATH="$PWD/.cache/toolchain/go" \
GOMODCACHE="$PWD/.cache/go-mod" GOCACHE="$PWD/.cache/go-build" \
go test ./...
```

建议把这一点写进报告验证附录或 `AGENTS.md`（该文件目前只写了"缓存可设置为项目 .cache/go-build 和 .cache/go-mod"，未点明离线环境必须显式指定）。

仍**不能**由本轮复核支撑的：安卓真机内存快照（无 `adb`/无设备）、历史 instrument 与浏览器测试（未重跑）、Windows MSI 完整升级链路（未重跑）。

## 5. D 类：没有变化的部分

- 原报告 §15 决策表 D01–D14 **全部仍是"待填写"**，本轮没有任何一项被决定。
- §13 风险表 R01–R16 **没有一项在本轮被修改**。
- §1 的 C01（安卓十分钟空闲停止与常驻目标冲突）仍是全报告唯一"代码行为与产品目标直接冲突"的确认项，且仍**未被产品层面确认**。

也就是说：本文补充的是"报告哪里需要改、哪里漏了"，**不改变原报告的核心结论——后台接收策略需要产品决策先行。**

## 6. 附：本次复核对照的证据清单

| 主题 | 证据 |
| --- | --- |
| 发现快照整体拒绝 | `internal/discovery/mdns.go:37-67` |
| 安卓吞掉发现异常、128 上限 | `apps/android/app/src/main/java/org/polysync/android/NearbyDiscovery.kt:31-44` |
| 安卓监听所有网卡 | `SyncService.kt:61`、`mobile/engine.go:37,51` |
| 桌面默认全网卡监听 | `cmd/polysync/main.go:29` |
| 桌面默认回环控制台 | `cmd/polysync/main.go:30` |
| 未配对可触达的操作 | `internal/service/tcp.go:200-235` |
| `learnPeer` 覆写地址 | `internal/service/tcp.go:386-405` |
| 4 个名额的计数范围 | `internal/service/transfer_store.go:227-235` |
| 接收侧也创建任务 | `internal/service/transfers.go:359-360` |
| AutoReceive 默认 true | `internal/service/transfer_store.go:60` |
| 安卓 API 白名单无设置接口 | `mobile/engine.go:141-159` |
| 状态接口丢弃冲突错误 | `internal/service/api.go:91`、`internal/store/store.go:282-287` |
| 冲突整文件读取与 5 MiB 上限 | `internal/service/conflicts.go:19,169,177,199-211` |
| 正文通道不受帧上限约束 | `internal/protocol/protocol.go:172-188` |
| 2 秒调度 tick | `internal/service/service.go:243` |
| 会话绝对截止 | `internal/service/tcp.go:81,190`、`internal/service/transfers.go:259` |
| 快照 128 KiB 上限 | `mobile/engine.go:114` |
| 安卓 watchdog 可见性与十分钟 | `SyncService.kt:23,32-42`、`MainActivity.kt:101-102` |
| 安卓线程池与生命周期 | `SyncService.kt:17-19,92-101` |
| 安卓导入导出 | `MainActivity.kt:307-333` |
| 原始内存快照 | `.cache/design-audit-2026-09-22/`（`snapshot-time.txt`、`android-services.txt`、`android-meminfo.txt`） |
