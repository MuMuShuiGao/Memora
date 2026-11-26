# NOW

> 状态：生效中
> 摘要：当前进度快照与断点，新会话从这里接上
> 更新：2026-11-26 12:56

这份文件只描述**现在**。已完成的历史进 ADR 或对应设计文档，卡了两周以上的事项应该转成 ADR 或 refactor 方案，不要赖在这里。上限 60 行。

## 在做什么

- **服务端写完并可验收**：`cmd/memora`（`serve` / `hashpass` / `version`）+ 六块 internal（config / auth / store / period / report / api）。零 web 框架；直接依赖只有纯 Go 的 SQLite 驱动和 bcrypt，`CGO_ENABLED=0` 单二进制成立（ADR-0006）。
- 验证腿**都是实测绿的**：`make vet` 干净；`make test` 全过，**33 个测试**（period 7、store 2、report 14、api 10）；`make smoke` **70 项全过**——本机起 HTTPS + 假模型跑端到端，覆盖口令边界（FR-07）、归属算法、正文上限、报表生成与失败留痕（FR-05）、进行中周期出报（FR-06）、重启补跑、限速 429。
- 本轮为安卓端补的两件事，都有实测：`GET /v1/periods/now`（周期只在服务端算一份，客户端不许再写一遍周一规则）；`deploy/mkcert.sh` 支持逗号分隔多地址 SAN（验过 `10.0.2.2 + 127.0.0.1 + localhost` 三项同证）。
- **安卓工程已在需求方 Windows 上首次编译通过**（`localDebug` 全绿，APK 9.6 MiB），**且 AS 的 Gradle sync 也首次通过**：`android/` 下 12 个 Kotlin 文件 + Gradle 脚手架（AGP 8.7.3 / Kotlin 2.0.21 / Compose BOM 2024.12.01，两个 flavor）。`localDebug` 全绿，APK 实测 9.6 MiB、`res/raw/memora_server_cert.pem` 在包里；`serverDebug` 停在缺证书那一步，报 `raw/memora_server_cert not found`——按设计该是编译期失败，实测确实是。整轮只改了一处真实编译错误（`CredentialStore.passphrase()` 表达式体里用了 `return`，改块体）。
- 报表生成**没有真 key 参与过**：假模型只证明调用形状、重试次数、时刻语义对，不证明 DeepSeek 的返回能用。
- systemd 服务没装过（`deploy/memora.service` 只是文件）。
- **远端是需求方的公开 GitHub 仓库**（账号与地址不入档，§9），凭据是那把仓库专用 deploy key（已开写权限），私钥只在 `~/.ssh` 下、权限 600。`main` 上是"服务端首版 → 安卓端 → 文档脱敏"一串快进提交，没强推过、没改写历史。要看地址跑 `git remote -v`。

## 卡在哪

- **不卡编码**，卡"上线"的是需求方侧两件事：把 key 写进服务端配置文件（**不要贴进对话**）、给业务端口加一条入方向安全组规则。原来第三件"在 Windows 上跑第一次编译"本轮已完成。
- **测试回路已经在 Windows 上实跑一遍**（不再是纸面步骤）：`go build` 出 `memora.exe`（17.6 MiB PE32+）→ 本机起服务 → 模拟器经 `10.0.2.2` 连上 → `localDebug` 装 APK 操作到报表。整条链**不需要 Windows 放行规则**——模拟器发起的是宿主机本地连接，不是外部入站。FR-01 至此有了"客户端经内置证书连上服务端"这一半实测，还缺的是公网那一半（安全组 + 真机）。步骤与必填 env 项写在 `android/README.md`。
- **sync 两个拦路石都已解掉**（Windows 实测）：Gradle 本体走华为云镜像（[ADR-0011](decisions/0011-build-mirror-and-gradle-jvm.md)），JVM 靠**在 AS 里手点 Gradle JDK = `ms-17.0.14`**，12:03 `onSuccess(RESOLVE_PROJECT)`，之后 `Incompatible` 零次。纠正两条我自己写错的结论：`java.home` 那条**实测没被 AS 采纳**（写回 17、重启后宏仍解析出 JBR 25），已从 `local.properties` 撤掉；机制也不是"IDE 预检"，堆栈显示 IDE 真的发起了构建、由 Gradle 拒绝。代价记下来：这个选择落在 `.idea/gradle.xml`，被 ignore、仓库带不过去，**别人 clone 后第一次 sync 仍要手点**——已写进 `android/README.md` 的步骤。结论在 [ADR-0012](decisions/0012-ide-and-cli-gradle-jvm.md)。两条都要写的结论在 [ADR-0012](decisions/0012-ide-and-cli-gradle-jvm.md)。
- 模拟器侧已探明的前提：本机 WHPX 可用（`emulator -accel-check` 实测），AVD `Medium_Phone_API_37.0` 完整（x86_64 / API 37），不需要新建。

