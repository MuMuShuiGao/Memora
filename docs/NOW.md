# NOW

> 状态：生效中
> 摘要：当前进度快照与断点，新会话从这里接上
> 更新：2026-11-27 11:54

这份文件只描述**现在**。已完成的历史进 ADR 或对应设计文档，卡了两周以上的事项应该转成 ADR 或 refactor 方案，不要赖在这里。上限 60 行。

## 在做什么

- **服务端写完并可验收**：`cmd/memora`（`serve` / `hashpass` / `version`）+ 六块 internal（config / auth / store / period / report / api）。零 web 框架；直接依赖只有纯 Go 的 SQLite 驱动和 bcrypt，`CGO_ENABLED=0` 单二进制成立（ADR-0006）。上一轮按 [ADR-0014](decisions/0014-todo-tasks-with-priority.md) 加了第二类对象：`tasks` 表 + `/v1/tasks` 五条接口，报表取数变成两路。本轮按 [ADR-0015](decisions/0015-self-hosted-apk-release-channel.md) 加了更新通道：`internal/api/release.go` 两条只读接口挂在同一道口令后面，读文件系统不读库，配置项多一个 `MEMORA_RELEASE_DIR`。
- 验证腿**都是实测绿的**：`go vet` 干净；`go test` 全过，**64 个测试**（period 7、store 12、report 18、api 27）；`scripts/smoke.sh` **107 项全过**——本机起 HTTPS + 假模型跑端到端，覆盖口令边界（FR-07）、归属算法、正文上限、报表生成与失败留痕（FR-05）、进行中周期出报（FR-06）、重启补跑、限速 429、待办的队首插入 / 整列重排 / 勾掉盖归属 / 未勾掉不进提示词（FR-09~FR-11），以及更新通道的 401 / 404 `not_released` / 500 `release_bad`（清单在包不在）/ 206 续传 / 拉到的包与服务端字节一致。**这 107 项在需求方 Windows 本机跑通**。
- **安卓工程在需求方 Windows 上编译与实跑都通**（`localDebug` 全绿，APK 9.9 MiB）：`android/` 下 17 个 Kotlin 文件 + Gradle 脚手架（AGP 8.7.3 / Kotlin 2.0.21 / Compose BOM 2024.12.01，两个 flavor）。视觉是「编辑部案头」（[ADR-0013](decisions/0013-editorial-desk-visual.md)）；主屏页签「待办 → 记录 → 报表」，打开默认落在待办。待办屏在模拟器逐条点过：录入、勾选沉底、前五/展开、拖把手调序后切走页签再回来顺序仍在。本轮加的应用内更新也在模拟器同一条链上点过：提醒行 → 更新页（本机 v0.2 → v0.9 测试，约 9.7 MB）→ 下载进度 76% → 系统安装页被 `ACTION_VIEW` 真正拉起（logcat 里 `content://…fileprovider` + 校验 Pass）→ 收回系统开关后先落"要先允许 Memora 装应用"那一屏、开回来点"继续安装"也验过。`serverDebug` / `serverRelease` 现在编得过：服务器证书已按 README 拷进 `res/raw/`，四个变体统一用 `~/.android/memora.keystore` 签（覆盖安装的前提）；主屏报头的「改口令 / 改地址」入口已删（需求方定：连上后不再主动改，401 才回连接页）。`38a2dc9` 已在 Windows 上编过 `serverRelease`（v0.3，7,559,220 字节）。本轮按 [ADR-0017](decisions/0017-drawn-line-icons.md) 把行内的「加 / 刷新 / 删」换成自绘线条图标（待办录入行的加、待办行与编辑页的删除、报表页的刷新），在模拟器逐屏点过：录入一条 → 垃圾桶盖朱框 → 再点真删；编辑页删掉那条回到"还没有记录"；`serverRelease` 的更新提醒行与更新页顺带又看了一遍。启动图标按 [ADR-0018](decisions/0018-launcher-icon-ink-mark.md) 换成自适应图标（墨底 + 纸色描边 M + 朱印收笔 + 单色层，只有 anydpi-v26 一份矢量），装上之后在最近任务卡与系统应用列表两处看过真实形状。刷新按 [ADR-0019](decisions/0019-refresh-only-on-reports.md) 收成一枚：记录页与待办录入行的刷新图标删掉、只留报表页，另两屏靠切页签重拉；同步在模拟器重验了"两条待办拖完、切走页签再回来顺序仍在"（测试数据已删），三屏顶部逐屏看过。这一轮按 [ADR-0020](decisions/0020-inline-task-edit.md) 让待办正文点开就在这一行里改：模拟器逐项点过——单行输入预填并聚焦、回车落定、切页签回来是新文字、连按两次返回（先收键盘再放弃）、点另一行先把当前字落定再开新行、改已勾掉那条不改完成态与"完成于"、拖拽顺带重验；服务端零改动，`go vet` / `go test` 仍全过（64 个）；测试数据已删。删除确认随后改成"点别处即撤"：仍连点两次，但第一次点完碰输入框、勾选、别的行、拖拽把手、空白处、切页签或按返回都会撤掉朱框，换点另一只垃圾桶则把确认挪过去（同时只认一条）；待办行与编辑页同一套，模拟器逐项点过，确认真删仍要点第二下，测试数据已清。
- AS 的 Gradle sync 也通：Gradle 本体走华为云镜像（[ADR-0011](decisions/0011-build-mirror-and-gradle-jvm.md)），JVM 靠**在 AS 里手点 Gradle JDK = `ms-17.0.14`**；这个选择落在被 ignore 的 `.idea/gradle.xml`，**别人 clone 后第一次 sync 仍要手点**（写进 `android/README.md`），结论见 [ADR-0012](decisions/0012-ide-and-cli-gradle-jvm.md)。
- systemd 服务已装并 active：`/etc/systemd/system/memora.service` 以 `memora` 用户跑 `memora serve -config /etc/memora/memora.env`（18:40 起）。服务器证书 SAN 就是公网 IP，`notAfter` 实测 2028-11-25。**这台云机同时也是构建机**（Go 1.27.4、systemd 在跑、`ubuntu` 免密 sudo），服务端这一侧不需要在 Windows 上交叉编译再 scp——反向才是要做的那件事：把公钥证书从云机取到 Windows。
- **远端是需求方的公开 GitHub 仓库**（账号与地址不入档，§9），凭据是那把仓库专用 deploy key（已开写权限），私钥只在 `~/.ssh` 下、权限 600。`main` 上是"服务端首版 → 安卓端 → 文档脱敏"一串快进提交，没强推过、没改写历史。要看地址跑 `git remote -v`。

