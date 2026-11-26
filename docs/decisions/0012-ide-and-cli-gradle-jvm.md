# ADR-0012：IDE 与命令行的 Gradle JVM 是两套入口，各配一处

> 状态：生效中
> 摘要：AS 里必须把 Gradle JDK 显式选成 17（落在 `.idea/gradle.xml`），命令行靠用户级 `org.gradle.java.home`；`local.properties` 的 `java.home` 实测没被 AS 采纳，已撤
> 日期：2026-11-26 ｜ 关联：[0011](0011-build-mirror-and-gradle-jvm.md)、[../design/android-client.md](../design/android-client.md)

## 决策

构建 JVM 固定在 17~23 区间不变（0011），但 IDE 和命令行是**两套独立入口，各要配一次**：

1. **Android Studio**：在 Settings → Build Tools → Gradle → **Gradle JDK** 里显式选 `ms-17.0.14`
   （报错弹窗上的 `Open JVM settings` 是同一个地方）。选择落在 `.idea/gradle.xml` 的 `gradleJvm`，
   本机现在的值是 `ms-17`。`.idea/` 不进仓库，所以**每个新 clone、每次删掉 `.idea/` 都要手点这一遍**——
   这件事必须写在 `android/README.md` 的第一次 sync 一节里，不能假装它已被配置解决。
2. **命令行**：用户级 `~/.gradle/gradle.properties` 的 `org.gradle.java.home=<17 的路径>`。

本机 `~/.jdks/` 下只有 17 落在区间内：`openjdk-24` 与 AS 自带 JBR 25 一样超 Gradle 8.11 的 23 上限；
列表里那个 Oracle `1.8.0_351` 虽然在区间内，但 AGP 8.7 要求 17，选它只是把报错推到下一环。
AS 弹窗推荐的 `Use JVM 21` 也别点——本机没有 21，点了是让它去下载一份。

## 背景

0011 把 JVM 问题判成"已解决"：写了 `org.gradle.java.home`，用"启动时故意设 `JAVA_HOME` 为 JBR 25、
daemon 仍起在 17"证明它生效，于是把 `local.properties` 的 `java.home` 当冗余删掉，还在 README 写下
"前者会盖掉 IDE 选的 Gradle JDK"。需求方随后在 AS 里点 sync，报
`Incompatible Gradle JVM version`，**543 ms 就失败**。

第一版这条 ADR 的解释是"AS 的 sync 预检读 `local.properties` 的 `java.home`，那行被删所以回落到 JBR 25"。
**两处都不成立**，按实测更正如下，保留错误版本是为了让下一次排查别再走同样的路。

**机制不是预检。** `idea.log` 里这次失败的堆栈顶是
`BaseProjectImportErrorHandler.doGetUserFriendlyError` ← `GradleExecutionHelper.execute` ← Tooling API，
即 IDE 真的发起了构建，是 Gradle 拒绝后由 IDE 翻译成友好文案。JVM 由 IDE **显式传过去**：
`GradleInstallationManager.getGradleJvmPath` 解析 `gradleJvm`，经 `GradleExecutionSettings.setJavaHome`
交给 Tooling API。所以 `org.gradle.java.home` 在这条路上拿不到决定权——不是"还没轮到读它"，
而是 IDE 已经替它做了选择。

**`java.home` 那条没被采纳。** 写回 17 并用 `java.util.Properties` 的转义规则实测还原出正确目录之后，
AS 重启（新进程）再打开 Gradle 设置，下拉框显示的仍是
`GRADLE_LOCAL_JAVA_HOME  JetBrains Runtime 25.0.4  D:\Android Studio\jbr`——
宏解析出来的还是 JBR，不是我写的那个 17。jar 里确实有
`GradleJvmGradleLocalJavaHomeResolver` 和常量 `GRADLE_LOCAL_JAVA_HOME_PROPERTY = "java.home"`，
但**在这台机器上它没有把那个值交出来**，原因没查出来（可能是解析结果被缓存，也可能这个宏读的不是
项目根下那份 `local.properties`）。真正让 sync 过的是把 Gradle JDK 显式选成 `ms-17`，
即绕开宏。12:03 的 `onSuccess(RESOLVE_PROJECT)` 是这一条的实测凭据。

**A/B 实验证明的是 JVM 值本身，不是配置位置。** 用本机 Gradle 8.11 的 tooling-api，以 IDE 同款方式
`newBuild().setJavaHome(...)` 发起真实构建：传 JBR 25 → `IllegalArgumentException: 25.0.4`（3.8 s），
正是 0011 里"报错正文只有一行版本号"的那个错；传 17 的路径 → 构建跑通。
所以这个实验能证明"17 这个值对"，**不能**证明"IDE 会从我写的那行读到 17"。当时我把前者当成了后者。

