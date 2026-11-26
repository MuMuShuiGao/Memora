package com.memora.app

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

// 地址与口令的唯一入口。ADR-0008 说的三块业务界面之外多出这一屏，
// 是因为口令必须有输入的地方（ADR-0002、0009），不是又加了一个需求。
@Composable
fun ConnectScreen(initialUrl: String, onSave: suspend (String, String) -> String?) {
    val scope = rememberCoroutineScope()
    var url by remember { mutableStateOf(initialUrl) }
    var passphrase by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf<String?>(null) }

    val ready = url.isNotBlank() && passphrase.isNotEmpty() && !busy

    // 这一屏像笔记本的扉页：先是名字，再是接入方式。
    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp),
    ) {
        Spacer(Modifier.height(40.dp))
        Text("Memora", style = MaterialTheme.typography.displaySmall, color = Ink)
        Text(
            "记下的想法，会变成周报和月报。",
            style = MaterialTheme.typography.bodyMedium,
            color = InkSoft,
            modifier = Modifier.padding(top = 6.dp),
        )

        SectionRule(Modifier.padding(top = 24.dp))

        Text(
            "连接服务器",
            style = MaterialTheme.typography.titleLarge,
            color = Ink,
            modifier = Modifier.padding(top = 28.dp),
        )
        Text(
            "地址以 https:// 开头。证书已经打进 APK，地址不在证书里就会握手失败。",
            style = MaterialTheme.typography.bodyMedium,
            color = InkSoft,
            modifier = Modifier.padding(top = 6.dp),
        )

        UnderlineField(
            value = url,
            onValueChange = { url = it },
            label = "服务器地址",
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
            modifier = Modifier.padding(top = 24.dp),
        )

        UnderlineField(
            value = passphrase,
            onValueChange = { passphrase = it },
            label = "访问口令",
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            modifier = Modifier.padding(top = 16.dp),
        )

        PrimaryAction(
            if (busy) "连接中…" else "测试并保存",
            enabled = ready,
            modifier = Modifier.fillMaxWidth().padding(top = 28.dp),
            onClick = {
                busy = true
                notice = null
                val target = normalizeUrl(url)
                val typed = passphrase
                scope.launch {
                    // 用一次真实请求当验收：这个接口同时要口令、又只走 TLS，
                    // 一次调用把证书、地址、口令三件事都验了。
                    val probe = MemoApi(target)
                    probe.passphrase = typed
                    val error = runCatching { probe.periods() }.exceptionOrNull()
                    busy = false
                    notice = when {
                        error == null -> onSave(target, typed)
                        error is ApiError -> describe(error)
                        else -> "没预料的错：${error?.message}"
                    }
                }
            },
        )

        TextAction(
            "服务器还没起来？只保存，先不测",
            enabled = ready,
            color = InkSoft,
            modifier = Modifier.padding(top = 4.dp),
            onClick = {
                val target = normalizeUrl(url)
                scope.launch { notice = onSave(target, passphrase) }
            },
        )

        notice?.let {
            Notice(it, Modifier.padding(top = 16.dp))
        }
        Spacer(Modifier.height(32.dp))
    }
}

internal fun normalizeUrl(raw: String): String {
    val trimmed = raw.trim().trimEnd('/')
    return if (trimmed.startsWith("https://") || trimmed.startsWith("http://")) {
        // 明文一律升级：服务端只接受 HTTPS，写 http:// 只会更快失败。
        if (trimmed.startsWith("http://")) "https://" + trimmed.removePrefix("http://") else trimmed
    } else {
        "https://$trimmed"
    }
}