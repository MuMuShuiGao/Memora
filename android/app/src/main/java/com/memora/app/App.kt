package com.memora.app

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

private const val TAB_NOTES = "notes"
private const val TAB_TASKS = "tasks"
private const val TAB_REPORTS = "reports"

// 编辑页的目标：note 为 null 是新建，否则是改这一条。
data class Editing(val note: Note?)

@Composable
fun AppRoot(local: LocalStore, credentials: CredentialStore) {
    var baseUrl by remember { mutableStateOf<String?>(null) }
    var passphrase by remember { mutableStateOf(credentials.passphrase()) }

    LaunchedEffect(Unit) {
        local.baseUrl.collect { baseUrl = it }
    }

    Box(
        Modifier
            .fillMaxSize()
            .background(Paper)
            .windowInsetsPadding(WindowInsets.safeDrawing),
    ) {
        val url = baseUrl
        val secret = passphrase
        when {
            // DataStore 第一次读要等一帧，这中间不能跳到任何一屏。
            url == null -> Busy("读本地配置…")
            secret.isNullOrBlank() -> ConnectScreen(
                initialUrl = url,
                // 返回 null 表示一切正常，否则是一句要显示给用户的提醒。
                onSave = { newUrl, newSecret ->
                    local.setBaseUrl(newUrl)
                    passphrase = newSecret
                    if (credentials.save(newSecret)) {
                        null
                    } else {
                        "这台设备的 Keystore 存不住口令，能用，但每次启动都要重输一遍"
                    }
                },
            )

            else -> {
                val api = remember(url, secret) { MemoApi(url).also { it.passphrase = secret } }
                Workspace(
                    api = api,
                    local = local,
                    onForgetPassphrase = {
                        // 401 之后不重试、不排队：口令已经是错的了，重试只会喂限速计数（ADR-0002）。
                        credentials.clear()
                        passphrase = null
                    },
                )
            }
        }
    }
}

@Composable
private fun Workspace(api: MemoApi, local: LocalStore, onForgetPassphrase: () -> Unit) {
    var tab by rememberSaveable { mutableStateOf(TAB_TASKS) }
    var editing by remember { mutableStateOf<Editing?>(null) }
    // "正在看更新页"不进 savedInstanceState：装完包进程会被系统换掉，
    // 重启后回到正常首屏，那句"刚装好"由更新页自己的状态带出来。
    var showUpdate by remember { mutableStateOf(false) }

    val target = editing
    if (target != null) {
        EditorScreen(
            api = api,
            local = local,
            note = target.note,
            onDone = { editing = null },
            onUnauthorized = onForgetPassphrase,
        )
        return
    }

    // checker 挂在这一层，所以下载中切页签再回来，进度不会丢。
    val update = rememberUpdateChecker(api)
    if (showUpdate) {
        UpdatePage(update, onBack = { showUpdate = false })
        return
    }

    Column(Modifier.fillMaxSize()) {
        Masthead(tab = tab, onTab = { tab = it })
        // 更新提醒不弹窗，占报头下面一行，点了才进更新页（ADR-0015）。
        update.hintText()?.let {
            Notice(
                it,
                Modifier
                    .padding(horizontal = 20.dp, vertical = 6.dp)
                    .clickable(onClick = { showUpdate = true }),
            )
        }
        when (tab) {
            TAB_NOTES -> NotesScreen(api, onForgetPassphrase) { note ->
                editing = Editing(note)
            }

            TAB_TASKS -> TasksScreen(api, onForgetPassphrase)

            else -> ReportsScreen(api, local, onForgetPassphrase)
        }
    }
}

@Composable
private fun Masthead(tab: String, onTab: (String) -> Unit) {
    Column(Modifier.fillMaxWidth()) {
        Text(
            "Memora",
            style = MaterialTheme.typography.headlineSmall,
            color = Ink,
            modifier = Modifier.padding(start = 20.dp, end = 8.dp, top = 8.dp),
        )
        SectionRule(Modifier.padding(top = 8.dp))
        Row(Modifier.padding(horizontal = 20.dp), Arrangement.spacedBy(24.dp)) {
            UnderlineTab("待办", tab == TAB_TASKS) { onTab(TAB_TASKS) }
            UnderlineTab("记录", tab == TAB_NOTES) { onTab(TAB_NOTES) }
            UnderlineTab("报表", tab == TAB_REPORTS) { onTab(TAB_REPORTS) }
        }
    }
}

@Composable
fun Busy(text: String) {
    Column(
        Modifier.fillMaxSize().padding(24.dp),
        verticalArrangement = Arrangement.Center,
    ) {
        Text(text, style = MaterialTheme.typography.bodySmall, color = InkSoft)
    }
}