## 备选方案

| 方案 | 主要代价 | 结论 |
| --- | --- | --- |
| **IDE 用 Gradle JDK 下拉框选 17 + 命令行用 `org.gradle.java.home`** | 两个入口各配一次，且 IDE 那次落在不进仓库的 `.idea/`，每次新 clone 都要重点 | **采纳**：AS 里 sync 实测 `onSuccess`，命令行实测 daemon 起在 17 |
| 只写 `org.gradle.java.home` | 配置最少 | 否决：AS 里 sync 报 `Incompatible Gradle JVM version`——IDE 把自己解析的 JVM 显式交给 Tooling API，这份配置拿不到决定权 |
| 只写 `local.properties` 的 `java.home` | 看着最贴近 IDE | 否决（实测）：写回 17 后 AS 重启仍解析出 JBR 25，这个值没被采纳；已删掉这行，不留未生效的配置 |
| 让 `gradleJvm` 继续用 `#GRADLE_LOCAL_JAVA_HOME` 宏 | 不用改 IDE 设置 | 否决：宏在本机解析不出 17，等于把成败押在一条没跑通的路径上；显式选 `ms-17` 后不再依赖它 |
| 点弹窗的 `Use JVM 21` | 一键 | 否决：本机没有 21，点了是触发下载一份新 JDK，而 17 已经在 `~/.jdks/` 下 |
| 选列表里那个 Oracle `1.8.0_351` | 也在 8~23 区间内 | 否决：AGP 8.7 要求 17，报错只会推到下一环 |
| 升 Gradle 到支持 25 的版本，两处都不用配 | 要动 AGP / Kotlin 版本组合 | 否决：为绕开一个本机配置去动整套工具链版本，代价不成比例；作为将来撤掉配置的自然时机记在复查条件里 |

## 影响

- **得到**：AS 里 sync 首次通过（12:03 `onSuccess`），命令行与 IDE 各有一条已验证的配置，报错文案与
  底层异常能对上号。
- **付出**：IDE 那次配置落在 `.idea/gradle.xml`，被 `.gitignore` 挡住，**仓库带不过去**——别人 clone
  下来第一次 sync 仍会撞上同一个错，除非照 README 手点一遍。这是本决定已知且接受的成本。
- **返工点**：`~/.jdks/` 下换成别的 17~23 JDK 时，要改 IDE 下拉框 + 用户级 `gradle.properties` 两处。
- **连带决定**：`android/README.md` 的排障段改成"两种入口两种报错"的对照表，并把"第一次必须手点
  Gradle JDK"写成显式步骤；0011 头部加更正行指向本条，正文按规则不动。
- **方法教训（两条）**：一、判一个配置"无效、可以删"之前要列出它影响的所有入口逐一验证，
  **删除同样是需要验证的决定**；二、"值是对的"和"IDE 会读到这个值"是两个命题，
  用前者的实验去宣布后者，就会像这一轮一样把没生效的配置写进文档。

## 复查条件

- Gradle 升到支持 JBR 25，或 AS 自带 JBR 回落到 23 及以下：两处配置一起撤掉。
- 查清 `#GRADLE_LOCAL_JAVA_HOME` 到底读哪份文件——若确实能读 `local.properties`，可以把 IDE 那条
  换成可照抄的文本配置，省掉每次手点。
- 换开发机器：IDE 下拉框与用户级 `gradle.properties` 都要在新机器上重配，仓库带不过去。

## 替代成本参考

JetBrains 系 IDE 对 Gradle JVM 有两层设计：`gradleJvm` 决定 IDE 拿哪个 JVM 去连 Gradle（经 Tooling API
显式传入），`org.gradle.java.home` 是 Gradle 自己的 daemon 选择。命令行下后者说了算，IDE 下前者说了算。

排这类问题**不要靠失败耗时猜位置**：543 ms 的失败曾被我读成"IDE 预检、daemon 没起"，堆栈显示
Tooling API 已经发起构建。耗时只说明构建没真正跑起来，区分不了谁拒绝；看 `idea.log` 的堆栈顶——
`BaseProjectImportErrorHandler` 意味着错误来自 Gradle 侧、由 IDE 翻译。

同理，**不要用一两个采样窗口判断"有没有在下载"**：本次 import 期间我测到"20 秒依赖缓存零新增"，
就下结论说没在下载；整个 import 结束后再数，缓存从 697 个文件涨到 930 个，AS 自己也报了
"taking a significant amount of time to download dependencies"。瞬时静止不等于没有网络活动，
要前后总量对比才算数。
