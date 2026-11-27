# 设计：记录、待办与报表的数据、接口与传输

> 状态：生效中
> 摘要：四张表的字段与归属口径（读书不进报表）、十九个接口的请求响应形状（业务十七 + 更新通道二）、自签 HTTPS 与口令校验的落点、进程内补跑式调度
> 依据：[ADR-0001](../decisions/0001-server-owned-data-on-lan.md)、[0002](../decisions/0002-single-user-passphrase.md)、[0004](../decisions/0004-manual-and-scheduled-report.md)、[0006](../decisions/0006-go-single-binary-server.md)、[0007](../decisions/0007-sqlite-over-postgres.md)、[0008](../decisions/0008-native-kotlin-client.md)、[0009](../decisions/0009-public-ip-selfsigned-https-access.md)、[0014](../decisions/0014-todo-tasks-with-priority.md)、[0015](../decisions/0015-self-hosted-apk-release-channel.md)、[0020](../decisions/0020-inline-task-edit.md)、[0021](../decisions/0021-calendar-dated-tasks.md)、[0025](../decisions/0025-backup-snapshot-and-offsite-pull.md)、[0026](../decisions/0026-manual-offsite-pull.md)、[0027](../decisions/0027-completion-takeaway.md)、[0029](../decisions/0029-reading-list.md)。
> 更新：2026-11-28（新增 §1 `books` 表与 §5 四个读书接口，读书不进周报月报素材，按 ADR-0029；此前 2026-11-27 新增 §8 备份与恢复按 ADR-0025、盘外副本改手动按 ADR-0026，按 ADR-0021 给 `tasks` 加 `due_on`，按 ADR-0027 给待办加完成心得 `takeaway`）

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

### `tasks`

待办是与记录平行的第二类对象（ADR-0014）：多两列状态，排序语义完全不同，所以不复用 `notes`。

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | INTEGER PRIMARY KEY | 自增 |
| `body` | TEXT NOT NULL | 原文，同样不改写（ADR-0005） |
| `done` | INTEGER NOT NULL DEFAULT 0 | 0 未完成 / 1 已完成。CHECK 只认这两个值 |
| `rank` | INTEGER NOT NULL | 未完成序列内的名次，从 1 起连续无空洞 |
| `done_at` | TEXT | 勾掉的时刻，UTC。未完成时为 NULL，它是报表归属的依据 |
| `done_week_start` / `done_month_start` | TEXT | ① 同 `notes` 那两列的写法，但算的是**完成那一刻**所在的周/月。未完成时为 NULL |
| `due_on` | TEXT NOT NULL | 安排日期，服务端本地时区的自然日 `YYYY-MM-DD`。只决定日历把它放在哪一格，与报表归属无关（ADR-0021） |
| `takeaway` | TEXT NOT NULL DEFAULT '' | 完成心得，最多 50 个字符，空串表示没写。勾选、取消勾选与改正文都不碰它；空串可写回，等于清掉（ADR-0027） |
| `created_at` / `updated_at` | TEXT NOT NULL | 同 `notes` |

索引：`idx_tasks_open (done, rank)`、`idx_tasks_done (done, done_at DESC)`、`idx_tasks_done_week (done_week_start)`、`idx_tasks_done_month (done_month_start)`、`idx_tasks_due (due_on)`。

`rank` 只在 `done = 0` 的集合里有意义，且**凡改到顺序的写操作之后整体重编**：新建排第 1（刚想起来的事正因为要先做才会去记），删除、勾掉、取消勾选、拖拽都走"按给定顺序重编号"这一条路径。改正文例外：`PATCH {body}` 只动 `body` 与 `updated_at`，rank、完成态与归属列原样（[ADR-0020](../decisions/0020-inline-task-edit.md)）。代价是一次拖拽写 N 行，换来的是"前 5 名"这个查询不需要处理并列、空洞、分数耗尽这三种情况——待办量级是几十条（估算），一个事务里重写全部未完成的 rank 毫无压力。

已完成的行保留它勾掉那一刻的 rank 但不再维护，已完成区按 `done_at` 倒序显示。`done = 1` 的行不参与重编号，所以 rank 的连续性约束只覆盖未完成集。

`due_on` 的取值规则：创建时显式给了日期就用它；没给就落服务端本地时区的「今天」。升级前已有的行在启动时按 `created_at` 的本地日期回填一次，回填只碰 `due_on` 为空的行、幂等。日历页拿到的是同一份 `GET /v1/tasks` 全量，按 `due_on` 分天——没有按月的查询接口，也没有第二张表。日期只有自然日，没有时分与提醒。

### `books`

