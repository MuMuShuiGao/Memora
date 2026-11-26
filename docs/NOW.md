# NOW

> 状态：生效中
> 摘要：当前进度快照与断点，新会话从这里接上
> 更新：2026-11-26 17:50

这份文件只描述**现在**。已完成的历史进 ADR 或对应设计文档，卡了两周以上的事项应该转成 ADR 或 refactor 方案，不要赖在这里。上限 60 行。

## 在做什么

- **服务端写完并可验收**：`cmd/memora`（`serve` / `hashpass` / `version`）+ 六块 internal（config / auth / store / period / report / api）。零 web 框架；直接依赖只有纯 Go 的 SQLite 驱动和 bcrypt，`CGO_ENABLED=0` 单二进制成立（ADR-0006）。本轮按 [ADR-0014](decisions/0014-todo-tasks-with-priority.md) 加了第二类对象：`tasks` 表 + `/v1/tasks` 五条接口，报表取数变成两路（记录按创建归属，待办按 `done_at` 归属，未勾掉的一条不进）。
- 验证腿**都是实测绿的**：`go vet` 干净；`go test` 全过，**59 个测试**（period 7、store 12、report 18、api 22）；`scripts/smoke.sh` **96 项全过**——本机起 HTTPS + 假模型跑端到端，覆盖口令边界（FR-07）、归属算法、正文上限、报表生成与失败留痕（FR-05）、进行中周期出报（FR-06）、重启补跑、限速 429，以及待办的队首插入 / 整列重排 / 勾掉盖归属 / 未勾掉不进提示词（FR-09~FR-11）。**这 96 项现在在需求方 Windows 本机跑通**，之前只在 Linux 上跑过。
- **安卓工程在需求方 Windows 上编译与实跑都通**（`localDebug` 全绿，APK 9.8 MiB）：`android/` 下 15 个 Kotlin 文件 + Gradle 脚手架（AGP 8.7.3 / Kotlin 2.0.21 / Compose BOM 2024.12.01，两个 flavor）。`serverDebug` 停在缺证书那一步，报 `raw/memora_server_cert not found`——按设计该是编译期失败，实测确实是。视觉是「编辑部案头」（[ADR-0013](decisions/0013-editorial-desk-visual.md)）。本轮新加的待办屏在模拟器逐条点过：录入、勾选沉底、前五/展开、拖把手调序后点刷新顺序仍在、报表里"用了 3 条记录"=2 记录 + 1 条本周勾掉的待办。
- AS 的 Gradle sync 也通：Gradle 本体走华为云镜像（[ADR-0011](decisions/0011-build-mirror-and-gradle-jvm.md)），JVM 靠**在 AS 里手点 Gradle JDK = `ms-17.0.14`**；这个选择落在被 ignore 的 `.idea/gradle.xml`，**别人 clone 后第一次 sync 仍要手点**（写进 `android/README.md`），结论见 [ADR-0012](decisions/0012-ide-and-cli-gradle-jvm.md)。
- systemd 服务没装过（`deploy/memora.service` 只是文件）。
- **远端是需求方的公开 GitHub 仓库**（账号与地址不入档，§9），凭据是那把仓库专用 deploy key（已开写权限），私钥只在 `~/.ssh` 下、权限 600。`main` 上是"服务端首版 → 安卓端 → 文档脱敏"一串快进提交，没强推过、没改写历史。要看地址跑 `git remote -v`。

## 卡在哪

- **不卡编码**，卡"上线"的是需求方侧两件事：把 key 写进服务端配置文件（**不要贴进对话**）、给业务端口加一条入方向安全组规则。
- **测试回路已经在 Windows 上实跑一遍**（不再是纸面步骤）：`go build` 出 `memora.exe` → 本机起服务 → 模拟器经 `10.0.2.2` 连上 → `localDebug` 装 APK 操作到报表与待办。整条链**不需要 Windows 放行规则**——模拟器发起的是宿主机本地连接，不是外部入站。FR-01 至此有了"客户端经内置证书连上服务端"这一半实测，还缺的是公网那一半（安全组 + 真机）。步骤与必填 env 项写在 `android/README.md`。
- 模拟器侧已探明的前提：本机 WHPX 可用（`emulator -accel-check` 实测），AVD `Medium_Phone_API_37.0` 完整（x86_64 / API 37），不需要新建。

## 下一步

1. 部署一次并填真 key：`deploy/mkcert.sh`（SAN 从环境变量取）→ systemd 起 → 手机关 WiFi 验。**验证**：手机读到同一条记录，且 `source_count` 与库里条数一致。
2. 真模型生成的报表还没看过一次：本机两轮用的都是 `scripts/stub-llm.py` 假模型，只钉住调用形状、耗时与落库，不证明真实返回读得通。上一条做完就在手机或模拟器上对 `serverDebug` 生成一次。**验证**：正文读得通，`复制全文` 粘出来是完整一段。

