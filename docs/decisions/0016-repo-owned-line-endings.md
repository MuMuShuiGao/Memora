# ADR-0016：行尾规则写进仓库，不留在各人机器的配置里

> 状态：生效中
> 摘要：仓库根加一份 `.gitattributes`（文本一律 LF 入库、签出成 LF，`*.bat`/`*.cmd` 例外留 CRLF，二进制不转换），行尾由仓库定死
> 日期：2026-11-26 ｜ 关联：`.gitattributes`、`android/README.md`

## 决策

`.gitattributes` 是仓库里唯一说行尾的地方：`* text=auto eol=lf`，`*.bat` 与 `*.cmd` 单独指成 `eol=crlf`，`*.jar` 与 `*.png` 标 `binary`。任何人、任何机器上的 `core.autocrlf` 是什么值，都不再影响签出来的字节和 `git status` 报什么。

## 背景

需求方这台 Windows 上 `core.autocrlf=true`（机器级配置）。库里 105 个文本 blob 全是 LF，签出时 Git 会把它们刷成 CRLF；而 `gofmt`、编辑器和 `sed` 这类工具写回文件时产出的是 LF。于是 11 个 `.go` 长期挂在 `git status` 里报 `M`——`git diff` 输出 0 字节、`git ls-files --eol` 显示 `i/lf w/lf`，纯粹是 Git 按"应该长这样"的 CRLF 大小去比对的结果。这堆假 `M` 已经撞到过一次实际损失：在 VS Code 的提交图里想签出一个旧提交看代码，Git 用"在签出前，请清理仓库工作树"拦下来，而根本没什么可清理的。

## 备选方案

| 方案 | 主要代价 | 结论 |
| --- | --- | --- |
| 仓库里放一份 `.gitattributes` | 它是共享文件，别人 clone 后第一次签出会重写一遍行尾（一次性） | **采纳** |
| 每台机器自己把 `core.autocrlf` 关掉 | 配置带不进仓库：换机、别人 clone、云机、CI 都要重说一遍，漏一处噪声就回来；且不改别人的 git 配置是项目硬规则 | 否决：一个只能靠口头传递的修复等于没修 |
| 每次冒出来就 `git checkout -- .`，或者习惯它 | 噪声一行都治不了，`gofmt` 每跑一次就回来一轮；习惯之后真改动和假改动混在同一列里，`git add` 时容易一起带走 | 否决：把辨别成本长期压在每次提交上 |
| 对噪声文件 `git update-index --skip-worktree` / `--assume-unchanged` | 这两个标记只作用在本机索引、同样带不进仓库，而且会把以后**真的**代码改动一起藏起来——`git status` 和 `git add` 都看不见，是最坏的一种眼不见为净 | 否决：会咬人 |
| 反过来全局 `eol=crlf` | `scripts/smoke.sh`、`scripts/release.sh`、`deploy/mkcert.sh`、`Makefile`、`deploy/memora.service` 都在 Linux 上跑，带 CRLF 的脚本和 unit 文件是新的坑 | 否决：为了修 Windows 的显示问题给 Linux 侧埋雷 |

## 影响

- **得到**：`git status` 只报真改动；fresh clone 在 Windows 与 Linux 上字节一致；`.sh` 与 systemd unit 保证是 LF，不会出现"在 Windows 上编辑过一次就跑不起来"。
- **付出**：多一个仓库根文件要维护；`*.bat` 那条例外是反直觉的，得靠注释和这条 ADR 说明为什么。
- **返工点**：想退回"各人自己配"，删掉 `.gitattributes` 即可，代价是那 11 个 `.go` 的假 `M` 立刻回来。
- **连带决定**：`android/app/src/local/res/raw/memora_server_cert.pem` 被本机重签的证书覆盖时**仍然要显示成 `M`并留在工作树里**（README 明写它是本机产物、不许提交）——那种"看得见的脏"是有意的，别用 `skip-worktree` 去盖。

## 复查条件

出现以下情况时重新评估本决策：

- `git ls-files --eol` 里除了 `gradle-wrapper.jar` 还出现 `i/-text` 以外的非 `i/lf` 条目，说明有 CRLF blob 混进库了，需要再跑一次 `git add --renormalize .`。
- 引入要求 CRLF 的构建工具或第三方生成物（例如某个只能吃 CRLF 的代码生成器），例外清单要跟着扩。

## 替代成本参考

Go 项目社区惯例就是这份文件本身：`* text=auto eol=lf` 加批处理例外。Kubernetes、Go 标准库仓库里都能找到同形写法，没有一家靠 README 里写"请把 autocrlf 关掉"来解决问题。
