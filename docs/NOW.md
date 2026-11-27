# NOW

> 状态：生效中
> 摘要：当前进度快照与断点，新会话从这里接上
> 更新：2026-11-28 00:31

这份文件只描述**现在**。已完成的历史进 ADR 或对应设计文档，卡了两周以上的事项应该转成 ADR 或 refactor 方案，不要赖在这里。上限 60 行。

## 在做什么

- **读书区块与左侧抽屉已实现并发布 v0.9（2026-11-28 00:30）**：ADR-0028 把主屏五区块收进左侧抽屉（打开仍落待办，返回键先合抽屉，状态按区块名保留）；ADR-0029 新增 `books` 表、服务端四个接口、安卓读书屏与书编辑页——书名/作者/读完年月/≤200 字心得，按年分组，心得可清空，读书不进周报月报。
- **验证**：`go vet ./...`、`gofmt -l .` 无输出、`go test -race ./...` 全过；`./gradlew :app:assembleLocalDebug` 成功；模拟器上抽屉五区块切换、加书默认当月、年月选择器跨年、补作者/心得、两击删除、空态恢复都点过，`/v1/books` 已清回空。
- **v0.9 已发布（2026-11-28 00:30）**：云机二进制换成含 `books` 的 v0.9（旧版备份 `/usr/local/bin/memora.bak-20261128-002947`，启动后 `books` 表已建，`/healthz` 200）；更新目录已上传 `app-server-release.apk`（7,659,508 字节）与 `release.json`（versionCode 9），云端与本机 sha256 一致（`6994f26530cad4c7`），签名证书 CN=memora。
- **远端是需求方的公开 GitHub 仓库**，凭据是仓库专用 deploy key（私钥只在 `~/.ssh`，600）；不强推、不改写历史。

## 卡在哪

- 不卡编码。公网 8443/TCP 安全组已放行，证书 SAN 是公网 IP（`notAfter` 2028-11-25），口令已用真值验过。
- `MEMORA_LLM_API_KEY` 仍为空，真模型报表没跑过，生成时服务端会 WARN「模型未配置」。
- 手机真机还没验 v0.9 更新链路：提醒 → 更新页 notes → 下载 → 系统覆盖安装 → 一次性「刚装好」，也还没用真手指试读书页与待办心得。
- 模拟器侧前提：本机 WHPX 可用，AVD `Medium_Phone_API_37.0` 完整（x86_64 / API 37），不用新建。
- `android/app/src/local/res/raw/memora_server_cert.pem` 已还原成仓库里那张联调证书，工作树没有证书改动；`devlocal/` 与测试截图不提交。

## 下一步

1. 手机真机验 v0.9 更新链路：提醒 → 下载 → 系统覆盖安装 → 「刚装好」一次性提示；装上后读书页能加/改/删，顺带补待办心得。**验证**：真手指走完整条，书单换设备可见。
2. 恢复演练一次：停服 → 快照覆盖回 `MEMORA_DB_PATH` → 删同目录 `-wal`/`-shm` → 起服。**验证**：记录、报表与书单条数和原库一致。
3. 真模型生成一次报表（先填 `MEMORA_LLM_API_KEY`）。**验证**：正文读得通，心得被采信、没心得的待办没有编造结果，书单不出现。

## 断点说明

没有"改到一半没验证"的代码。已知粗糙：auth 与 config 没有自己的单测（靠 api 单测和冒烟间接兜住）；待办拖拽只以 `input swipe` 在模拟器验过，真手指的抖动没测；日历每次进屏拉全量、无缓存；跨时区旅行时手机本地日期与服务端缺省日期可能差一天（ADR-0021 的取舍）；日期选择器是 Material 组件，与案头视觉只靠颜色对齐。
**验证口径**：云机内部连自己的公网地址不通（hairpin 不成），防火墙只能由手机或 Windows 侧验；bcrypt 哈希过 systemd `EnvironmentFile=` 不变形；出网实测 `api.deepseek.com` 可达（无 key 回 401）。
**备份链路已通、恢复没演练**：云机快照已产出、本机手动拉取哈希一致；恢复步骤没实跑过（下一步 2）；盘外副本手动拉取（ADR-0026），忘拉就落后。
**云机二进制 = 本地构建的 v0.9**：与源码同源，`books` 迁移已生效；更新目录也已是 v0.9。回滚用 `/usr/local/bin/memora.bak-20261128-002947`。
**本机联调回路留在盘上**：本机 `memora.exe` 听着本地端口，`devlocal/`、`tls-test/` 都在；devlocal 库里留着交周报/买牛奶两条测试待办（都带心得），会进本机报表素材。
**未验**：书单在第二台真机上的同步；带书的一次真实生成——按取数路径书不会进素材（`design/memo-and-report.md` §6）。

## 本轮改动过哪些文档

- 新建 `decisions/0028-left-drawer-navigation.md`、`0029-reading-list.md`；`INDEX.md` 收录。
- `projectneed.md`：FR-15 读书记录、FR-16 读书心得，业务口径补「读过的书 / 读完月份 / 读书心得」，否决清单补两条。
- `design/memo-and-report.md`：`books` 表与四个接口，§6 明确书不进报表素材。
- `design/android-client.md`：九屏、抽屉五区块定论、读书屏六条定论、视觉/边界/验收同步。
- 代码：`internal/store/books.go`、`internal/api/books.go` 及两组测试、`store.go` 迁移、`server.go` 路由；安卓 `App.kt` 抽屉、`BooksScreen.kt`、`Models.kt`、`MemoApi.kt`、`Format.kt`、`Components.kt`。

## 待确认（会影响下一步的）

- **`deploy/memora.service` 写 `Restart=on-failure`，设计文档 §3 写 `always`**：先定哪个是要的。
- `MEMORA_WEEKLY_AT` / `MEMORA_MONTHLY_AT` 已留但调度不读：删掉还是想清楚用途。
- 模型档位与 key（需求方自己填）。
- **视觉方向「编辑部案头」是否保留**（ADR-0013）：返工集中在 `Theme.kt` + `Components.kt`。
- **公网地址是否固定**：轻量服务器无弹性 IP，换主机要重签证书 + 重装 APK（ADR-0009 复查条件）。
- **公开仓库历史不清理（需求方已定）**：不 force push、不改写历史。