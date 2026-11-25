# 安卓客户端

原生 Kotlin + Compose（ADR-0008），依赖只有 OkHttp 和 kotlinx.serialization（ADR-0010）。
界面划分、错误文案、本地存哪四样，都在 [docs/design/android-client.md](../docs/design/android-client.md)。

**已在 Windows 上编过**：`localDebug` 全绿，APK 实测 9.6 MiB，`res/raw/memora_server_cert.pem` 在包里。
`serverDebug` 停在你还没拷证书这一步，报 `raw/memora_server_cert not found`——这是有意的编译期失败，不是坏了。

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

## sync 卡在下载

第一次 sync 要下两样东西：Gradle 本体（约 130M）和依赖库（几百 M）。境内网络下 `services.gradle.org`
经常直接连接超时，报的是：

```
Could not install Gradle distribution from 'https://services.gradle.org/distributions/gradle-8.11-bin.zip'
Reason: java.net.SocketTimeoutException: Connect timed out
```

这不是工程坏了，也不是网络完全不通：官方地址会 307 跳到 GitHub Releases，实测（curl）只有 60KB/s 量级，
130M 要跑半个多小时，wrapper 等不到就超时。仓库里已经配好镜像：Gradle 本体走华为云
（`gradle/wrapper/gradle-wrapper.properties`），依赖库把阿里云放在官方仓库前面兜底（`settings.gradle.kts`）。
找不到就自动落到下一个仓库，所以在能直连官方的机器上行为不变；要换回官方地址就把那两个文件里的镜像行删掉。

**别改成腾讯云镜像**，看着一样快但会坑一次：它对 curl 返回真 zip，对 Java 的 `HttpURLConnection`
返回 989 字节的 JS 反爬页，而 wrapper 正是用后者下载的，于是报 `zip END header not found`，
还在 `~/.gradle/wrapper/dists/` 下留一个坏文件，改回地址也不会重新下——要先把那个哈希目录删掉。
华为云那个的 sha256 与官方逐字节一致（`57dafb5c…f1e9`），换镜像前都应该先比这个哈希。

## Gradle JDK 不能是 25

Android Studio Rabbit 2 自带的 JBR 是 25，Gradle 8.11 只支持到 23，sync 会失败，
而且报错正文只有版本号一行：

```
* What went wrong:
25.0.4
```

看到这行就是 JVM 太新，不是网络问题。两个地方都能指定，写哪个都行、写两个更好：

- `local.properties` 里 `java.home=<JDK 路径>`——Android Studio 的 Gradle JDK 下拉框选的就是这一项
  （`.idea/gradle.xml` 里那个 `#GRADLE_LOCAL_JAVA_HOME` 指的就是它），只管 IDE。
- 用户级 `~/.gradle/gradle.properties` 里 `org.gradle.java.home=<JDK 路径>`——Gradle 自己认这条，
  命令行构建也生效，而且会盖掉 IDE 选的 JVM。

两处都不进仓库（`local.properties` 已被 ignore，用户级那份在仓库外），因为里面是绝对路径。
只改 IDE 下拉框不够：命令行 `gradlew` 还是会用 `JAVA_HOME`，而这台机器的 `JAVA_HOME` 指向 JDK 8。

## 在 Windows 上跑模拟器

只想先看它能不能编：clone → AS 打开 `android` 目录 → Build Variants 选 `localDebug` → Make Project。
不需要服务端，也不需要拷任何文件（证书已在仓库里）。要真跑起来才往下走。

1. 装 Android Studio（自带 SDK、模拟器、JDK）。
2. 签一张覆盖模拟器别名地址的证书。用 Git for Windows 自带的 Git Bash 在这台机器上做就行，
   私钥留在本机、不外传也不提交：

   ```bash
   MEMORA_PUBLIC_IP=10.0.2.2,127.0.0.1,DNS:localhost ./deploy/mkcert.sh ./tls-test
   ```

   `10.0.2.2` 是模拟器看宿主机的固定地址，`127.0.0.1` 给宿主机自己用。
   签完把 `tls-test/server.crt` 改名覆盖 `app/src/local/res/raw/memora_server_cert.pem`——
   **这一步别提交**，仓库里那张对应的私钥在别处，配不上你本机这张。
3. 交叉编译出 Windows 版服务端。本机装了 Go 就一条命令；没装就在云机上编好把 `memora.exe` 取过来
   （12M 的独立可执行文件，不需要 Go 运行时）：

   ```bash
   make windows   # 或 GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o memora.exe ./cmd/memora
   ```

4. 把 `memora.exe`、`tls-test/server.crt`、`tls-test/server.key` 和一个 env 文件放到一起，
   env 里 `MEMORA_TLS_CERT` / `MEMORA_TLS_KEY` 指到那两个文件，`MEMORA_LISTEN_ADDR=0.0.0.0:8443`，
   然后 `memora.exe serve -config memora.env`。
5. Windows 防火墙放行 8443 入站——不放行的话症状是"连不上服务器"，而不是证书报错。
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