## 卡在哪

- **不卡编码**，上线只剩真模型 key（真机 2026-11-26 23:08 已连上）：安全组入方向 8443/TCP 已放行并实测——公网 `/healthz` 从需求方 Windows 回 200（`--cacert` 校验通过，证书 SAN 就是公网 IP）；服务由 systemd 跑，证书在 `/etc/memora/tls/server.crt`（目录 root 700，读需 sudo）；口令已用真值验过（`/v1/notes` 200），服务器上的口令明文已删。`MEMORA_LLM_API_KEY` 仍为空。
- **更新通道已开启，v0.4 已发（2026-11-27 11:22）**：`/etc/memora/memora.env` 加了 `MEMORA_RELEASE_DIR=/var/lib/memora/release`；发版用户 `ubuntu` 已入 `memora` 组、`/var/lib/memora` 改 770（否则 `release.sh` 直接 scp 没写权限）；**云机二进制已重建安装**——旧二进制根本没有 `/v1/app/release`（未带口令实测 404，配了 env 也还是 404），重建到 `38a2dc9` 后两条路由回 401。`/var/lib/memora/release` 里是 `app.apk`（sha256 `93a9d1ef…`，与本地编出的一致）与 `release.json`（versionCode 4 / 0.4 / notes），`memora` 用户读得到；公网 `/v1/app/release` 未带口令回 401、`/healthz` 200（实测）。手机提醒行与下载安装还待真实打开 App 确认。
- **测试回路已经在 Windows 上实跑一遍**（不再是纸面步骤）：`go build` 出 `memora.exe` → 本机起服务 → 模拟器经 `10.0.2.2` 连上 → `localDebug` 装 APK 操作到报表、待办与更新页。整条链**不需要 Windows 放行规则**——模拟器发起的是宿主机本地连接，不是外部入站。FR-01 至此有了"客户端经内置证书连上服务端"这一半实测；公网那一半也已用 curl 从外部实测通过；手机真机那半 2026-11-26 23:08 由蜂窝网连上，服务端日志可见口令通过。步骤与必填 env 项写在 `android/README.md`。
- 模拟器侧已探明的前提：本机 WHPX 可用（`emulator -accel-check` 实测），AVD `Medium_Phone_API_37.0` 完整（x86_64 / API 37），不需要新建。

