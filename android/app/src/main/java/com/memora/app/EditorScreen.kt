package com.memora.app

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.firstOrNull
import kotlinx.coroutines.launch

// text 的可空就是"草稿还没回填"：回填必须在用户开始打字之前定下来，
// 否则输入框先闪一下空，而那几个字会被后到的草稿盖掉。
@Composable
fun EditorScreen(
    api: MemoApi,
    local: LocalStore,
    note: Note?,
    onDone: () -> Unit,
    onUnauthorized: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    val focus = remember { FocusRequester() }
    var text by remember { mutableStateOf<String?>(null) }
    var notice by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var askDelete by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) {
        val draft = local.draft.firstOrNull()
        text = when {
            note == null -> draft?.body ?: ""
            draft != null && draft.noteId == note.id -> draft.body
            else -> note.body
        }
        focus.requestFocus()
    }

    val value = text ?: ""
    val max = MemoApi.MAX_NOTE_CHARS
    // 数的是码点数，不是 String.length：服务端的 2000 是字符数（utf8.RuneCountInString），
    // 按 UTF-16 长度算的话，一条带表情符号的记录会在客户端被提前挡下。
    val chars = value.codePointCount(0, value.length)
    val over = chars > max

    // 进程随时可能被杀（需求 §5 第 3 条），所以每改一次都要落草稿。
    // 去抖 400ms 只是免得每次按键都打一次磁盘，不是为了省流量。
    LaunchedEffect(value) {
        if (text == null) return@LaunchedEffect
        delay(400)
        local.saveDraft(Draft(note?.id, value))
    }

    Column(Modifier.fillMaxSize().padding(horizontal = 16.dp, vertical = 12.dp)) {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
            Text(
                if (note == null) "记一条" else "改第 ${note.id} 条",
                style = MaterialTheme.typography.titleMedium,
            )
            TextButton(onClick = onDone) { Text("取消") }
        }
        note?.let {
            Text(
                "记于 ${formatInstant(it.createdAt)}　归 ${it.weekStart} 那周",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }

        OutlinedTextField(
            value = value,
            onValueChange = { text = it },
            modifier = Modifier.fillMaxWidth().weight(1f).focusRequester(focus),
            placeholder = { Text("一个想法一条，写完就存") },
        )

        Row(
            Modifier.fillMaxWidth().padding(top = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                "$chars/$max",
                color = if (over) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.weight(1f))
            if (note != null) {
                OutlinedButton(
                    enabled = !busy,
                    onClick = {
                        if (!askDelete) {
                            askDelete = true
                            return@OutlinedButton
                        }
                        busy = true
                        scope.launch {
                            try {
                                api.deleteNote(note.id)
                                local.clearDraft()
                                onDone()
                            } catch (e: ApiError) {
                                // 已删除再删回 404。按设计当"已经不在了"处理，不报错。
                                when (e.kind) {
                                    ApiError.Kind.Unauthorized -> onUnauthorized()
                                    ApiError.Kind.NotFound -> {
                                        local.clearDraft()
                                        onDone()
                                    }

                                    else -> {
                                        notice = describe(e)
                                        busy = false
                                    }
                                }
                            }
                        }
                    },
                ) { Text(if (askDelete) "再点一次确认删除" else "删除") }
            }
            Button(
                // 空白不发请求：服务端会回 400，但没必要跑这一趟。
                enabled = !busy && value.isNotBlank() && !over,
                onClick = {
                    busy = true
                    val body = value.trim()
                    scope.launch {
                        local.saveDraft(Draft(note?.id, body))
                        try {
                            if (note == null) api.createNote(body) else api.updateNote(note.id, body)
                            local.clearDraft()
                            onDone()
                        } catch (e: ApiError) {
                            when (e.kind) {
                                ApiError.Kind.Unauthorized -> onUnauthorized()
                                // 这条在别处被删了，留在编辑框里只会让人再存一次旧的。
                                ApiError.Kind.NotFound -> {
                                    local.clearDraft()
                                    onDone()
                                }

                                else -> {
                                    notice = describe(e)
                                    busy = false
                                }
                            }
                        }
                    }
                },
            ) { Text(if (busy) "存…" else "存") }
        }

        notice?.let {
            Text(it, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(top = 8.dp))
        }
    }
}