读书是第三类业务对象（[ADR-0029](../decisions/0029-reading-list.md)）：记录带创建时间、待办带完成态与顺序，书只有读完年月和一句心得，三者互不转换，所以单独成表。

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | INTEGER PRIMARY KEY | 自增 |
| `title` | TEXT NOT NULL | 书名，必填，≤100 码点 |
| `author` | TEXT NOT NULL DEFAULT '' | 作者，可空，≤100 码点 |
| `finished_month` | TEXT NOT NULL | 读完年月 `YYYY-MM`。只有月，没有日、时分与时区含义（需求方 2026-11-27 定） |
| `takeaway` | TEXT NOT NULL DEFAULT '' | 读完心得，≤200 码点，空串表示没写；上限比待办心得的 50 字宽，书的心得是一段完整的想法（ADR-0029） |
| `created_at` / `updated_at` | TEXT NOT NULL | 同 `notes` |

索引：`idx_books_month (finished_month DESC)`。列表按 `finished_month` 倒序、同月按 `created_at` 倒序、再按 `id` 倒序，一次给全量（年几十本，不做搜索与分页），另加 1000 条安全上限。`finished_month` 的格式校验在 api 层：只认带零填充的 `YYYY-MM`，解析后再格式化回比一次。

`books` **不参与报表归属**：读完一本书不是一份工作交付，书单不进周报月报素材（§6），`week_start` / `month_start` 那套冗余列在这里没有对应物。备份快照用 `VACUUM INTO` 导整库，`books` 自动包含在内，不增加运维动作。

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
- **待办看完成时刻，不看 `created_at`**：一条待办归哪个周期，只由"它在哪一周被勾掉"决定，上周记的待办这周做完就算这周的（需求 §4 完成时刻、FR-11）。没勾掉的待办不属于任何报表周期。落法与记录的 ① 同源：勾掉那一刻把 `done_week_start` / `done_month_start` 冗余存下，取消勾选就清空，查询走列相等而不是时间区间——SQL 里不出现日期与时区函数，`done_at` 永不可能是未来值，所以"生成中的周期截至此刻"这条对待办天然成立，不需要额外条件。

## 3. 传输与部署

监听 `0.0.0.0` 上单个业务端口，只接受 TLS（ADR-0009）。地址、端口值不入档（writing-rules §9）。

- **证书**：自签，ECDSA P-256。② 有效期定 **2 年**——一年一换要勤重装 APK，十年又怕泄露后换不掉；到期前 30 天服务端在日志里打提醒。换证书必须同步重装 APK，因为客户端锁定的是这张证书。
- **客户端校验**：APK 内置该证书并做校验，**代码里不留"忽略证书错误"的开关**。没有这条，中间人换一张证书客户端毫无察觉，等于没加密（ADR-0009 的付出的那一条）。
- **systemd**：`Restart=always`，配置文件与库文件路径通过 `EnvironmentFile` 给，不进命令行参数（避免出现在进程列表里）。
- **配置项**：库文件路径、监听端口、`local_tz`、口令哈希、DeepSeek 的 base URL / model / key、两个定时时刻、更新包目录（`MEMORA_RELEASE_DIR`，缺了更新接口一律 404）。key 只存在于服务端配置文件，需求方自己填，不进对话、不进文档。

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
| `GET /v1/periods/now` | 现在算哪个周期 | 返回 `week_start`/`week_end`/`month_start`/`month_end`（含端点）与 `now`、`local_tz`。存在的理由：归属算法只准有一处实现，客户端点"生成本周"要的 `period_start` 从这里拿，不在 Kotlin 里重算周一 |
| `GET /v1/reports` | 报表列表 | `kind`、`limit`；返回含 `status` |
| `GET /v1/reports/{kind}/{period_start}` | 取单份 | 未生成返回 404 + `code:"not_generated"` |
| `POST /v1/reports/{kind}/{period_start}/generate` | 手动生成 | 同步返回结果；失败也要落 `status=failed` |
| `GET /v1/tasks` | 待办全量 | 无分页：未完成按 `rank` 升序、已完成按 `done_at` 降序，一次返回 `{tasks, open_count, done_count}`。不分页的理由写在下面 §5.1 |
| `POST /v1/tasks` | 新建 | 请求 `{body, due_on?}`；`due_on` 缺省为服务端本地时区的今天，给了必须是 `YYYY-MM-DD`。响应含 `rank=1`、`done=false` 与 `due_on`——新建插到队首，其余顺延 |
| `PATCH /v1/tasks/{id}` | 改 | 接受 `{body}` 与/或 `{done}` 与/或 `{due_on}` 与/或 `{takeaway}`；只改 `{body}` 时不重编 rank、不碰完成态与归属列（ADR-0020）；`due_on` 不能清空，只换到另一个自然日；`takeaway` 按码点计数、超过 50 回 400，空串等于清掉（ADR-0027）；`done` 从 true 改回 false 时清空 `done_at` 并把这条排到未完成队尾，但不动 `takeaway` |
| `POST /v1/tasks/reorder` | 整列重排 | 请求 `{ids:[已完成拖拽的新顺序]}`，必须是**全部未完成 id 的一个排列**；服务端在一个事务里按数组下标重写 `rank=1..n`，返回重排后的列表。ids 有重复、有未知 id、漏了某条未完成 → 400 `bad_request`，不部分生效 |
| `DELETE /v1/tasks/{id}` | 删除 | 幂等，已删除返回 404；删的是未完成项时后面的 rank 自动收紧 |
| `GET /v1/books` | 书单全量 | 无分页：按 `finished_month` 倒序、同月新加的在前，一次返回 `{books, count}`；不做搜索，另加 1000 条安全上限（ADR-0029） |
| `POST /v1/books` | 新建 | 请求 `{title, author?, finished_month?}`；书名必填 ≤100 码点，作者 ≤100 码点；`finished_month` 缺省为服务端本地时区的当月，给了必须是带零填充的 `YYYY-MM`。响应含 `id` 与两个时间戳 |
| `PATCH /v1/books/{id}` | 改 | 接受 `{title}` / `{author}` / `{finished_month}` / `{takeaway}` 任意组合，只改发过来的字段；书名与年月不能清空，作者与心得传空串等于清掉；心得 ≤200 码点（ADR-0029）。字段全缺 → 400 |
| `DELETE /v1/books/{id}` | 删除 | 幂等，已删除返回 404 |
| `GET /v1/app/release` | 有没有新版可装 | 读 `<MEMORA_RELEASE_DIR>/release.json`，回 `{version_code, version_name, notes, size}`；`size` 由服务端 stat `app.apk` 得到，不是发布时手填的。没配目录或文件不在 → 404 `not_released`；清单在而包不在（发了一半）→ 500 `release_bad` |
| `GET /v1/app/release/file` | 拉 APK 本体 | `application/vnd.android.package-archive`，走 `http.ServeContent`，因此带 `Accept-Ranges`；这条响应单独把 HTTP/2 单流写期限放宽到十分钟，避免慢网在一分钟被 `RST_STREAM` 掐断（[ADR-0022](../decisions/0022-download-write-deadline.md)）。文件缺失同样 404 `not_released`。服务端支持续传，客户端目前断了从头重下——10 MB 量级下那段偏移状态机不值（见 [android-client.md](android-client.md) §边界与异常） |