## 下一步

1. 手机真机已连上公网服务端（`serverDebug` 内置证书过得了公网 TLS），还差数据一致性：在手机上新写一条记录，到云端库核它落盘、列表回读一致。**验证**：云库 notes 从 0 变 1，手机显示同一条。
2. 真模型生成的报表还没看过一次（`MEMORA_LLM_API_KEY` 还是空的，要先填）：本机两轮用的都是 `scripts/stub-llm.py` 假模型，只钉住调用形状、耗时与落库，不证明真实返回读得通。上一条做完就在手机或模拟器上对 `serverDebug` 生成一次。**验证**：正文读得通，`复制全文` 粘出来是完整一段。
3. 手机上验证 v0.4 更新链：进主屏应出现提醒行 → 更新页显示 0.4 与 notes → 下载 → 系统安装页覆盖安装（不用卸载，同一把钥匙）。**验证**：装完回 App 那一行变成一次性的"刚装好"；若没有提醒，先确认手机装的是 `serverDebug/serverRelease`（同 applicationId、versionCode 小于 4）且能连上服务器。

## 断点说明

没有"改到一半没验证"的代码。六处已知粗糙，摆明了不藏：auth 与 config 没有自己的单测（靠 api 单测和冒烟间接兜住）；冒烟里"上一个已结束的周期"是直接写库造的（今天永远落在当前周里）；**手机真机已连上公网服务端**（2026-11-26 23:08 / 23:43 两轮口令通过，云端日志可见；云库还是空的，「同一条记录」这半没法核）——之前"只编译没运行"当场撞出过一个编译期完全看不见的致命错（`App.kt` 里 `apply` 把口令赋给了外层 Compose 状态，见 ADR-0012 之后的修复），"能编"和"能用"之间确实埋着东西；待办屏的拖拽只在模拟器上以 `input swipe` 验过，真手指的抖动与快速甩动没测；启动图标的单色层只验到"编得过、形状对"，主题图标模式没实测——模拟器抽屉的合成手势滑不开，实际形状是在最近任务卡与系统应用列表两处看的；刷新收成一枚之后，记录/待办页看跨设备新内容或加载失败重试都要切页签、没有提示（ADR-0019 明写的代价）。
`scripts/stub-llm.py` 把每次调用追加到 hits 文件，"参数不合法时一次模型都没打""补跑一次不多打"靠它计数，不是靠日志猜。
**云机侧的验证口径**（部署那轮实测）：在云机内部连自己的公网地址不通（hairpin 不成），**这不能拿来判断防火墙开没开**，公网那一半只能由手机或 Windows 侧验。bcrypt 哈希过 systemd `EnvironmentFile=` 不变形（`$` 后面只可能是数字，用一次性 `systemd-run` 打进 `printenv` 验过）；unit 里 `EnvironmentFile=` 与 `-config` 双读同一个文件、环境变量优先，而 `MEMORA_LOG_DEBUG` 只走 `os.Getenv`，所以**那一行不能删**。出网实测 `api.deepseek.com` 可达（无 key 回 401，TLS 正常），`api.ipify.org` 不可达——下一条真模型报表有网可用。
**本机联调回路整条留在盘上，此刻是连着**：本机的 `memora.exe` 还在听本地端口（值不入档）、`devlocal/`、`tls-test/` 都在；模拟器上装的 `com.memora.app.local` 是本轮为验行内编辑临时 `cp tls-test/server.crt` 覆盖 `app/src/local/res/raw/memora_server_cert.pem` 后编的，所以那一台连得上本机服务；仓库那张证书已还原成不与 `tls-test/server.key` 配对的原样，**下次重装要先再拷一次（这一步别提交）**。另有一坑：`devlocal/` 两份 env 注释里写的口令都过不了 bcrypt（实测 401），本轮靠 `adb install -r` 保住的应用内已存口令重连——要换口令用 `hashpass` 重写 env，别信注释。
**冒烟脚本本身在 Windows 上有两道码页坎**（产品没这问题，安卓端 OkHttp 直接发 UTF-8）：Git Bash 会把 curl 的命令行参数按 GBK 转一遍，中文正文和中文口令全失真；Python 的 stdio 同样按 GBK 解，读服务端的 UTF-8 会把汉字后面的引号吃掉。脚本侧已按"载荷走文件 + `PYTHONUTF8=1` + 口令取 ASCII"绕开，别再往回改。
更新这一轮在模拟器上撞出两个**只有真点才看得见**的错：`canRequestPackageInstalls()` 少个 s（这条编译期就拦住了），以及"我开好了，继续安装"按下去没反应——`install()` 认的入场状态集合里漏了 `NeedsPermission`，界面给了按钮却不接。还有一处是走查时发现的漏网：`Installed` 那一档当时只在更新页里有文案，主屏提醒行返回 null，用户没有任何入口进那页，等于这句永远看不见——现在补成一行一次性的"刚装好"。这一行是靠把 `pending_install_code` 写进偏好文件验出来的（上一轮本机包是 v0.2，版本号没真涨）；v0.3 是真实涨号（2→3），之后又发了 v0.4（versionCode 4，含图标与刷新收敛），手机仍等第一次覆盖安装确认。