## 下一步

1. 部署一次并填真 key：`deploy/mkcert.sh`（SAN 从环境变量取）→ systemd 起 → 手机关 WiFi 验。**验证**：手机读到同一条记录，且 `source_count` 与库里条数一致。
2. 真模型生成的报表还没看过一次：本机那轮用 `scripts/stub-llm.py` 假模型，只钉住调用形状、耗时与落库，不证明真实返回读得通。上一条做完就在手机或模拟器上对 `serverDebug` 生成一次。**验证**：正文读得通，`复制全文` 粘出来是完整一段。

## 断点说明

没有"改到一半没验证"的服务端代码。三处已知粗糙，摆明了不藏：auth 与 config 没有自己的单测（靠 api 单测和冒烟间接兜住）；冒烟里"上一个已结束的周期"是直接写库造的（今天永远落在当前周里）；**安卓那 12 个 Kotlin 文件之前只过一次编译、从没运行过**，这一跑当场撞出一个编译期完全看不见的致命错（见"本轮改动"）——"能编"和"能用"之间确实埋着东西，剩下的雷要靠继续点出来。
`scripts/stub-llm.py` 把每次调用追加到 hits 文件，"参数不合法时一次模型都没打""补跑一次不多打"靠它计数，不是靠日志猜。

## 本轮改动过哪些文档

- 新建 `decisions/0010-minimal-android-deps.md`：客户端只依赖 OkHttp + kotlinx.serialization，否掉 Retrofit / Ktor / DI 容器 / 第三方加密库；并记下"证书作唯一信任锚"选它是因为漏拷文件会**编译失败**而不是运行时永远握手失败。
- 新建 `design/android-client.md`，并按已写出的代码回填了四处：导航改为 `AppRoot` 状态机（不引导航库）、下拉刷新改"刷新"按钮、口令清除入口在顶栏、长度计数按码点。
- `design/memo-and-report.md` §5：加 `GET /v1/periods/now` 一行；§7 那句每轮最多补六份仍标着"（推断，未实测）"。
- `INDEX.md`：补 0010 与 android-client 两行。
- 代码侧新增：`internal/api/periods.go`、`android/**`（含 `README.md`、`.gitignore`）；`Makefile` 加 `windows` 目标。
- 需求方选了"本地装 Android Studio 开发"（不在云机上装 Android 工具链编 APK，也不在 Windows 跑远程开发——AS 没有那个能力）。为了 clone 下来就能编 `localDebug`，仓库里放了一张**只含模拟器别名与回环地址**的测试证书（668 字节，私钥不在此列），`server` 那张照旧只被手拷。
- 标识值扫过两遍：`docs/` 与 `android/` 里没有 IP、端口、账号、家目录、密钥；`server` flavor 的默认地址是空串，公网地址不进仓库也不进 APK；`*.pem` / `*.keystore` 已被 ignore。
- **事后又扫了一遍全部历史**（仓库是公开的，推送已完成，所以这轮是审计不是事前检查）：全部提交里没有任何 IP、口令哈希真值、API key、证书、`.db`、邮箱、手机号、家目录——冒烟与单测全是合成数据。补堵了一处漏网的：ADR-0009 与 `scripts/smoke.sh` 原本写着云厂商与区域，现改成"云上主机/云厂商的安全组"。要说清：**改 HEAD 抹不掉历史**，厂商+区域仍能从 `12835de` 那批提交里翻出来；另外最新提交的 git 作者是这台机器的默认身份（账号名+厂商字样），修正它要 force push `main`——问过，需求方选择接受，见"待确认"。
- 本轮（Windows 首次编译）：新建 [ADR-0011](decisions/0011-build-mirror-and-gradle-jvm.md) 记下镜像与 JVM 两条决定，`INDEX.md` 补一行；`gradle-wrapper.properties` 镜像换成华为云并删掉没起作用的 `networkTimeout`；`settings.gradle.kts` 删掉冗余的阿里云 `gradle-plugin` 仓库；`android/README.md` 顶部"还没编译过"换成实测结论、排障两节合成一节；`CredentialStore.kt` 一处表达式体改块体；`android/.gitignore` 补 `.idea/`。仓库内只写占位符不写绝对路径，JVM 那条实际落在仓库外的用户级 `~/.gradle/gradle.properties`。

