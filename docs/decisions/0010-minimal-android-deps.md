# ADR-0010：安卓端只用 OkHttp 直连，不引 Retrofit / DI 容器 / 第三方加密库

> 状态：生效中
> 摘要：网络层自己封装 OkHttp，JSON 用 kotlinx.serialization；否掉 Retrofit、Ktor、Hilt、secure-storage 这类库
> 日期：2026-11-24 ｜ 关联：[0008](0008-native-kotlin-client.md)、[0002](0002-single-user-passphrase.md)、[0009](0009-public-ip-selfsigned-https-access.md)、[../design/android-client.md](../design/android-client.md)

## 决策

客户端只依赖三样东西：OkHttp（含它的证书锁定能力）、kotlinx.serialization、Jetpack 的 DataStore 与 Compose。九个接口各写一个挂起函数，请求拼装、错误解析、超时都在这一个文件里。口令加密直接用系统 KeyStore 的 API，不包第三方库。

## 背景

ADR-0008 把网络层写成"OkHttp/Retrofit"，等于把这两个当成同一档选择，实际动手时必须定一个。同时这个项目有几个已知特征：接口一共九个（design §5）、错误形状服务端已经统一成 `{"error":{"code","message"}}`、只有一个人用、只有一个人维护、编译发生在需求方的 Android Studio 里。库的选择直接影响他第一次打开工程能不能同步成功。

## 备选方案

| 方案 | 主要代价 | 结论 |
| --- | --- | --- |
| **OkHttp + 手写一层封装** | 九个接口各写一个函数，加错误解析约 200 行（估算），以后每加接口都要动这一层 | **采纳** |
| Retrofit + kotlinx.serialization converter | 多两个依赖和一套注解；`CallAdapter`/`Converter` 的价值在横切逻辑（重试、分页、统一拦截）成规模时才显出来 | 否决：本项目错误解析只有一处、超时策略服务端已经定死（60s），横切逻辑一只手数得过来，却要先背下注解处理器的版本互锁 |
| Ktor client | 又一套协程 HTTP 抽象，与 OkHttp 二选一 | 否决：它对自签证书 + 只信任一张证书这个场景没有额外好处，而 OkHttp 是 Compose 工具链里更默认的那条路 |
| Hilt / Koin 依赖注入 | 注解处理器或模块定义的成本全额支付，收益在对象图变大之后才有 | 否决：需要注入的就是"一个 API 客户端 + 两个存储"，构造函数传参够用 |
| secure-storage / sqlcipher 之类加密偏好库 | 把系统 KeyStore 又包一层，出问题（密钥失效、版本迁移）时最难查 | 否决：[ADR-0008](0008-native-kotlin-client.md) 连带决定要的就是 KeyStore 本身；直接调系统 API，一共二十行 |
| 把地址和口令写进 `SharedPreferences` 明文 | 少写二十行 | 否决：口令是整个访问控制的唯一防线（ADR-0002），明文躺在偏好文件里等于把防线交给任何拿到手机并 root 的人 |

## 影响

- **得到**：`libs.versions.toml` 里的条目一屏放得下，需求方那边同步失败的概率最低；错误形状与服务端只对齐一次。
- **付出**：这层封装没有测试兜底（本机没有 JDK 和 Android SDK，客户端代码在需求方编译之前属于"看起来对但无证据"）；接口变多时手写的那部分会线性增长。
- **连带**：证书锁定走 `network_security_config`（声明式）而不是代码里的 `CertificatePinner`——因为要信任的是打进 APK 的那张自签证书本身，声明式写法漏拷证书时会在编译期失败，运行期失败换成编译期失败（详见 design §6）。
- **返工点**：改成 Retrofit 只动网络层那一个文件，界面与存储不动。

## 复查条件

出现以下情况时重新评估本决策：

- 接口超过十五个，或需要重试、分页、离线队列这类横切逻辑 → 重谈 Retrofit
- 界面状态多到构造函数传递开始碍事 → 重谈 DI 容器
- 需要给口令加"每次启动都要生物识别解锁"这类能力 → 重谈 KeyStore 用法（可能要引 androidx.biometric）
