# NOW

> 状态：生效中
> 摘要：当前进度快照与断点，新会话从这里接上
> 更新：2026-11-27 12:38

这份文件只描述**现在**。已完成的历史进 ADR 或对应设计文档，卡了两周以上的事项应该转成 ADR 或 refactor 方案，不要赖在这里。上限 60 行。

## 在做什么

- **服务端写完了**：`cmd/memora`（`serve` / `hashpass` / `version`）+ 六块 internal（config / auth / store / period / report / api）。零 web 框架，`CGO_ENABLED=0` 单二进制（ADR-0006）。本轮按 [ADR-0021](decisions/0021-calendar-dated-tasks.md) 给 `tasks` 加 `due_on TEXT NOT NULL`：POST 缺省落服务端本地时区的今天，PATCH 只认真实存在的 `YYYY-MM-DD` 且不能清空（空串 400），启动时按 `created_at` 回填历史行。验证：`go vet` 干净、`go test` 70 个全过、`scripts/smoke.sh` 113 项全过（需求方 Windows 实测）。
- **安卓在需求方 Windows 上编译与实跑都通**（`assembleLocalDebug` 全绿）：`android/` 下 18 个 Kotlin 文件 + 两 flavor，视觉「编辑部案头」（ADR-0013），行内图标 ADR-0017，启动图标 ADR-0018，刷新只留报表页 ADR-0019，正文行内改字与删除确认点别处即撤 ADR-0020。本轮页签改成「待办 / 日历 / 记录 / 报表」，新增 [CalendarScreen](../android/app/src/main/java/com/memora/app/CalendarScreen.kt)：周一起始固定六行、一格最多三枚记号（多出的折成两条短横 + 「+N」）、月内周一挂周号、相邻月可点、默认选中今天且翻月落到 1 号。模拟器逐项点过：日历加一条 → 待办页立刻出现；待办页加一条显式带手机本地今天；12 月 3 日那条在待办页显示「安排 · 12月3日 周四」；勾选同步沉底、`done_week_start` 与周归属不变；翻月、今天、相邻月、四任务格的「+2」；测试数据已从本机库删净。
- **云机服务端已重建到 `c51a2fa`**（systemd active）：启动日志 `历史待办已补上安排日期 count=6`，真实库迁移与回填都成功。公网 `/healthz` 200，`/v1/app/release`、`/v1/app/release/file` 未带口令均 401（实测，从需求方 Windows 发起）。
- **v0.5 已发（2026-11-27 12:18）**：`serverRelease` 已编（7,593,972 字节，versionCode 5 / 0.5，`aapt2` 核对），`scripts/release.sh` 上传成功；云上 `app.apk` 与本地 sha256 前 16 位一致（`9a8f2b4431399ca5`），`release.json` 的 notes 写明日历页。
- **真机第一次下载 v0.5 失败已修**：更新页显示 `stream was reset: INTERNAL_ERROR`，云机日志两次下载都整 60 秒结束（`took=1m0.976s` / `1m0.236s`）——HTTP/2 把全局 `WriteTimeout` 当成单流写期限，慢网拉 7.6 MB 没拉完就被 `RST_STREAM`。按 [ADR-0022](decisions/0022-download-write-deadline.md) 把下载这条响应单独放宽到十分钟（日志包装补了 `Unwrap`，否则 `ResponseController` 静默失效），云机已重建；手机重试下载即可，APK 本身没改、无需重新发版。
- **远端是需求方的公开 GitHub 仓库**，凭据是仓库专用 deploy key（私钥只在 `~/.ssh`，600）。`main` 已推到 `87f3237`，没强推、没改写历史。要查地址跑 `git remote -v`。

## 卡在哪

- **不卡编码**，上线只剩真模型 key（真机 2026-11-26 已连上）：公网 8443/TCP 安全组已放行，服务由 systemd 跑，证书 `/etc/memora/tls/server.crt` SAN 就是公网 IP（`notAfter` 2028-11-25），口令已用真值验过。`MEMORA_LLM_API_KEY` 仍为空，报表生成时服务端会 WARN「模型未配置」。
- **手机真机还没验 v0.5 更新链**：应出现提醒行 → 更新页显示 0.5 与 notes → 下载 → 系统覆盖安装（同一把钥匙）。若没提醒，先确认手机装的是 `server*` 变体且 versionCode 小于 5、能连上服务器。
- 模拟器侧前提：本机 WHPX 可用，AVD `Medium_Phone_API_37.0` 完整（x86_64 / API 37），不用新建。

## 下一步

1. 手机真机重试 v0.5 下载（之前整 60 秒被掐已修），装上后再进日历加一条、回待办页核对同一条（真实设备上的双页同步）。**验证**：下载拉得完、装上 0.5、提醒行变一次性的「刚装好」，两边列表一致。
2. 真模型生成一次报表（先填 `MEMORA_LLM_API_KEY`）：本机两轮用的都是 `scripts/stub-llm.py` 假模型，只钉调用形状，不证明真实返回读得通。**验证**：正文读得通，`复制全文` 粘出来是完整一段。
3. 确认云库那 6 条历史待办回填出的日期与需求方预期一致（日志已证明回填发生，内容没逐条核）。

## 断点说明

