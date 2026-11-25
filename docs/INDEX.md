# 项目索引

> 状态：生效中
> 摘要：docs/ 全部文档的总目录，进项目先看这里

**项目一句话**：给一个人用的安卓备忘录，随手记纯文字想法，由大模型按自然周/自然月汇总成能直接交出去的周报和月报。

**代码在哪**：服务端在仓库根目录（`cmd/memora` + `internal/` 六块，Go 单二进制），安卓端在 `android/`（Kotlin + Compose，两个 flavor，**尚未编译验证过**）。设计与验收口径见 [design/memo-and-report.md](design/memo-and-report.md) 与 [design/android-client.md](design/android-client.md)，当前进度与断点见 [NOW.md](NOW.md)。

---

## 清单

| 文档 | 状态 | 说明 |
| --- | --- | --- |
| [NOW.md](NOW.md) | 生效中 | 当前进度快照与断点，新会话从这里接上 |
| [projectneed.md](projectneed.md) | 生效中 | 给一个人用的安卓备忘录，随手记纯文字想法，由大模型按自然周/自然月汇总成周报和月报 |
| [WORKFLOW.md](WORKFLOW.md) | 生效中 | 开发流程 + 每一步该动哪个文档 |
| [writing-rules.md](writing-rules.md) | 生效中 | 文档书写规范：分层、状态行、什么能改什么不能改 |
| [decisions/README.md](decisions/README.md) | 生效中 | ADR 目录的收录边界与命名规则 |
| [decisions/0001-server-owned-data-on-lan.md](decisions/0001-server-owned-data-on-lan.md) | 生效中 | 手机直连私网服务器读写，服务器上的副本就是唯一真相；离线记录与补交暂不做 |
| [decisions/0002-single-user-passphrase.md](decisions/0002-single-user-passphrase.md) | 生效中 | 服务端存一个访问口令，客户端记住它；不做注册、登录态管理与用户维度 |
| [decisions/0003-external-llm-generation.md](decisions/0003-external-llm-generation.md) | 生效中 | 需求方自带 API key，服务端调用一个对话补全接口生成报表；不在服务器本地跑模型，也不用模板拼接 |
| [decisions/0004-manual-and-scheduled-report.md](decisions/0004-manual-and-scheduled-report.md) | 生效中 | 定时负责到点归档，手动负责重算与看未结束的周期；每个周期只保留一份当前报表 |
| [decisions/0005-no-redaction-in-app.md](decisions/0005-no-redaction-in-app.md) | 生效中 | 备忘录按原样存储、原样交给外部模型、原样进报表，不引入识别和替换 IP／密钥／手机号的处理 |
| [decisions/0006-go-single-binary-server.md](decisions/0006-go-single-binary-server.md) | 生效中 | 一门 Go 写后端，静态二进制 + systemd 起在单个业务端口上，不进 Docker，定时走进程内调度 |
| [decisions/0007-sqlite-over-postgres.md](decisions/0007-sqlite-over-postgres.md) | 生效中 | 记录和报表都存在服务端一个 SQLite 文件里（WAL），备份就是复制该文件 |
| [decisions/0008-native-kotlin-client.md](decisions/0008-native-kotlin-client.md) | 生效中 | 原生 Kotlin + Compose 写列表、录入、报表查看三块界面，产物是自己装的 APK；不引跨端框架，也不走 PWA |
| [decisions/0009-public-ip-selfsigned-https-access.md](decisions/0009-public-ip-selfsigned-https-access.md) | 生效中 | 服务端在公网 IP 上以自签证书提供 HTTPS，证书内置进 APK 做校验；安全组只放行这一个业务端口 |
| [decisions/0010-minimal-android-deps.md](decisions/0010-minimal-android-deps.md) | 生效中 | 客户端只依赖 OkHttp + kotlinx.serialization，网络层手写；否掉 Retrofit、Ktor、DI 容器、第三方加密库 |
| [decisions/0011-build-mirror-and-gradle-jvm.md](decisions/0011-build-mirror-and-gradle-jvm.md) | 生效中 | Gradle 本体从华为云镜像取、依赖库阿里云两个仓库排在官方前面；构建 JVM 固定为本机的 JDK 17，不用 AS 自带的 JBR 25 |
| [design/README.md](design/README.md) | 生效中 | 定论设计的收录边界 |
| [design/memo-and-report.md](design/memo-and-report.md) | 生效中 | 两张表的字段与归属算法、九个业务接口的请求响应形状、自签 HTTPS 与口令校验的落点、进程内补跑式调度 |
| [design/android-client.md](design/android-client.md) | 生效中 | 手机端界面划分、本地只存哪四样、九个接口怎么调、每种错误显示成什么、地址与证书怎么随构建环境切换 |
| [refactor/README.md](refactor/README.md) | 生效中 | 重构专项的收录边界与开工前置条件 |
| `templates/` | — | 6 份模板：adr / prd / design / refactor / milestone / incident。目录本身无状态，模板自带的 `草案` 是占位提醒 |

`decisions/`、`design/`、`refactor/` 下每新增一份文档，就在这里补一行——格式是「链接 + 状态 + 说明」，状态照抄该文档头部的 `> 状态：`，说明照抄 `> 摘要：`。改名或删文档同样要回来改这张表。

---

## 怎么读这个目录

1. 第一次接触项目：`projectneed.md` → `decisions/` 按编号顺序读 → 关心的模块进 `design/`。
2. 接着上次做：直接读 `NOW.md`。
3. 找不到东西：`design/` 和 `decisions/` 的 README 写了各自的收录边界。

## 阅读顺序建议

新会话开工前：`NOW.md` → 与本任务相关的 ADR → 相关设计文档。**不要跳过 ADR 直接改设计**，里面的备选方案表会告诉你哪些路已经验证过走不通。
