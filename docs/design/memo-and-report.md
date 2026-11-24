# 设计：记录与报表的数据、接口与传输

> 状态：生效中
> 摘要：两张表的字段与归属算法、七个接口的请求响应形状、自签 HTTPS 与口令校验的落点、进程内补跑式调度
> 更新：2026-11-24 17:24

依据 [ADR-0001](../decisions/0001-server-owned-data-on-lan.md)、[0002](../decisions/0002-single-user-passphrase.md)、[0004](../decisions/0004-manual-and-scheduled-report.md)、[0006](../decisions/0006-go-single-binary-server.md)、[0007](../decisions/0007-sqlite-over-postgres.md)、[0008](../decisions/0008-native-kotlin-client.md)、[0009](../decisions/0009-public-ip-selfsigned-https-access.md)。

标 ①②③ 的三处（归属冗余列、自签证书 2 年、`reports.status` 列）已由需求方于 2026-11-24 确认采纳。

---

## 1. 数据层

SQLite 单文件，WAL 模式，路径来自配置项（ADR-0007）。驱动用 `modernc.org/sqlite`（纯 Go，不引 cgo，保 ADR-0006 的单二进制）。

### `notes`

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | INTEGER PRIMARY KEY | 自增。单人自用，不外泄什么 |
| `body` | TEXT NOT NULL | 原始正文，不做任何改写（ADR-0005） |
| `created_at` | TEXT NOT NULL | UTC，RFC3339 带毫秒。字典序即时间序 |
| `updated_at` | TEXT NOT NULL | 同上 |
| `week_start` | TEXT NOT NULL | ① 归属周起始日（UTC+8 的周一，`YYYY-MM-DD`） |
| `month_start` | TEXT NOT NULL | ① 归属月起始日（`YYYY-MM-01`） |

`week_start` / `month_start` 在**插入那一刻算好冗余存下**，之后任何修改都不重算。这样做的理由：如果只存 `created_at`、查询时现算归属，那么"UTC+8"这条口径会散落在每个查询里，将来时区配置一变，历史报表的归属会集体漂移。多两列换归属永久稳定，并且天然实现了需求 §4 的"改过的旧记录不迁入新周期"。

索引：`idx_notes_created (created_at DESC)`、`idx_notes_week (week_start)`、`idx_notes_month (month_start)`。

关键词过滤用 `body LIKE '%q%' ESCAPE '\'`。千条量级下全表扫也不需要 FTS（估算）；q 必须做 `LIKE` 通配符转义，且一律用参数绑定，不拼字符串。

### `reports`

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | INTEGER PRIMARY KEY | |
| `kind` | TEXT NOT NULL | `week` \| `month` |
| `period_start` / `period_end` | TEXT NOT NULL | 自然周期的起止日，**主键语义在这对上** |
| `status` | TEXT NOT NULL | ③ `pending` \| `running` \| `ready` \| `failed` |
| `body` | TEXT | 模型返回的正文，`status != ready` 时为 NULL |
| `source_count` | INTEGER | 依据多少条记录（FR-08） |
| `fail_reason` | TEXT | 人可读的失败原因，仅 `failed` 时有值 |
| `generated_at` | TEXT | 生成完成或失败的时刻，UTC |
| `created_at` / `updated_at` | TEXT NOT NULL | |

`UNIQUE (kind, period_start)` —— 一个周期只有一份当前报表，重新生成是 UPDATE 覆盖（ADR-0004）。

`status` 这一列不是冗余：FR-05 要求"不静默缺失"，光靠"查不到报表"分不出**还没到点**、**跑失败了**、**跑一半进程死了**三种情况，而第三种必须有办法被重新捞起来。代价是列表里会出现没有正文的行，客户端要按 status 显示不同文案。

## 2. 周期与归属算法

时区固定取服务端配置的 `local_tz`（当前 UTC+8），不在 SQL 里算日期。

- 周一为一周首日；`week_start = 该日本地日期回退到本周周一`
- 手动生成"本周/本月"这类未结束的周期时，`period_end` 仍写自然终点，但**取记录的界限是 `min(now, period_end 23:59:59)`**，`generated_at` 记录实际截止时刻。需求 §4"生成中的报表"由此落地。
- 取记录条件：`week_start = ?`（或 `month_start = ?`），不看 `updated_at`。

## 3. 传输与部署

监听 `0.0.0.0` 上单个业务端口，只接受 TLS（ADR-0009）。地址、端口值不入档（writing-rules §9）。

