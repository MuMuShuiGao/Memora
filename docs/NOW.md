# NOW

> 状态：生效中
> 摘要：当前进度快照与断点，新会话从这里接上
> 更新：2026-11-26 00:52

这份文件只描述**现在**。已完成的历史进 ADR 或对应设计文档，卡了两周以上的事项应该转成 ADR 或 refactor 方案，不要赖在这里。上限 60 行。

## 在做什么

- **服务端写完并可验收**：`cmd/memora`（`serve` / `hashpass` / `version`）+ 六块 internal（config / auth / store / period / report / api）。零 web 框架；直接依赖只有纯 Go 的 SQLite 驱动和 bcrypt，`CGO_ENABLED=0` 单二进制成立（ADR-0006）。
- 验证腿**都是实测绿的**：`make vet` 干净；`make test` 全过，**33 个测试**（period 7、store 2、report 14、api 10）；`make smoke` **70 项全过**——本机起 HTTPS + 假模型跑端到端，覆盖口令边界（FR-07）、归属算法、正文上限、报表生成与失败留痕（FR-05）、进行中周期出报（FR-06）、重启补跑、限速 429。
- 本轮为安卓端补的两件事，都有实测：`GET /v1/periods/now`（周期只在服务端算一份，客户端不许再写一遍周一规则）；`deploy/mkcert.sh` 支持逗号分隔多地址 SAN（验过 `10.0.2.2 + 127.0.0.1 + localhost` 三项同证）。
- **安卓工程已建，但一行都没编译过**：`android/` 下 12 个 Kotlin 文件 + Gradle 脚手架（AGP 8.7.3 / Kotlin 2.0.21 / Compose BOM 2024.12.01，两个 flavor）。这台云机没有 Android SDK、没有 JDK，也没有 `/dev/kvm`（`/dev/kvm` 不存在、CPU flags 无 vmx/svm），**模拟器在这台机器上根本起不来**——所以第一次编译只能在需求方 Windows 上做。这是本轮最大的不确定项，按设计文档"验收"节的说法处理，不当它已经过。
- 报表生成**没有真 key 参与过**：假模型只证明调用形状、重试次数、时刻语义对，不证明 DeepSeek 的返回能用。
- systemd 服务没装过（`deploy/memora.service` 只是文件）。
- **远端是需求方的公开 GitHub 仓库**（账号与地址不入档，§9），凭据是那把仓库专用 deploy key（已开写权限），私钥只在 `~/.ssh` 下、权限 600。`main` 上是"服务端首版 → 安卓端 → 文档脱敏"一串快进提交，没强推过、没改写历史。要看地址跑 `git remote -v`。

## 卡在哪

- **不卡编码**，卡"上线"的是需求方侧三件事：把 key 写进服务端配置文件（**不要贴进对话**）、给业务端口加一条入方向安全组规则、在 Windows 上跑第一次编译。
- 测试回路已经定死在 Windows：`make windows` 出 `memora.exe`（已实测是 PE32+ x86-64）→ 本机起服务 → 模拟器经 `10.0.2.2:8443` 连它 → `local` flavor 装 APK。步骤写在 `android/README.md`。冒烟全程走回环，不需要安全组，所以 FR-01 目前只有"服务在 TLS 上应答正确"这一半证据。
- **当前断点在需求方 Windows 的网络**：第一次 sync 报 `Could not install Gradle distribution ... SocketTimeoutException`，`services.gradle.org` 连不通。已改成镜像优先（Gradle 本体走腾讯云，依赖库阿里云排在官方前面，两边都验过能取到 AGP 8.7.3 / Kotlin 2.0.21 / Compose BOM 2024.12.01 的构件），等需求方 `git pull` 重跑。顺带确认：他的 AS 是 Rabbit 2、自带 JBR 25，而 Gradle 8.11 只认到 23，得选 JVM 21。

## 下一步

1. 需求方 Windows：装 Android Studio → 拷 `memora.exe` + 证书 + env → `server.crt` 改名 `memora_server_cert.pem` 放 `android/app/src/local/res/raw/` → 选 `localDebug` 运行。**验证**：能编译；编译错误贴回来逐条修（预期会有）。
2. 模拟器里跑通记录增删改查 + 搜索 + 超限行文 + 报表生成与复制。**验证**：`memora.exe` 的库里能看到同一条，报表正文能整段复制走。
3. 部署一次并填真 key：`deploy/mkcert.sh`（SAN 从环境变量取）→ systemd 起 → 手机关 WiFi 验。**验证**：手机读到同一条记录，且 `source_count` 与库里条数一致。

## 断点说明

没有"改到一半没验证"的服务端代码。三处已知粗糙，摆明了不藏：auth 与 config 没有自己的单测（靠 api 单测和冒烟间接兜住）；冒烟里"上一个已结束的周期"是直接写库造的（今天永远落在当前周里）；**安卓那 12 个 Kotlin 文件全部只有人眼审过**，重复的 fetch 逻辑、Compose 作用域细节最容易在这种时候埋雷。
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

## 待确认（会影响下一步的）

- **SQLite 文件谁定期拷走**（ADR-0007 把备份简化成复制一个文件，但没机制保证真做）——"换机找回"的实际断点。
- `MEMORA_WEEKLY_AT` / `MEMORA_MONTHLY_AT` 两个键已留但调度**不读它们**（补跑式判据不需要精确时刻）：删掉，还是按需求方口径想清楚它们还有什么用。
- 模型档位与 key（需求方自己填）。
- APK 图标与应用名、签名 keystore 放哪（都不进仓库）。
- 公网地址是否固定（不固定则换 IP 要重装 APK，ADR-0009）、安全组入站规则。
- **公开仓库的历史不做清理（需求方已定）**：HEAD 干净，历史里留着云厂商+区域与一条默认 git 身份，都不含 IP、密钥、口令或记录正文。因此**不 force push、不改写历史**——以后谁提出要抹历史，先确认这条决定还成立，别再自动 amend。
