# 安卓客户端

原生 Kotlin + Compose（ADR-0008），依赖只有 OkHttp 和 kotlinx.serialization（ADR-0010）。
界面划分、错误文案、本地存哪四样，都在 [docs/design/android-client.md](../docs/design/android-client.md)。

**已在 Windows 上编过并跑通**：`localDebug` 全绿，APK 实测 9.7 MiB，`res/raw/memora_server_cert.pem` 在包里；
模拟器里记录增删改查、搜索、超限拦截、报表生成与复制全文都过了（服务端日志与界面双向对上）。
`serverDebug` 停在`raw/memora_server_cert not found`——这是有意的编译期失败，不是坏了。

## 两个 flavor

`env` 维度下只有两个构建变体，差别就两样：默认地址，和打进包的那张证书。

| 变体 | 默认地址 | 证书 | 用途 |
| --- | --- | --- | --- |
| `local` | `https://10.0.2.2:8443` | **仓库里已有** `app/src/local/res/raw/memora_server_cert.pem` | 模拟器连本机 `memora.exe` |
| `server` | 空（必须手填） | 要自己放 `app/src/server/res/raw/memora_server_cert.pem` | 装到手机上连公网服务器 |

`server` 的默认地址是空串，公网 IP 既不进仓库也不进 APK。

`local` 那张证书只覆盖模拟器别名与回环地址（`10.0.2.2`、`127.0.0.1`、`localhost`），不含任何机器标识，
所以进仓库，换你 clone 下来就能编译；**对应私钥在这台云机上**（`/tmp` 下，不在仓库）。
要换一张或重新签：`MEMORA_PUBLIC_IP=10.0.2.2,127.0.0.1,DNS:localhost ./deploy/mkcert.sh <临时目录>`，
把产出的 `server.crt` 改名成 `memora_server_cert.pem` 覆盖进去，同时 Windows 上的服务端要改用新证书。

`server` 那张仍然只能手拷——它的 SAN 是你服务器的实际地址，那个值不进仓库（writing-rules §9）。

证书文件名必须是 `memora_server_cert.pem`——网络安全配置里写的是 `@raw/memora_server_cert`，
资源名就是文件名。**没放证书是编译期失败，不是运行期失败**，这是有意的：
装上一个连不上自己服务器的包，比编译不过难查得多。

## 第一次 sync 失败

三种失败模式，报错长得不一样，都好认。仓库里已经把该配的配上了，只有 JVM 那两条得你在机器上配，
其中 **IDE 那条只能手点、仓库带不过来**。

**`Could not install Gradle distribution ... Connect timed out`**——Gradle 本体下不动。官方地址会 307 跳到
GitHub Releases，实测只有 60KB/s 量级，130 MiB 要跑半个多小时。仓库里已改走华为云镜像
（`gradle/wrapper/gradle-wrapper.properties`），依赖库把阿里云排在官方仓库前面（`settings.gradle.kts`）；
找不到就自动落到下一个仓库，所以在能直连官方的机器上行为不变，要换回去就把这两处的镜像行删掉。
为什么不能换成腾讯云、换镜像站该怎么验，见 [ADR-0011](../docs/decisions/0011-build-mirror-and-gradle-jvm.md)。

要是报的是 `zip END header not found`：`~/.gradle/wrapper/dists/` 下留着一个不是 zip 的坏文件，
**改地址不会让它重新下载**，先把对应的哈希目录删掉再 sync。

**构建 JVM 太新**——跟网络没关系。Android Studio Rabbit 2 自带 JBR 25，Gradle 8.11 只支持到 23，
`~/.jdks/` 下通常只有 17 能用（`openjdk-24` 同样超上限）。IDE 和命令行是**两套入口，各配一次**，
只配一个会只修好一半：

| 报错长什么样 | 卡在哪一步 | 要改的地方 |
| --- | --- | --- |
| `Incompatible Gradle JVM version` / `... Gradle JVM version 25 currently selected`，几百毫秒就失败 | IDE 把它解析出的 JVM 显式交给 Tooling API 起构建，Gradle 当场拒绝 | AS 的 **Gradle JDK** 下拉框，选 17 |
| `* What went wrong:` 后面只有一行版本号，比如 `25.0.4` | 命令行 `gradlew` 起的 daemon 用了 25 | 用户级 `~/.gradle/gradle.properties` 的 `org.gradle.java.home` |

**IDE 这条只能手点，仓库里带不过来。** 在报错弹窗点 `Open JVM settings`，或
Settings → Build, Execution, Deployment → Build Tools → Gradle → **Gradle JDK**，选 `ms-17.0.14`
（列表里没有就 Other… 指到 `~/.jdks/` 下那个 17）。选择落在 `.idea/gradle.xml` 的 `gradleJvm`，
而 `.idea/` 不进仓库——**每次新 clone、每次删掉 `.idea/` 都要重新点一遍**。

弹窗和列表里那几个别碰：`Use JVM 21` 会去下载一份新 JDK（本机没有 21）；`openjdk-24` 超上限；
Oracle `1.8.0_351` 虽然落在 8~23 区间，但 AGP 8.7 要求 17，报错只会推到下一环。

**别指望 `local.properties` 的 `java.home`。** `.idea/gradle.xml` 里那个 `#GRADLE_LOCAL_JAVA_HOME` 宏
看着像正路，但实测把 17 写回去之后，AS 重启、重新打开设置，宏解析出来的仍是 JBR 25——这个值没被采纳，
原因没查清。别再往这个文件里加，见 [ADR-0012](../docs/decisions/0012-ide-and-cli-gradle-jvm.md)。

