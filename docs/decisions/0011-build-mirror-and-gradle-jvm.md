# ADR-0011：安卓构建走国内镜像，Gradle JVM 锁在 17

> 状态：生效中
> 摘要：Gradle 本体从华为云镜像取、依赖库阿里云两个仓库排在官方前面；构建 JVM 固定为本机的 JDK 17，不用 AS 自带的 JBR 25
> 日期：2026-11-26 ｜ 关联：[0010](0010-minimal-android-deps.md)、[../design/android-client.md](../design/android-client.md)、提交 `a5b546c` `4a056fd`

## 决策

三件事，都只留实测有效的那一个：

1. Gradle 本体（130 MiB 量级）从华为云的 `gradle-8.11-bin.zip` 取，官方地址不进配置文件，只在需要时手动换回。换之前比对 sha256，与官方一致才用。
2. 依赖仓库只加阿里云 `google` 和 `public` 两个，排在 `google()` / `mavenCentral()` 前面兜底。不加 `gradle-plugin` 那个仓库。
3. 构建 JVM 写死在本机已有的 JDK 17 上，位置是**用户级** `~/.gradle/gradle.properties` 的 `org.gradle.java.home`。不写进项目的 `gradle.properties`，也不依赖 Android Studio 的 Gradle JDK 下拉框。

## 背景

需求方在自己 Windows 上第一次 sync，报 `Could not install Gradle distribution ... SocketTimeoutException: Connect timed out`。当时以为就是"境内连不上 `services.gradle.org`"，于是先换成腾讯云镜像，仍然失败，只是错误变成 `zip END header not found`。

实测把两件事分开：官方地址会 307 跳到 GitHub Releases，跳转后能连通但只有 60KB/s 量级，130 MiB 要跑半个多小时——是**慢**，不是不通。腾讯云那个 CDN 有反爬：同一个 URL，curl 拿到真 zip，Java 的 `HttpURLConnection` 拿到 989 字节的 JS 挑战页，而 wrapper 只用后者下载，于是解包失败，还在 `~/.gradle/wrapper/dists/` 下留下坏文件，改地址也不会重新下载。

同期另一件事与网络无关：Android Studio Rabbit 2 自带 JBR 25，Gradle 8.11 支持上限是 23，sync 会失败，而报错正文只有版本号一行 `25.0.4`，看不出是 JVM 太新。

## 备选方案

| 方案 | 主要代价 | 结论 |
| --- | --- | --- |
| **Gradle 本体走华为云镜像** | 依赖第三方镜像的可用性；换 Gradle 版本时要重新比一次哈希 | **采纳**：实测 5.8 MB/s，sha256 与官方逐字节一致（`57dafb5c…f1e9`） |
| 官方 `services.gradle.org` | 307 跳 GitHub Releases，实测 60KB/s，130 MiB 半个多小时 | 否决：不是不通而是太慢，wrapper 等不到；能直连的机器上仍可用，所以留在注释里而不是配置文件里 |
| 腾讯云镜像 | 看着同样快（curl 5.8 MB/s），但反爬 | 否决：对 Java 客户端返回 JS 挑战页，wrapper 解包报 `zip END header not found`；失败模式比超时更难查，因为它伪装成"下载成功" |
| 手动下 zip 塞进 `~/.gradle/wrapper/dists/<哈希>/` | 哈希目录名由 URL 算出来，写错就不生效；换机器要重来 | 否决：一次性绕过，不进仓库，别人 clone 还是失败 |
| 调大 `networkTimeout` | 从 10s 调到 30s | 否决：60KB/s × 30s 只有 1.8 MiB，照样下不完。真正卡的是吞吐不是握手，这条是当时按"连接超时"的字面猜的，没起作用，已删回默认值 |
| 依赖仓库加阿里云 `gradle-plugin` | 每次解析多试一个仓库 | 否决：它能取到的四个 Kotlin 插件标记，`public` 全都能取到，而它在 AGP 标记上 404——纯冗余 |
| 只留官方 `google()` / `mavenCentral()` | 境内不稳 | 否决：但保留在后面兜底，Gradle 找不到才往下试，能直连的机器行为不变 |
| Gradle JVM 用 AS 自带 JBR 25 | — | 否决：超出 Gradle 8.11 的 23 上限 |
| Gradle JVM 用 JDK 21 | 要额外下载 | 否决：本机没有 21。之前记的"得选 JVM 21"是记错了，17~23 都行，本机已有的 17 与 `jvmTarget = 17` 正好对齐 |
| Gradle JVM 用本机那个 `openjdk-24` | — | 否决：同样超上限 |
| 写 `local.properties` 的 `java.home` | AS 的 Gradle JDK 下拉框读的就是这条 | 否决（准确说是**没验成**）：这条只管 IDE，命令行 `gradlew` 不认；而 `org.gradle.java.home` 两条路都管住，留一个就够。没有 AS 图形界面可以佐证它生效，不留未验证的配置 |
| 写进项目的 `android/gradle.properties` | 会提交 | 否决：里面是绝对路径，公开仓库不能带（writing-rules §9） |

## 影响

- **得到**：`git clone` → 打开 AS → sync，一次通过；命令行与 IDE 走同一个 JVM，不再出现"IDE 能编、`gradlew` 不能编"。
- **付出**：构建环境依赖第三方镜像的可用性；`org.gradle.java.home` 是**机器级**配置，换一台机器要重新写一次，仓库里带不过去。
- **返工点**：升 Gradle 版本时改 `distributionUrl` 一处，并重新比对哈希；换 JDK 时改用户级 `gradle.properties` 一处。
- **连带决定**：`~/.gradle/wrapper/dists/` 里的坏缓存必须手工删掉，改地址不会自动重下；这条写进 `android/README.md` 的排障段。

## 复查条件

出现以下情况时重新评估本决策：

- Gradle 版本升级后镜像站没有对应版本，或哈希与官方不一致。
- 换到能直连 GitHub Releases 的开发环境（那时官方地址更快，镜像行可以整段删掉）。
- Android Studio 升级到自带 JBR 在 23 及以下的版本，或 Gradle 升到支持 JBR 25 的版本——那时 `org.gradle.java.home` 这条可以撤掉，回到"用 IDE 自带的"。
- 依赖解析出现阿里云没有、官方才有的构件。

## 替代成本参考

境内构建走镜像是常规做法，Gradle 官方也承认分发量大而建议就近取。值得记一笔的是失败模式的差别：**按客户端类型返回不同内容的 CDN，比单纯连不上更坑**——curl 一切正常、Gradle 拿到 HTML，而 wrapper 只报"不是合法 zip"。所以以后换镜像站，判据不是"curl 能不能下"，而是"用同一个 JDK 的 `HttpURLConnection` 发一次请求，头四个字节是不是 `PK\x03\x04`"，再比 sha256。
