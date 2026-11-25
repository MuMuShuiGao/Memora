package com.memora.app

// 服务端每种错误在界面上必须有对应的说法（design/android-client.md 的接口表），
// 所以这里把 HTTP 状态码 + error.code 收成一个能 when 分支的枚举，而不是只留一个字符串。
class ApiError(
    val kind: Kind,
    override val message: String,
    val retryAfterSeconds: Int = 0,
    val failedReport: Report? = null,
) : Exception(message) {

    enum class Kind {
        Unauthorized,
        RateLimited,
        BadRequest,
        NotFound,
        NotGenerated,
        NoRecords,
        TooLarge,
        LlmNotConfigured,
        GenerationFailed,
        Server,
        Network,
        Certificate,
    }
}

// 界面上直接显示的话。不显示英文异常名——这台机器上跑的是自己的服务，
// 看到 "SSLPeerUnverifiedException" 对一个想记一条备忘的人没有任何用处。
fun describe(e: ApiError): String = when (e.kind) {
    ApiError.Kind.Unauthorized -> "口令不对，重新输一次"
    ApiError.Kind.RateLimited -> "试太快了，${e.retryAfterSeconds} 秒后再试"
    ApiError.Kind.BadRequest -> e.message
    ApiError.Kind.NotFound -> "这条已经不在了"
    ApiError.Kind.NotGenerated -> "这个周期还没生成过报表"
    ApiError.Kind.NoRecords -> "这个周期一条记录都没有"
    ApiError.Kind.TooLarge -> e.message
    ApiError.Kind.LlmNotConfigured -> "服务端没配模型，找服务器上的配置"
    ApiError.Kind.GenerationFailed -> e.failedReport?.failReason ?: e.message
    ApiError.Kind.Server -> "服务端出错了：${e.message}"
    ApiError.Kind.Network -> "连不上服务器，检查一下网络和地址"
    ApiError.Kind.Certificate -> "证书和地址不匹配，或打进包的证书不是这个环境的（见 android/README.md）"
}