- **证书**：自签，ECDSA P-256。② 有效期定 **2 年**——一年一换要勤重装 APK，十年又怕泄露后换不掉；到期前 30 天服务端在日志里打提醒。换证书必须同步重装 APK，因为客户端锁定的是这张证书。
- **客户端校验**：APK 内置该证书并做校验，**代码里不留"忽略证书错误"的开关**。没有这条，中间人换一张证书客户端毫无察觉，等于没加密（ADR-0009 的付出的那一条）。
- **systemd**：`Restart=always`，配置文件与库文件路径通过 `EnvironmentFile` 给，不进命令行参数（避免出现在进程列表里）。
- **配置项**：库文件路径、监听端口、`local_tz`、口令哈希、DeepSeek 的 base URL / model / key、两个定时时刻。key 只存在于服务端配置文件，需求方自己填，不进对话、不进文档。

## 4. 鉴权

- 客户端在每个请求带固定头 `X-Access-Key`。服务端只存口令的 **bcrypt 哈希**，不存明文（ADR-0009 连带决定）。
- 校验失败返回 401 且响应体不含任何业务数据（FR-07 的验收线）。
- **限速**：同一来源连续失败到阈值后指数退避拒绝。这台机器实测已有 152 条来自公网的 SSH 暴力拦截规则，说明它在被持续扫描——公网暴露下这一个口令是唯一防线，限速不是可选加固项。
- 来源 IP 记进访问日志用于限速和排查。这跟 writing-rules §9 不冲突：那条禁的是把标识值抄进 `docs/`，运行日志是另一回事。
- 不做会话、不做 token 刷新（ADR-0002）。

## 5. 接口

一律 `/v1` 前缀。错误统一 `{"error":{"code":"...","message":"..."}}`。

| 方法与路径 | 用途 | 关键字段 |
| --- | --- | --- |
| `GET /v1/notes` | 列表 + 过滤 | `q`、`limit`、`before`（游标，取 `created_at`） |
| `POST /v1/notes` | 新建 | 请求 `{body}`；响应含算好的 `week_start` |
| `PATCH /v1/notes/{id}` | 修改 | 只接受 `{body}`；**不接受 `created_at`**，归属不动 |
| `DELETE /v1/notes/{id}` | 删除 | 幂等，已删除返回 404 |
| `GET /v1/reports` | 报表列表 | `kind`、`limit`；返回含 `status` |
| `GET /v1/reports/{kind}/{period_start}` | 取单份 | 未生成返回 404 + `code:"not_generated"` |
| `POST /v1/reports/{kind}/{period_start}/generate` | 手动生成 | 同步返回结果；失败也要落 `status=failed` |

`body` 长度上限先按 2000 字符定（需求 §6 待定项之一，客户端和服务端同用一个常量配置）。超限返回 413 而不是静默截断。

安卓端还有一件事归客户端：进程被杀前把未提交的输入留一份草稿，这是需求 §5 第 3 条，服务端不管（ADR-0001 不做离线，不代表可以让用户白打一段字）。

## 6. 生成报表

1. 按周期取全部 `notes.body`，按 `created_at` 升序拼进提示词，条数写进 `source_count`。
2. 调 DeepSeek 的对话补全接口（OpenAI 兼容），`model` 走配置。提示词要点：只用给到的内容、不编造未提到的交付物（需求 §5 第 4 条）、按主题归并而非逐日流水、输出纯文本。
3. 超时与重试：单次调用超时给到 60s（FR-04 的量级），失败重试 1 次；仍失败则 `status=failed` + `fail_reason`。
4. 空周期（0 条记录）不生成，直接返回 `code:"no_records"`——避免让模型憋出一份空话。

## 7. 定时

**不用 crontab 精确触发，用进程内 ticker 每小时检查一次"哪些已结束的周期还没有 ready 的报表"，缺就补**（ADR-0006 的进程内调度）。

这个形状顺手把 FR-05 的"服务中断错过时刻要能补、不静默缺失"变成自然结果：判断依据是"该周期有没有 ready 报表"，而不是"现在是不是周一早上八点"。机器关机两天再起来，漏掉的周期会被自动补上。代价是每小时空转一次查询，可忽略。

具体两个时刻（周报、月报各自周几几点）仍待需求方定，配置项留好。

一轮扫描里每种报表最多补最近六份，其余留到下一轮。理由是：关机几个月后重启，候选可能有几十周，一次全打完会把重启后的头几分钟全占在排队等模型上（推断，未实测）。候选按周期由新到旧排，所以先拿到的是最近的。

## 8. 本设计不包含

备份机制（谁定期拷走库文件）——尚无结论，见 NOW 待确认。安卓端界面细节与包结构——等接口定了再单独写一份 `design/android-client.md`。
