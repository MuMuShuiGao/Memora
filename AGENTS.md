# 项目说明

> 状态：生效中
> 摘要：新会话开工前必读——先读 docs/，再动代码

## 动代码之前，按顺序读这三份

1. [`docs/NOW.md`](docs/NOW.md) — 它写的就是当前事实，包括"改到一半、没验证"。**不要**凭代码或印象推测项目进度。
2. [`docs/INDEX.md`](docs/INDEX.md) — 定位与本次任务相关的 ADR 和设计文档。
3. 相关的 `docs/decisions/NNNN-*.md` — 不要跳过 ADR 直接改设计，备选方案表里写了哪些路已经否掉过。

读完先回一句：在做什么 + 下一步 + 你看到的与 NOW.md 不一致之处。不一致就先确认，别直接改。

## 例外：项目还没起步

若 `docs/NOW.md` 里还是 `<尖括号占位>`，说明这是刚拷进来的空模板，上面那套"复述断点"不适用——没有断点可复述。此时先问清要做什么，填 `docs/projectneed.md` 和 `docs/NOW.md`，手上已有选型结论就顺手建第一条 `docs/decisions/0001-*.md`，填完才动代码。

## 四条硬规则

| 情况 | 动作 |
| --- | --- |
| 有方案被否掉 | 当场用 `docs/templates/adr.md` 建一条 ADR，备选方案与否决理由不能省 |
| 要推翻已生效的 ADR | 新开一条，旧的只把状态行改成 `已废弃（被 NNNN 取代）`，正文不动 |
| 写 `docs/projectneed.md` | 只写需求层。技术栈、表结构、接口契约、排期都不进——架构约束剥掉实现名翻译成产品语言 |
| 任务结束、会话要断 | **无条件更新 `docs/NOW.md`**，含 `> 更新：` 时间 |

## 提交规范（Conventional Commits，写死，不许自行简化）

标题一律 `<type>(<scope>): <subject>`。type 与 scope 用小写英文，subject 用中文说清做了什么。

| type | 什么时候用 |
| --- | --- |
| `feat` | 新增对外可见的功能 |
| `fix` | 修 bug |
| `docs` | 只动文档（`docs/**`，含根目录 `AGENTS.md` / `README.md`）或代码注释，不改运行行为 |
| `refactor` | 不改外部行为的结构调整 |
| `perf` | 只为更快或更省 |
| `test` | 只动测试 |
| `build` | 构建系统、依赖、打包（`Makefile` / `go.mod` / `Gradle` / `deploy/`） |
| `ci` | 持续集成配置 |
| `chore` | 以上都不是 |
| `revert` | 回滚某个提交 |

- scope 取模块名：`api` `store` `auth` `config` `period` `report` `android` `docs`——其中 `docs` 作 scope 只指 `docs/` 里的内容，根目录文档不算。跨多模块或纯仓库级就省略括号，但省略的只是括号，type 仍照上表判，不等于自动落 `chore`。
- 标题 ≤ 50 字符，句末不加句号。禁止 `update`、`修改`、`临时`、`WIP`、`fix bug` 这类空词——写了等于没写。
- 破坏性变更在 type 后加 `!`（`feat(api)!: ...`），并在正文末尾单独一行写 `BREAKING CHANGE: <改了什么、调用方怎么迁移>`。
- 正文：标题空一行后写**为什么**，不复述 diff；每行 ≤ 72 字符。改的动机、非显然的取舍、踩过的坑在这里，不在代码注释里。
- 正文最后一行必须是验证口径：`验证：make vet && make test 全过（实测）`。没验证就照实写 `验证：未编译（本机无 SDK）`——藏起来比写出来更糟。
- footer 各占一行，可选：`ADR: 0010`（该提交依据的决策编号）、`Fixes: #12`、`Co-authored-by: 名字 <邮箱>`。

例：

```
build(android): Gradle 与依赖仓库改走镜像优先

需求方第一次 sync 报 SocketTimeoutException，是 services.gradle.org 连不通，
不是工程问题。Gradle 本体指向腾讯云镜像，依赖库阿里云排在官方之前——找不到
就落到下一个仓库，能直连官方的环境行为不变。

验证：两边都取到过 AGP 8.7.3 / Kotlin 2.0.21 构件（实测）。
```

一个提交 = 一个能自证完整的改动：

- 只 `git add <具体路径>`，不用 `git add -A` / `git add .`——公开仓库里误提交的代价由别人替你付。提交前跑一次 `git status` 看清进了什么。
- 编译不过、测试不过、`TODO` 没交代，不提交。文档更新可以和代码同提交，但不许留到"以后一起补"。
- 半成品不要提交，也不要在共享工作树里裸 `git stash`。

历史卫生（硬规则）：

- **已推送的提交不 amend、不 rebase、不 force push**。远端是需求方的公开仓库，这条已由需求方定死。
- 拉取用 `git pull --rebase`，保持线性历史；rebase 只允许发生在本地未推送的提交上。
- 不许 `--no-verify` 绕过钩子。钩子报错就修根因，不关掉它。
- 不建空提交凑数，不把密钥、`.db`、`*.pem`、`.env` 提交进去——文件名看着人畜无害也要点开看一眼。

适用范围：**从下一个提交起**。已有的 8 个提交（`Initial commit` … `a5b546c`）保持原样，不改写——把它们套上 type 前缀要重写全部历史并 force push 公开仓库。

完整流程见 [`docs/WORKFLOW.md`](docs/WORKFLOW.md)，书写约束见 [`docs/writing-rules.md`](docs/writing-rules.md)。