**命令行这条**：在**用户级** `~/.gradle/gradle.properties` 里写一行
`org.gradle.java.home=<一个 17~23 的 JDK 路径>`，命令行 `gradlew` 走它——实测过：启动时故意用 JBR 25，
daemon 仍按这条起在 17，构建通过。不放项目级 `gradle.properties` 是因为那要提交，而里面是绝对路径。

## 在 Windows 上跑模拟器

只想先看它能不能编：clone → AS 打开 `android` 目录 → **第一次要按上一节把 Gradle JDK 点成 17** →
Build Variants 选 `localDebug` → Make Project。
不需要服务端，也不需要拷任何文件（证书已在仓库里）。要真跑起来才往下走。

1. 装 Android Studio（自带 SDK、模拟器、JDK）。
2. 签一张覆盖模拟器别名地址的证书。用 Git for Windows 自带的 Git Bash 在这台机器上做就行，
   私钥留在本机、不外传也不提交。**前面那个 `MSYS_NO_PATHCONV=1` 不能省**——不关路径转换，
   脚本里的 `-subj "/CN=memora"` 会被 MSYS 改写成 `D:/Git/CN=memora`，openssl 报
   `req: subject name is expected to be in the format /type0=value0/...`：

   ```bash
   MSYS_NO_PATHCONV=1 MEMORA_PUBLIC_IP=10.0.2.2,127.0.0.1,DNS:localhost ./deploy/mkcert.sh ./tls-test
   ```

   `10.0.2.2` 是模拟器看宿主机的固定地址，`127.0.0.1` 给宿主机自己用。
   签完把 `tls-test/server.crt` 改名覆盖 `app/src/local/res/raw/memora_server_cert.pem`——
   **这一步别提交**，仓库里那张对应的私钥在别处，配不上你本机这张。改完 `git status` 会一直显示这一个
   `M`，它是本机产物不是工程事实，提交前看清楚。
3. 交叉编译出 Windows 版服务端。Git Bash 里没有 `make`，所以本机直接用下面那条 `go build`
   （等价于 `make windows`）；没装 Go 就在云机上编好把 `memora.exe` 取过来
   （十几 M 的独立可执行文件，不需要 Go 运行时）：

   ```bash
   GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o memora.exe ./cmd/memora
   ```

4. 写一个 env 文件起服务端。**六项都是必填**，缺任一项服务直接拒起（不带着半套配置蒙跑）：
   `MEMORA_LISTEN_ADDR`、`MEMORA_DB_PATH`、`MEMORA_TLS_CERT`、`MEMORA_TLS_KEY`、
   `MEMORA_PASS_HASH`、`MEMORA_LOCAL_TZ`。口令哈希这样生成，填进 env：

   ```bash
   printf '你的口令' | ./memora.exe hashpass
   ```

   `MEMORA_LOCAL_TZ` 收 `+08:00` 这种固定偏移，也收 IANA 名字——用偏移是因为 Windows 上没有系统 tzdata。
   三个 `MEMORA_LLM_*` 可以不填，此时记录读写照常，只有点生成会回"模型未配置"。
   然后 `./memora.exe serve -config memora.env`（`memora.env` 已被 ignore，别提交）。
5. 不用配 Windows 防火墙。实测模拟器经 `10.0.2.2:8443` 连本机服务端全程无阻断——这条连接是模拟器进程
   在宿主机本地发起的，不是一次外部入站，所以放行规则对它没用也不需要。要放行的只有真机走公网那次。
6. Android Studio 里 Build Variants 选 `localDebug`，点运行。

改过证书就要重装 APK：网络安全配置里的证书是编译期打进包的。

## 装到手机上

模拟器也可以直接连云机，不必先在 Windows 起服务端：选 `serverDebug`，连接页填云机地址即可——
模拟器和真机都能出网，变的只是那张要打进包的证书。

1. `MEMORA_PUBLIC_IP=<公网地址> ./deploy/mkcert.sh /etc/memora/tls`，SAN 里必须是手机实际连的那个地址。
2. 那张 `server.crt` 改名进 `app/src/server/res/raw/memora_server_cert.pem`。
3. Build Variants 选 `serverRelease`（或 `serverDebug`），产出 APK 拷到手机安装。
   首次运行要在连接页手填 `https://<公网地址>:8443`。
4. 签名：仓库里没有 keystore，也不该有。要发 Release 包就在 `app/build.gradle.kts` 里配
   `signingConfig`，keystore 文件已被 `.gitignore` 挡住在仓库外。

## 已知的坑

- **`local` 和 `server` 是两个 App**：`local` 的 applicationId 带 `.local` 后缀，
  所以能同时装、各自的口令和地址互不影响。这也是故意的——测试包不该动正式包的数据。
- **重装 APK 后口令可能要重输**：口令密文绑定 Android KeyStore 里的密钥，
  卸载重装就没了。这不影响记录，只是要再输一次。
- **401 之后不会自动重试**：口令已经是错的，重试只会喂服务端的限速计数（ADR-0002）。
  界面直接回到连接页。
- **报表生成是同步等待**：服务端一次生成最多 60 秒，客户端读超时设到 90 秒。
  这期间按钮禁用、界面转圈，没有后台任务也没有轮询——服务端就是同步返回的（ADR-0004）。
- **归属周期不由客户端算**：周一开头这套规则只在服务端 `internal/period` 一份，
  手机上显示的周/月来自 `GET /v1/periods/now` 和记录里的 `week_start`。手机时区和服务端
  时区不一致时，看到的日期会不一样，这是正确的——报表按服务端时区归档。