#### 5.1 待办为什么一次给全量

待办的界面要"按优先级只露前 5、其余折叠"（FR-09），折叠区一点就要全看到——分两次拉会把"展开"这个动作变成一次网络等待。而待办天然会被勾掉或删除，长期停留在几十条量级（估算），远够不到 `notes` 那个 200 的上限。所以 `GET /v1/tasks` 一次返回全部，服务端另加一个 500 条的安全上限（估算，防的是库里攒了几年的极端情况），超出的已完成项不返回。

`reorder` 走整列提交而不是单条改 rank，是为了让"拖一次"只对应一个请求：单条改动在中途网络失败时会留下一个"客户端显示新顺序、服务端是旧顺序"的分叉，而整列提交失败后客户端本地回滚、再拉一次就和服务端一致了。

日历页也读这份全量列表、在客户端按 `due_on` 分天，因此切区块重拉一次就能看到另一页刚加的那条。按月的服务端过滤留到待办量真的超过 500 条安全上限时再做（ADR-0021 的复查条件）。

`body` 长度上限先按 2000 字符定（需求 §6 待定项之一，客户端和服务端同用一个常量配置）。超限返回 413 而不是静默截断。

安卓端还有一件事归客户端：进程被杀前把未提交的输入留一份草稿，这是需求 §5 第 3 条，服务端不管（ADR-0001 不做离线，不代表可以让用户白打一段字）。

#### 5.2 更新通道为什么也挂口令

这两个接口读的是文件系统而不是库，跟业务数据没关系，看起来可以开放。但包里钉着的自签证书 SAN 就是服务器地址（ADR-0009），任何人都能下载就等于给任何人一个反复探测这台机器的入口，而公网那台上唯一的防线就是口令（§4）。多花的代价只有一条：客户端必须先连上才能查更新——全新安装本来就是最新的，不构成死锁。理由与备选方案见 [ADR-0015](../decisions/0015-self-hosted-apk-release-channel.md)。

发版动作在 `scripts/release.sh`：版本号从 `android/app/build.gradle.kts` 里读，不让人再抄一遍（抄错就是一次装不上的更新）；包和清单先写 `<目录>.tmp.$$` 再整体换名，避免出现"清单已到位、包还没传完"那个半成品窗口——那种状态接口按设计回 500 `release_bad`。

