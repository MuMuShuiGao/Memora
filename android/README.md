# 安卓客户端

原生 Kotlin + Compose（ADR-0008），依赖只有 OkHttp 和 kotlinx.serialization（ADR-0010）。
界面划分、错误文案、本地存哪四样，都在 [docs/design/android-client.md](../docs/design/android-client.md)。

**这个工程还没有编译过**——开发用的那台服务器上没有 Android SDK，也没有 KVM，跑不了模拟器。
第一次在 Android Studio 里编译，报错按行贴回来改，这是预期的事。

## 两个 flavor

`env` 维度下只有两个构建变体，差别就两样：默认地址，和打进包的那张证书。

| 变体 | 默认地址 | 证书放哪 | 用途 |
| --- | --- | --- | --- |
| `local` | `https://10.0.2.2:8443` | `app/src/local/res/raw/memora_server_cert.pem` | 模拟器连本机 `memora.exe` |
| `server` | 空（必须手填） | `app/src/server/res/raw/memora_server_cert.pem` | 装到手机上连公网服务器 |

`server` 的默认地址是空串，公网 IP 既不进仓库也不进 APK。

证书文件名必须是 `memora_server_cert.pem`——网络安全配置里写的是 `@raw/memora_server_cert`，
资源名就是文件名。**没放证书是编译期失败，不是运行期失败**，这是有意的：
装上一个连不上自己服务器的包，比编译不过难查得多。

## 在 Windows 上跑模拟器（推荐先走这条）

1. 装 Android Studio（自带 SDK、模拟器、JDK）。
2. 生成一张覆盖模拟器别名地址的证书。在这台云机上：

   ```bash
   MEMORA_PUBLIC_IP=10.0.2.2,127.0.0.1 ./deploy/mkcert.sh ./tls-test
   ```

   `10.0.2.2` 是模拟器看宿主机的固定地址，`127.0.0.1` 给宿主机自己用。
3. 交叉编译出 Windows 版服务端（在这台云机上做，Windows 上不用装 Go）：

   ```bash
   make windows   # 或 GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o memora.exe ./cmd/memora
   ```

4. 把 `memora.exe`、`tls-test/server.crt`、`tls-test/server.key` 和一个 env 文件拷到 Windows，
   env 里 `MEMORA_TLS_CERT` / `MEMORA_TLS_KEY` 指到那两个文件，`MEMORA_LISTEN_ADDR=0.0.0.0:8443`，
   然后 `memora.exe serve -config memora.env`。
5. Windows 防火墙放行 8443 入站——不放行的话症状是"连不上服务器"，而不是证书报错。
6. `server.crt` 改名成 `memora_server_cert.pem`，放进 `app/src/local/res/raw/`。
7. Android Studio 里 Build Variants 选 `localDebug`，点运行。

改过证书就要重装 APK：网络安全配置里的证书是编译期打进包的。

## 装到手机上

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