- 本轮（AS 里 sync 报 JVM 不兼容，最后通了）：新建 [ADR-0012](decisions/0012-ide-and-cli-gradle-jvm.md)（含两处我自己写错的结论和更正过程），`INDEX.md` 补一行并顺手改掉"安卓尚未编译验证过"的过时说法；ADR-0011 只加头部更正行，正文不动；`android/README.md` 排障段改成"IDE 手点 / 命令行配置"两套入口的对照表，并把"第一次必须手点 Gradle JDK"写进跑模拟器那节；`local.properties` 里我加的 `java.home` 已撤（未生效的配置不留）。两条方法教训进了 0012：**没验证过的配置该标"未验证"而不是删掉**，以及**"这个 JVM 值是对的"推不出"IDE 会从我写的这行读到它"**。

- 本轮（模拟器实跑）：修掉一个编译期看不见的致命错——`App.kt` 里 `MemoApi(url).apply { passphrase = secret }`，Kotlin 在 `apply` 内**优先解析外层局部变量**而不是接收者成员，赋值打到了 Compose 状态上，`MemoApi.passphrase` 始终是空串；于是"测试并保存"的探测请求能过（那处是显式 `probe.passphrase =`），进主界面后每个请求都带空口令 → 401 → 被自己的 401 处理踢回连接页，App 看着像"永远连不上"。改成 `.also { it.passphrase = secret }`。抓法值得记：拿同一张自签证书另起一个只打印请求头的临时 HTTPS 服务，把连接页地址指过去，一次就看见探测带对值、`/v1/notes` 带空串。
- `android/README.md` 按实测改四处：补 `MSYS_NO_PATHCONV=1`（Git Bash 会把 openssl 的 `-subj "/CN=memora"` 改写成 `D:/Git/CN=memora`，脚本在 Windows 上签不出证书）、`make windows` 换成本机可直接跑的 `go build`（Git Bash 里没有 make）、env 六个必填项与 `hashpass` 写清（原步骤漏了 `MEMORA_DB_PATH`/`PASS_HASH`/`LOCAL_TZ`，照抄起不来）、"要放行入站端口"那条改成实测不需要的结论。本机重签的证书覆盖了仓库里 `local` 那张（SAN 相同、私钥不同），`git status` 会一直显示这一个 `M`，**不提交**。

## 待确认（会影响下一步的）

- **SQLite 文件谁定期拷走**（ADR-0007 把备份简化成复制一个文件，但没机制保证真做）——"换机找回"的实际断点。
- `MEMORA_WEEKLY_AT` / `MEMORA_MONTHLY_AT` 两个键已留但调度**不读它们**（补跑式判据不需要精确时刻）：删掉，还是按需求方口径想清楚它们还有什么用。
- 模型档位与 key（需求方自己填）。
- APK 图标与应用名、签名 keystore 放哪（都不进仓库）。
- 公网地址是否固定（不固定则换 IP 要重装 APK，ADR-0009）、安全组入站规则。
- **公开仓库的历史不做清理（需求方已定）**：HEAD 干净，历史里留着云厂商+区域与一条默认 git 身份，都不含 IP、密钥、口令或记录正文。因此**不 force push、不改写历史**——以后谁提出要抹历史，先确认这条决定还成立，别再自动 amend。
