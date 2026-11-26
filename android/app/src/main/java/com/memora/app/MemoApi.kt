package com.memora.app

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.decodeFromString
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import java.io.File
import java.io.IOException
import java.net.URLEncoder
import java.util.concurrent.TimeUnit
import javax.net.ssl.SSLException

class MemoApi(private val baseUrl: String) {

    // 口令变了要重建这个对象（App.kt 里用 remember(url, passphrase) 保证这一点）。
    @Volatile
    var passphrase: String = ""

    private val client = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        // 读取超时必须大于服务端单次生成的 60s 预算加一次重试的余量。客户端先断开的话，
        // 用户看到"连不上"，而报表其实正在库里落地——那是最难复查的一种误解。
        .readTimeout(90, TimeUnit.SECONDS)
        .build()

    suspend fun periods(): Periods = get("/v1/periods/now")

    // 服务端按 created_at 倒序返回；before 传上一页最后一条的 created_at。
    suspend fun listNotes(q: String = "", limit: Int = LIST_PAGE, before: String = ""): NoteList =
        get("/v1/notes" + query("q" to q, "limit" to limit.toString(), "before" to before))

    suspend fun createNote(body: String): Note = post("/v1/notes", body)

    suspend fun updateNote(id: Long, body: String): Note = patch("/v1/notes/$id", body)

    // 成功是 204，没有正文。已删除再删回 404，由调用方按"已经不在了"处理。
    suspend fun deleteNote(id: Long) {
        raw("DELETE", "/v1/notes/$id", null)
    }

    suspend fun listReports(kind: String, limit: Int = 60): ReportList =
        get("/v1/reports" + query("kind" to kind, "limit" to limit.toString()))

    suspend fun getReport(kind: String, periodStart: String): Report =
        get("/v1/reports/$kind/$periodStart")

    // 同步等结果：服务端就是这个形状（ADR-0004），客户端不造后台任务也不轮询。
    suspend fun generateReport(kind: String, periodStart: String): Report =
        postEmpty("/v1/reports/$kind/$periodStart/generate")

    // 待办五条，对齐 design §5。勾掉与取消勾走同一个 PATCH 的 done 字段；
    // 调序一次提交整列 id，不发单条 rank——半截顺序比失败更难收拾。
    suspend fun listTasks(): TaskList = get("/v1/tasks")

    suspend fun createTask(body: String): Task = post("/v1/tasks", body)

    suspend fun updateTaskBody(id: Long, body: String): Task = patch("/v1/tasks/$id", body)

    suspend fun setTaskDone(id: Long, done: Boolean): Task =
        sendBody("PATCH", "/v1/tasks/$id", DoneRequest(done))

    suspend fun reorderTasks(ids: List<Long>): TaskList =
        sendBody("POST", "/v1/tasks/reorder", IdsRequest(ids))

    suspend fun deleteTask(id: Long) {
        raw("DELETE", "/v1/tasks/$id", null)
    }

    // 更新通道（ADR-0015）：没有可更新的版本时服务端回 404，这不算错误。
    // 检查更新的调用方不该因为"还没发过版"被打扰，所以这里把 404 摘成 null，其余照抛。
    suspend fun releaseInfo(): ReleaseInfo? =
        try {
            get("/v1/app/release")
        } catch (e: ApiError) {
            if (e.kind == ApiError.Kind.NotFound) null else throw e
        }

    // 下到 dest，返回落盘字节数；total 取不到时为 -1。
    // 进度回调每个数据块回一次，多久刷一次由调用方管，这层不看时间。
    suspend fun downloadApk(dest: File, onProgress: (done: Long, total: Long) -> Unit): Long =
        withContext(Dispatchers.IO) {
            val request = Request.Builder()
                .url(baseUrl.trimEnd('/') + "/v1/app/release/file")
                .header("X-Access-Key", passphrase)
                .get()
                .build()
            try {
                client.newCall(request).execute().use { response ->
                    if (!response.isSuccessful) {
                        throw toApiError(response.code, response.header("Retry-After"), response.body?.string().orEmpty())
                    }
                    writeApk(response, dest, onProgress)
                }
            } catch (e: ApiError) {
                throw e
            } catch (e: IOException) {
                if (e is SSLException) {
                    throw ApiError(ApiError.Kind.Certificate, e.message ?: e.javaClass.simpleName)
                }
                throw ApiError(ApiError.Kind.Network, e.message ?: "连接失败")
            }
        }

    private fun writeApk(response: Response, dest: File, onProgress: (Long, Long) -> Unit): Long {
        val body = response.body ?: throw ApiError(ApiError.Kind.Server, "响应没有正文")
        val total = response.headers["Content-Length"]?.toLongOrNull() ?: -1L
        // 先写 .part 再换名：中途断掉留下的是残包，不会让下次以为已经下好了。
        val tmp = File(dest.parentFile, dest.name + ".part")
        tmp.outputStream().use { out ->
            body.byteStream().use { input ->
                val buf = ByteArray(64 * 1024)
                var done = 0L
                while (true) {
                    val n = input.read(buf)
                    if (n < 0) break
                    out.write(buf, 0, n)
                    done += n
                    if (total > 0) onProgress(done, total)
                }
            }
        }
        if (!tmp.renameTo(dest)) throw ApiError(ApiError.Kind.Server, "写 ${dest.name} 失败")
        return dest.length()
    }

