package com.memora.app

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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

    Column(Modifier.fillMaxSize().padding(16.dp)) {
        Text("连到服务器", style = MaterialTheme.typography.titleLarge)
        Text(
            "地址要带 https://。这张证书是打进 APK 的，地址必须出现在证书的 SAN 里，" +
                "否则握手会失败。",
            style = MaterialTheme.typography.bodySmall,
        )

        OutlinedTextField(
            value = url,
            onValueChange = { url = it },
            label = { Text("服务器地址") },
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
            modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
        )

        OutlinedTextField(
            value = passphrase,
            onValueChange = { passphrase = it },
            label = { Text("访问口令") },
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
        )

        Button(
            enabled = ready,
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
            modifier = Modifier.fillMaxWidth().padding(top = 16.dp),
        ) {
            Text(if (busy) "连接中…" else "测试并保存")
        }

        TextButton(
            enabled = ready,
            onClick = {
                val target = normalizeUrl(url)
                scope.launch { notice = onSave(target, passphrase) }
            },
        ) {
            Text("服务器还没起来？只保存，先不测")
        }

        notice?.let {
            Text(
                it,
                color = MaterialTheme.colorScheme.error,
                modifier = Modifier.padding(top = 12.dp),
            )
        }
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