## 本轮改动过哪些文档

- 新建 `decisions/0015-self-hosted-apk-release-channel.md`：更新包走同一台服务器的同一端口与同一口令，客户端比版本号后提示、下载、交给系统安装页；四个变体统一用一把仓库外 keystore 签。否掉 Play 内测轨道、GitHub Release、分发平台、静默自装、只留手动下载链接五条。
- `design/memo-and-report.md`：接口表加 `GET /v1/app/release` 与 `/file`，新增 §5.2（这两条为什么也挂口令）与发版脚本那段，§3 配置项加更新包目录，接口计数改成"业务十三 + 更新通道二"。
- `design/android-client.md`：五屏改六屏（更新页）+ 更新页四条定论（提醒是一行字不是弹窗、下载不起前台服务、安装那一下归系统、进度按字节比）、本地存储四样改五样（`memora.update` 两行标记与缓存里的 APK）、错误表加 `not_released` / `release_bad`、§6 补"四变体一把钥匙"的理由、验收段按本轮模拟器实跑更新；`INDEX.md` 补 0015 与两行摘要。
- `deploy/memora.env.example` 加 `MEMORA_RELEASE_DIR`（含 `ProtectSystem=strict` 下只能落在 `/var/lib/memora` 那句）；`android/README.md` 新增"签名：四个变体一把钥匙"与"发一版更新"两节（含首次"允许安装未知应用"），"已知的坑"补检查时机与"服务器只放一个 `app.apk`"这条限制。
- 代码侧新增：`internal/api/release.go` + 单测、`scripts/release.sh`、`android/.../UpdateChecker.kt`（状态机 / 下载 / 交棒）、`UpdatePage.kt`、`res/xml/file_paths.xml`；`MemoApi.kt` 加 `releaseInfo()` 与 `downloadApk()`，`AndroidManifest.xml` 加 `REQUEST_INSTALL_PACKAGES` 与 FileProvider，`build.gradle.kts` 接 keystore、`versionCode` 提到 2；`scripts/smoke.sh` 补 11 项更新通道断言。
- 新建 `decisions/0016-repo-owned-line-endings.md` + 仓库根 `.gitattributes`：Windows 上 11 个 `.go` 长期挂着假 `M`（`git diff` 输出 0 字节），根因是机器级 `core.autocrlf=true` 与工具写回的 LF 打架；现在行尾由仓库定，`git add --renormalize .` 跑过且暂存区为空。否掉"各人自己关 autocrlf""每次 checkout""对噪声文件 skip-worktree""全局 eol=crlf"四条。`INDEX.md` 补 0016 一行。
- 本轮收尾：`design/android-client.md` §界面把报头「改口令 / 改地址」删掉（需求方 2026-11-26 定）；`App.kt` 的 `Masthead` 不再接 `onSettings`，401 回连接页的路径不动。该提交已在 Windows 上编 `serverRelease` 并以 v0.3 发布，真机实点待确认。
- 图标这一轮新建 `decisions/0017-drawn-line-icons.md`（行内短动词改用自绘线条字形：40dp 方框、名称走 `contentDescription`、删除第二次盖朱框；否掉 material-icons 实心字形、图标+文字、只改无障碍名称、下拉刷新四条），`INDEX.md` 补一行；`design/android-client.md` 的界面表四屏动作项、视觉段新增一条、§边界与异常的「删除」「刷新」两条照写；代码侧 `Components.kt` 加 `DeskIcon` / `IconAction` 与三个自绘 `*Glyph`，`TasksScreen` 录入行与行内删除、`NotesScreen` 与 `ReportsScreen` 的刷新、`EditorScreen` 的删除改用图标（那处 `return@TextAction` 顺手改成 if/else）。启动图标这一轮新建 `decisions/0018-launcher-icon-ink-mark.md`（墨底 + 纸色描边 M + 朱印收笔的自适应图标，带单色层；否掉纸底、三行手稿、各密度位图、ImageGen 插画、保持默认五条），`INDEX.md` 补一行；`design/android-client.md` 的依据行补 0017/0018、§视觉 加一条、未决问题「图标与应用名」拆成只剩应用名；代码侧新增 `res/drawable/ic_launcher_foreground.xml` 与 `ic_launcher_monochrome.xml`、`res/mipmap-anydpi-v26/ic_launcher.xml`，`colors.xml` 加 `ic_launcher_background`，`AndroidManifest.xml` 补 `android:icon`。刷新这一轮新建 `decisions/0019-refresh-only-on-reports.md`（只留报表页；否掉三屏都留、全去掉、下拉刷新、自动轮询四条），`INDEX.md` 补一行；`design/android-client.md` 的界面表两行、§边界与异常「刷新」条、§验收里"点刷新"的验法改成切页签。正文行内编辑这一轮新建 `decisions/0020-inline-task-edit.md`（点正文就地单行改字：回车保存、返回放弃、只发 `{body}`；否掉整屏编辑页、弹窗、行尾铅笔、长按、常驻可编辑五条），`INDEX.md` 补一行；`projectneed.md` 的"做"清单加"写错了直接改字"、FR-12 栏；`design/android-client.md` 的待办屏表、新增定论 6、§边界与异常、§验收；`design/memo-and-report.md` 的 rank 段落与 `PATCH /v1/tasks/{id}` 行；代码侧 `TasksScreen.kt` 加编辑态（`editingId` / `editDraft` / `commitEdit` / `cancelEdit` / `startEdit` / `BackHandler`，`toggle` 与 `removeTask` 各自处理编辑中，`TaskRow` 编辑时换单行输入并藏删除与拖把手）与 `Components.kt` 的 `UnderlineField` 多一个可选 `FocusRequester`（焦点得挂在 `BasicTextField` 上）。服务端零改动。随后按需求方要求补"删除确认点别处即撤"：`design/android-client.md` 的删除条改写，代码在 `TasksScreen.kt`（`armedId` 单条确认 + 根级 tap + 返回键）、`EditorScreen.kt` 与 `Components.kt` 的 `dismissOnTouch`。编译验证这一轮把回路写死：`android/README.md` 新增「命令行编译与模拟器自测」（两条编译命令、证书临时替换与还原、`install -r` 保口令、点位走 UI 树、`MSYS_NO_PATHCONV`、截图等一帧），`WORKFLOW.md` §4 补「提交前验证」清单；下一轮照跑，不再试错。