## 断点说明

没有"改到一半没验证"的代码。四处已知粗糙，摆明了不藏：auth 与 config 没有自己的单测（靠 api 单测和冒烟间接兜住）；冒烟里"上一个已结束的周期"是直接写库造的（今天永远落在当前周里）；**真机与公网那半仍未跑**——之前"只编译没运行"当场撞出过一个编译期完全看不见的致命错（`App.kt` 里 `apply` 把口令赋给了外层 Compose 状态，见 ADR-0012 之后的修复），"能编"和"能用"之间确实埋着东西；待办屏的拖拽只在模拟器上以 `input swipe` 验过，真手指的抖动与快速甩动没测。
`scripts/stub-llm.py` 把每次调用追加到 hits 文件，"参数不合法时一次模型都没打""补跑一次不多打"靠它计数，不是靠日志猜。
**冒烟脚本本身在 Windows 上有两道码页坎**（产品没这问题，安卓端 OkHttp 直接发 UTF-8）：Git Bash 会把 curl 的命令行参数按 GBK 转一遍，中文正文和中文口令全失真；Python 的 stdio 同样按 GBK 解，读服务端的 UTF-8 会把汉字后面的引号吃掉。脚本侧已按"载荷走文件 + `PYTHONUTF8=1` + 口令取 ASCII"绕开，别再往回改。

## 本轮改动过哪些文档

- 新建 `decisions/0014-todo-tasks-with-priority.md`：待办独立成表，否掉"复用 notes 加两列"、"分数排序"、"待办只存本地"、"第三方拖拽库"四条；`projectneed.md` §2 那句"不做：待办勾选、完成状态"随之改掉。
- `design/memo-and-report.md`：`tasks` 表结构、`/v1/tasks` 五条接口、报表两路取数与 `source_count` 的新口径；`design/android-client.md`：待办屏五条定论（前五 + 折叠 + 把手拖拽整列提交 + 回车即加）与视觉，验收段按本轮模拟器实跑更新；`INDEX.md` 补 0014。
- 代码侧新增：`internal/store/tasks.go`、`internal/api/tasks.go`、`internal/report/generate.go` 两路素材、`android/.../TasksScreen.kt`（手写竖直拖拽，拖动期间不改列表只做视觉位移）、`MemoApi.kt` / `Models.kt` / `App.kt` 第三个 tab、`Components.kt` 的 `UnderlineField(onSubmit=…)`。
- `scripts/smoke.sh` 补 26 项待办断言，并把它移植到 Windows 本机跑通：Python 解释器按"能不能真跑"挑（`python3` 可能是应用商店占位符）、`WORK` 经 `cygpath -m` 转换、签证书那一步加 `MSYS_NO_PATHCONV=1`、改库从 `sqlite3` CLI 换成标准库。
- 移植过程中撞出**两个脚本自身的错**，都不是产品坏了：① 中文载荷被码页改写（见"断点说明"）；② "坏 key 下补跑留下 failed"那项一直在跟启动补跑抢时间——好 key 那一轮会先把上一周补成 ready，Linux 上 curl 快、恰好抢在前，Windows 上必输。改成起服务之前就换坏 key + 轮询等失败落库。

## 待确认（会影响下一步的）

- **SQLite 文件谁定期拷走**（ADR-0007 把备份简化成复制一个文件，但没机制保证真做）——"换机找回"的实际断点。
- `MEMORA_WEEKLY_AT` / `MEMORA_MONTHLY_AT` 两个键已留但调度**不读它们**（补跑式判据不需要精确时刻）：删掉，还是按需求方口径想清楚它们还有什么用。
- 模型档位与 key（需求方自己填）。
- APK 图标与应用名、签名 keystore 放哪（都不进仓库）。
- **视觉方向「编辑部案头」是否保留**（[ADR-0013](decisions/0013-editorial-desk-visual.md)）：不满意的话返工集中在 `Theme.kt` + `Components.kt`，Screen 结构基本不动。待办屏沿用同一套，返工一起带上。
- 公网地址是否固定（不固定则换 IP 要重装 APK，ADR-0009）、安全组入站规则。
- **公开仓库的历史不做清理（需求方已定）**：HEAD 干净，历史里留着云厂商+区域与一条默认 git 身份，都不含 IP、密钥、口令或记录正文。因此**不 force push、不改写历史**——以后谁提出要抹历史，先确认这条决定还成立，别再自动 amend。