没有"改到一半没验证"的代码。已知粗糙：auth 与 config 没有自己的单测（靠 api 单测和冒烟间接兜住）；冒烟里"上一个已结束的周期"直接写库造；待办拖拽只以 `input swipe` 在模拟器验过，真手指的抖动没测；启动图标单色层没实测主题图标模式（抽屉手势滑不开，只在最近任务卡与应用列表看过形状）；刷新收成一枚后记录/待办页看跨设备新内容要切页签（ADR-0019 明写的代价）；日历每次进页拉全量列表、无缓存，任务多时首屏会慢，且"今天"以手机本地为准、服务端缺省日期以服务端时区为准——跨时区旅行时两边可能差一天（ADR-0021 的取舍）。
**验证口径**：云机内部连自己的公网地址不通（hairpin 不成），防火墙开没开只能由手机或 Windows 侧验；bcrypt 哈希过 systemd `EnvironmentFile=` 不变形（用一次性 `systemd-run` 打过 `printenv`），unit 里 `EnvironmentFile=` 与 `-config` 双读同一文件、环境变量优先，`MEMORA_LOG_DEBUG` 只走 `os.Getenv`，那行不能删；出网实测 `api.deepseek.com` 可达（无 key 回 401），`api.ipify.org` 不可达。
**本机联调回路整条留在盘上**：本机 `memora.exe` 听着本地端口，`devlocal/`、`tls-test/` 都在；模拟器上的 `com.memora.app.local` 是本轮临时 `cp tls-test/server.crt` 覆盖 local flavor 证书后编的，仓库那张已还原，**下次重装要先再拷一次（别提交）**；`devlocal/` 两份 env 注释口令过不了 bcrypt（实测 401），靠 `adb install -r` 保住已存口令重连，要换口令用 `hashpass` 重写 env。
**冒烟脚本在 Windows 有两道码页坎**（产品没这问题）：Git Bash 把 curl 参数按 GBK 转，中文正文失真；Python stdio 同样按 GBK 解。脚本已按"载荷走文件 + `PYTHONUTF8=1` + 口令 ASCII"绕开，别再改回去。
**更新链只在真点下才露过的错**（已修）：`canRequestPackageInstalls()` 少个 s；"我开好了，继续安装"不认 `NeedsPermission`；`Installed` 档主屏没有入口；v0.5 首次真机下载被 HTTP/2 单流写超时整 60 秒掐断（ADR-0022 已放宽下载期限并重建云机，真机待重试确认）。v0.3（versionCode 2→3）与 v0.4 是真实涨号发过的版，v0.5 接在它们后面。

## 本轮改动过哪些文档

- 新建 `decisions/0022-download-write-deadline.md`：下载响应单独放宽 HTTP/2 单流写期限到十分钟、日志包装补 `Unwrap`；否掉全局放宽/关闭写超时、另起文件服务、客户端分段续传、强制 HTTP/1 五条。`INDEX.md` 与 `design/memo-and-report.md` 的下载接口行跟进；新增回归测试 `TestReleaseDownloadSurvivesSlowClient`（h2 + 300ms 写超时 + 慢读）。
- 新建 `decisions/0021-calendar-dated-tasks.md`：`due_on` 落库、日历纯客户端按天分组、历史回填；否掉新建日历表/接口、客户端本地日期字段、服务端分组接口、日历页另存一份列表、把完成态日期当安排日期五条。`INDEX.md` 补 0021。
- `design/memo-and-report.md`：tasks 字段与接口表加 `due_on`、回填段、POST 缺省与 PATCH 校验规则；`design/android-client.md`：页签四枚、日历屏七条定论（固定六行、三枚记号、周号、相邻月、单选中、只加与勾、待办页显式日期）、边界与验收；`projectneed.md` 加"按天看和记"需求层描述与对应 FR。
- 代码：`internal/store`（加列、迁移、回填、UpdateTaskDueOn）、`internal/api/tasks.go`（校验与默认值）、`internal/period`（DayID / IsDayID）、`cmd/memora/main.go`（启动回填）、`CalendarScreen.kt`、`Format.kt`（日期文案）、`App.kt`（页签）、`TasksScreen.kt`（非今天显示安排行）、`MemoApi.kt`（createTask 带 due_on）、`Components.kt`（左右箭头）、`build.gradle.kts`（v0.5）、`scripts/smoke.sh`（6 项日期断言）。

## 待确认（会影响下一步的）

- **SQLite 文件谁定期拷走**（ADR-0007 把备份简化成复制一个文件，但没机制保证真做）——"换机找回"的实际断点。
- **`deploy/memora.service` 写的是 `Restart=on-failure`，`design/memo-and-report.md` §3 写的是 `always`**：先定哪个是要的，再改另一处。
- `MEMORA_WEEKLY_AT` / `MEMORA_MONTHLY_AT` 已留但调度不读（补跑式判据不需要精确时刻）：删掉还是想清楚用途。
- 模型档位与 key（需求方自己填）。
- 应用名（图标已按 ADR-0018 定；签名 keystore 放哪由 ADR-0015 定死：仓库外那把，口令在用户级 gradle.properties）。
- **视觉方向「编辑部案头」是否保留**（ADR-0013）：返工集中在 `Theme.kt` + `Components.kt`，Screen 结构基本不动。
- **公网地址是否固定**：轻量应用服务器，公网 IP 随实例绑定、没有弹性 IP，实例存活期间不变；退订、迁移或换主机就换地址，那时要重签证书 + 重装 APK（ADR-0009 复查条件）。
- **公开仓库的历史不做清理（需求方已定）**：HEAD 干净，历史里留着云厂商+区域与一条默认 git 身份，都不含 IP、密钥、口令或记录正文。**不 force push、不改写历史**。