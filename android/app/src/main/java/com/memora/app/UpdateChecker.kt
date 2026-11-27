package com.memora.app

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.SystemClock
import android.provider.Settings
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.FileProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import java.io.File
import java.util.Locale

/**
 * 应用内更新（ADR-0015）：进主屏查一次，比 versionCode，拉包后交给系统安装页。
 *
 * 只查这一次，不轮询、不排后台任务——单人自用，重启的成本低，多一个调度就多一个能坏的地方。
 * 静默安装不做：下载完调起系统安装页，装不装由人在通知里点确认。
 */
enum class UpdateStage { Idle, Checking, UpToDate, Available, Downloading, Downloaded, NeedsPermission, Installing, Failed, Installed }

class UpdateChecker internal constructor(
    private val context: Context,
    private val api: MemoApi,
    private val scope: CoroutineScope,
) {
    var stage by mutableStateOf(UpdateStage.Idle)
        private set

    // 服务端 release.json 里的那几项，只在这次会话里有效
    var versionName by mutableStateOf("")
        private set
    var notes by mutableStateOf("")
        private set
    var sizeBytes by mutableLongStateOf(0L)
        private set

    /** 已下载的字节；percent 为 -1 表示服务端没给总长 */
    var downloadedBytes by mutableLongStateOf(0L)
        private set
    var percent by mutableIntStateOf(-1)
        private set

    var error by mutableStateOf<String?>(null)
        private set

    private val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
    private val apk = File(context.cacheDir, "app.apk")
    private var targetCode = 0

    /**
     * 查一次服务端版本。Idle 与 UpToDate 界面都不占地方——"还没发过版"和"已经是最新"
     * 不值得打扰用户（ADR-0015）。Installed 是唯一一句好消息，且只在这一次会话里出现。
     */
    fun check() {
        if (stage == UpdateStage.Downloading || stage == UpdateStage.Installing) return
        scope.launch(Dispatchers.IO) {
            stage = UpdateStage.Checking
            error = null
            // 404 在这里不是错误：服务端一次版都没发过。
            val info = try {
                api.releaseInfo()
            } catch (e: ApiError) {
                stage = UpdateStage.Failed
                error = e.message
                return@launch
            }
            val code = info?.versionCode ?: 0
            if (info == null || code <= BuildConfig.VERSION_CODE) {
                showCurrent()
                return@launch
            }
            targetCode = code
            versionName = info.versionName
            notes = info.notes
            sizeBytes = info.size
            downloadedBytes = 0
            percent = -1
            stage = UpdateStage.Available
        }
    }

    /** 这条提醒收起来，服务端再发别的版本时还会提醒 */
    fun skip() {
        if (stage == UpdateStage.Available || stage == UpdateStage.Failed) {
            prefs.edit().putInt(KEY_SKIPPED, targetCode).apply()
            showCurrent()
        }
    }

    fun download() {
        if (stage != UpdateStage.Available && stage != UpdateStage.Failed) return
        scope.launch(Dispatchers.IO) {
            stage = UpdateStage.Downloading
            percent = -1
            downloadedBytes = 0
            error = null
            var lastTick = 0L
            try {
                val written = api.downloadApk(apk) { done, total ->
                    downloadedBytes = done
                    // 每秒最多刷一次：进度不需要每 64 KB 重组一遍。
                    val now = SystemClock.elapsedRealtime()
                    if (now - lastTick < 1000) return@downloadApk
                    lastTick = now
                    percent = if (total > 0) ((done * 100) / total).toInt() else -1
                }
                if (sizeBytes > 0 && written != sizeBytes) {
                    stage = UpdateStage.Failed
                    error = "下载到 $written 字节，与服务端标的 $sizeBytes 不一致，重下一次"
                    return@launch
                }
                percent = 100
                stage = UpdateStage.Downloaded
            } catch (e: ApiError) {
                apk.delete()
                stage = UpdateStage.Failed
                error = e.message
            }
        }
    }

    /**
     * 调起系统安装页。装成功时系统会换掉本进程；用户在安装页按返回，进程还在，
     * 状态停在 Installing，所以"接着装"也走这里。
     *
     * 第一次会撞上"本机不允许 Memora 装应用"——Android 8 起这是按来源算的特殊权限，
     * 不是运行时弹窗。硬跳过去只会看到一句"For your security…"的死路，所以先查一次，
     * 没允许就把人送到那个开关前面（NeedsPermission）。
     */
    fun install() {
        if (stage !in setOf(UpdateStage.Downloaded, UpdateStage.Installing, UpdateStage.NeedsPermission)) return
        if (context.packageManager.canRequestPackageInstalls()) {
            handOff()
            return
        }
        stage = UpdateStage.NeedsPermission
        context.startActivity(
            Intent(
                Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
                Uri.parse("package:${context.packageName}"),
            ).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
        )
    }

    private fun handOff() {
        val uri = FileProvider.getUriForFile(context, "${context.packageName}.fileprovider", apk)
        val intent = Intent(Intent.ACTION_VIEW).apply {
            setDataAndType(uri, "application/vnd.android.package-archive")
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        // 标记先落盘再跳安装页：装完是全新进程，冷启动那次 check 靠它认出"刚装好"。
        prefs.edit().putInt(KEY_PENDING, targetCode).apply()
        stage = UpdateStage.Installing
        context.startActivity(intent)
    }

    /** 首屏那一行提醒；没配更新目录、已跳过这一版、正在检查，都回 null */
    fun hintText(): String? = when {
        stage == UpdateStage.Available && prefs.getInt(KEY_SKIPPED, 0) != targetCode ->
            "有新版 v$versionName（${aboutMb(sizeBytes)}）可更新"

        stage == UpdateStage.Downloading ->
            "正在下载 v$versionName：" + if (percent >= 0) "$percent%" else aboutMb(downloadedBytes)

        stage == UpdateStage.Downloaded -> "v$versionName 已下载，还差安装这一步"
        stage == UpdateStage.NeedsPermission -> "系统还没允许 Memora 装应用，点进来开一下"
        stage == UpdateStage.Installing -> "等系统安装页装 v$versionName"
        stage == UpdateStage.Installed -> "v$versionName 刚装好"
        stage == UpdateStage.Failed -> "更新没成：$error"
        else -> null
    }

    /**
     * 当前这版是不是"刚从服务器上装下来的"。这个标记是在旧进程里、跳安装页之前写的，
     * 新进程第一次查完版本号对上就认定装好了，随即清掉，所以这句只出现一次。
     */
    private fun showCurrent() {
        val pending = prefs.getInt(KEY_PENDING, 0)
        if (pending == BuildConfig.VERSION_CODE) {
            prefs.edit().remove(KEY_PENDING).apply()
            versionName = BuildConfig.VERSION_NAME
            stage = UpdateStage.Installed
        } else {
            stage = UpdateStage.UpToDate
        }
    }

    companion object {
        private const val PREFS = "memora.update"
        private const val KEY_SKIPPED = "skipped_version_code"
        private const val KEY_PENDING = "pending_install_code"

        internal fun aboutMb(bytes: Long): String =
            if (bytes <= 0) "大小未知" else String.format(Locale.CHINA, "%.1f MB", bytes / 1048576.0)
    }
}

@Composable
fun rememberUpdateChecker(api: MemoApi): UpdateChecker {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val checker = remember(api) { UpdateChecker(context.applicationContext, api, scope) }
    // 进程内只在进主屏那一次查（ADR-0015）
    LaunchedEffect(checker) { checker.check() }
    return checker
}