## 6. 生成报表

1. 按周期取全部 `notes.body`，按 `created_at` 升序；另取该周期内**已完成**的 `tasks.body` 与 `takeaway`，按 `done_at` 升序。两路素材分开列进提示词，不混成一段——模型要分得清"我记下的想法"和"我勾掉的事项"，否则需求 §5 第 4 条那条"不许编造"没法归因。条数写进 `source_count`。
1.1 `source_count` 的口径由此变了：它是**喂给模型的素材条数**（记录数 + 本周期完成的待办数），不再是"该周期记录条数"。FR-08 的验收文案跟着改成"依据了多少条素材"。
1.2 空周期的判据仍是"两路加起来为 0"：本周一条记录都没记、但勾掉了三条待办，照样要能出报（那三条正是这周做完的事）。
1.3 `takeaway` 非空时跟在同一条待办后面，写成「（心得：…）」，提示词规则里说明它是完成后的补充；没有心得的待办只给标题，模型不得补一个结果（ADR-0027）。条数仍按待办算，心得不是独立素材。
1.4 `books` 不进任何一路素材：哪怕某本书的 `finished_month` 落在这个周期，也不喂给模型。周报月报是工作口径，读一本书不等于一份交付（ADR-0029）。空周期判据也不考虑书。
2. 调 DeepSeek 的对话补全接口（OpenAI 兼容），`model` 走配置。提示词要点：只用给到的内容、不编造未提到的交付物（需求 §5 第 4 条，含给没写心得的待办编一个结果）、按主题归并而非逐日流水、输出纯文本。**两类素材分段给出且标明语义**——勾掉的待办就是已交付，想法只是当时的念头；混成一段模型就分不开这两件事，"不许编造"那条也就没了核查抓手。
3. 超时与重试：单次调用超时给到 60s（FR-04 的量级），失败重试 1 次；仍失败则 `status=failed` + `fail_reason`。
4. 空周期（记录与已完成待办两路都为 0）不生成，直接返回 `code:"no_records"`——避免让模型憋出一份空话。

## 7. 定时

**不用 crontab 精确触发，用进程内 ticker 每小时检查一次"哪些已结束的周期还没有 ready 的报表"，缺就补**（ADR-0006 的进程内调度）。

这个形状顺手把 FR-05 的"服务中断错过时刻要能补、不静默缺失"变成自然结果：判断依据是"该周期有没有 ready 报表"，而不是"现在是不是周一早上八点"。机器关机两天再起来，漏掉的周期会被自动补上。代价是每小时空转一次查询，可忽略。

具体两个时刻（周报、月报各自周几几点）仍待需求方定，配置项留好。

候选周期来自两路并集去重：`notes` 的 `DISTINCT week_start` 与 `tasks` 的 `DISTINCT done_week_start`。只靠前者会漏掉"这周一条笔记没记、但勾掉三件事"的那一周，而那一周恰恰最需要自动出报（FR-05 的不静默缺失对两类素材同等成立）。

一轮扫描里每种报表最多补最近六份，其余留到下一轮。理由是：关机几个月后重启，候选可能有几十周，一次全打完会把重启后的头几分钟全占在排队等模型上（推断，未实测）。候选按周期由新到旧排，所以先拿到的是最近的。

## 8. 备份与恢复

- **每日快照**：`serve` 启动时与之后每小时检查一次，距最新一份满 24 小时就用 `VACUUM INTO` 导出一份到 `MEMORA_BACKUP_DIR`，文件名 `memora-<UTC时间戳>.db`，只保留最近 `MEMORA_BACKUP_KEEP` 份（默认 5）。WAL 下运行中不能直接复制库文件（会漏 `-wal` 里的提交），理由与备选方案见 [ADR-0026](../decisions/0026-manual-offsite-pull.md)（服务器部分承自 ADR-0025）。
- **盘外副本**：手动双击本机入口脚本执行 `deploy/pull-backups.ps1`，scp 拉回本机，本机保留最近 5 份；不注册计划任务（ADR-0026），忘拉就落后。
- **恢复**：停 `memora.service`，把选定的快照覆盖回 `MEMORA_DB_PATH`，删掉同目录残留的 `-wal`/`-shm`，再起服务；恢复后核对记录与报表达数。
- **不进自动快照**：`/etc/memora/memora.env`（口令哈希、模型 key）与 TLS 私钥不每天复制，装机时各另存一份到本机；证书重签会让已装 APK 的证书固定失效（ADR-0009），私钥丢了要连带重发 APK。

## 9. 本设计不包含

安卓端界面细节与包结构在 [android-client.md](android-client.md)（接口定完后已单独成篇）。