    private suspend inline fun <reified T> get(path: String): T =
        JSON.decodeFromString(raw("GET", path, null))

    private suspend inline fun <reified T> post(path: String, body: String): T =
        sendBody("POST", path, BodyRequest(body))

    private suspend inline fun <reified T> patch(path: String, body: String): T =
        sendBody("PATCH", path, BodyRequest(body))

    // 三种载荷各是一个小序列化类（正文 / done / ids），拼串这件事只留一处。
    private suspend inline fun <reified T, reified P : Any> sendBody(method: String, path: String, payload: P): T =
        JSON.decodeFromString(raw(method, path, JSON.encodeToString(payload)))

    private suspend inline fun <reified T> postEmpty(path: String): T =
        JSON.decodeFromString(raw("POST", path, ""))

    private suspend fun raw(method: String, path: String, jsonBody: String?): String =
        withContext(Dispatchers.IO) {
            val requestBody = when (method) {
                "GET", "DELETE" -> null
                // POST 生成接口不读请求体，但 OkHttp 要求 POST 必须带一个体——空串即可。
                else -> (jsonBody ?: "").toRequestBody(JSON_TYPE)
            }
            val request = Request.Builder()
                .url(baseUrl.trimEnd('/') + path)
                .header("X-Access-Key", passphrase)
                .method(method, requestBody)
                .build()

            try {
                client.newCall(request).execute().use { response ->
                    val text = response.body?.string() ?: ""
                    if (!response.isSuccessful) {
                        throw toApiError(response.code, response.header("Retry-After"), text)
                    }
                    text
                }
            } catch (e: ApiError) {
                throw e
            } catch (e: IOException) {
                // 证书问题的异常本身就是 IOException 的子类，必须在"连不上"之前判掉：
                // 这两种的下一步动作完全不同（一个重装包，一个查网络）。
                if (e is SSLException) {
                    throw ApiError(ApiError.Kind.Certificate, e.message ?: e.javaClass.simpleName)
                }
                throw ApiError(ApiError.Kind.Network, e.message ?: "连接失败")
            }
        }

    private fun toApiError(status: Int, retryAfter: String?, text: String): ApiError {
        var code = ""
        var message = text.take(200)
        var failed: Report? = null

        runCatching {
            val root = JSON.parseToJsonElement(text) as? JsonObject ?: return@runCatching
            (root["error"] as? JsonObject)?.let { error ->
                error["code"]?.jsonPrimitive?.contentOrNull?.let { code = it }
                error["message"]?.jsonPrimitive?.contentOrNull?.let { message = it }
            }
            (root["report"] as? JsonObject)?.let {
                failed = JSON.decodeFromJsonElement(Report.serializer(), it)
            }
        }

        val kind = when {
            status == 401 -> ApiError.Kind.Unauthorized
            status == 429 -> ApiError.Kind.RateLimited
            status == 404 && code == "not_generated" -> ApiError.Kind.NotGenerated
            status == 404 -> ApiError.Kind.NotFound
            status == 409 || code == "no_records" -> ApiError.Kind.NoRecords
            status == 413 -> ApiError.Kind.TooLarge
            status == 503 && code == "llm_not_configured" -> ApiError.Kind.LlmNotConfigured
            status == 502 -> ApiError.Kind.GenerationFailed
            status in 400..499 -> ApiError.Kind.BadRequest
            else -> ApiError.Kind.Server
        }
        return ApiError(
            kind = kind,
            message = message,
            retryAfterSeconds = retryAfter?.toIntOrNull() ?: 0,
            failedReport = failed,
        )
    }

    private fun query(vararg pairs: Pair<String, String>): String {
        val kept = pairs.filter { it.second.isNotEmpty() }
        if (kept.isEmpty()) return ""
        return "?" + kept.joinToString("&") { (key, value) ->
            "$key=${URLEncoder.encode(value, "UTF-8")}"
        }
    }

    @Serializable
    private data class BodyRequest(val body: String)

    @Serializable
    private data class DoneRequest(val done: Boolean)

    @Serializable
    private data class IdsRequest(val ids: List<Long>)

    companion object {
        // 输入框计录用这个数字，服务端也有一份并会在超限时回 413。两处都有是有意的取舍，
        // 见 docs/design/android-client.md 的未决问题。
        const val MAX_NOTE_CHARS = 2000

        // 一次拉这么多。服务端的上限就是 200（internal/api/notes.go 的 maxListLimit），
        // 取满它能让"更早的"在绝大多数情况下一次到底。
        const val LIST_PAGE = 200

        private val JSON_TYPE = "application/json; charset=utf-8".toMediaType()

        private val JSON = Json {
            ignoreUnknownKeys = true
            explicitNulls = false
        }
    }
}