## 待确认（会影响下一步的）

- **SQLite 文件谁定期拷走**（ADR-0007 把备份简化成复制一个文件，但没机制保证真做）——"换机找回"的实际断点。
- **`deploy/memora.service` 写的是 `Restart=on-failure`，`design/memo-and-report.md` §3 写的是 `always`**：两处对不上，先定哪个是要的（进程自己退出算不算要拉起来），再改另一处。
- `MEMORA_WEEKLY_AT` / `MEMORA_MONTHLY_AT` 两个键已留但调度**不读它们**（补跑式判据不需要精确时刻）：删掉，还是按需求方口径想清楚它们还有什么用。
- 模型档位与 key（需求方自己填）。
- 应用名（图标已按 ADR-0018 定；签名 keystore 放哪由 ADR-0015 定死：仓库外那把，口令在用户级 gradle.properties）。
- **视觉方向「编辑部案头」是否保留**（[ADR-0013](decisions/0013-editorial-desk-visual.md)）：不满意的话返工集中在 `Theme.kt` + `Components.kt`，Screen 结构基本不动。待办屏沿用同一套，返工一起带上。
- **公网地址是否固定**：这台是轻量应用服务器（不是 CVM），公网 IP 随实例绑定、没有可转的弹性 IP，实例存活期间不变；退订、迁移或换主机就换地址，那时要重签证书 + 重装 APK（ADR-0009 复查条件）。控制台里对应"安全组"的那一格叫**防火墙**（实例详情顶部标签）。
- **公开仓库的历史不做清理（需求方已定）**：HEAD 干净，历史里留着云厂商+区域与一条默认 git 身份，都不含 IP、密钥、口令或记录正文。因此**不 force push、不改写历史**——以后谁提出要抹历史，先确认这条决定还成立，别再自动 amend。
