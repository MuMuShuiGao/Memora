package com.memora.app

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

private const val TAB_NOTES = "notes"
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

    val url = baseUrl
    if (url == null) {
        // DataStore 第一次读要等一帧，这中间不能跳到任何一屏。
        Busy("读本地配置…")
        return
    }

    val secret = passphrase
    if (secret.isNullOrBlank()) {
        ConnectScreen(
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
        return
    }

    val api = remember(url, secret) { MemoApi(url).apply { passphrase = secret } }

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

@Composable
private fun Workspace(api: MemoApi, local: LocalStore, onForgetPassphrase: () -> Unit) {
    var tab by rememberSaveable { mutableStateOf(TAB_NOTES) }
    var editing by remember { mutableStateOf<Editing?>(null) }

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

    Scaffold { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            Row(Modifier.padding(horizontal = 8.dp)) {
                TextButton(onClick = { tab = TAB_NOTES }) {
                    Text(if (tab == TAB_NOTES) "记录 ●" else "记录")
                }
                TextButton(onClick = { tab = TAB_REPORTS }) {
                    Text(if (tab == TAB_REPORTS) "报表 ●" else "报表")
                }
                TextButton(onClick = onForgetPassphrase) {
                    Text("改口令 / 改地址")
                }
            }
            when (tab) {
                TAB_NOTES -> NotesScreen(api, onForgetPassphrase) { note ->
                    editing = Editing(note)
                }

                else -> ReportsScreen(api, local, onForgetPassphrase)
            }
        }
    }
}

@Composable
fun Busy(text: String) {
    Column(Modifier.fillMaxSize().padding(16.dp)) {
        CircularProgressIndicator()
        Text(text, Modifier.padding(top = 8.dp))
    }
}
