package com.memora.app

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Card
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
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

@Composable
fun NotesScreen(
    api: MemoApi,
    onUnauthorized: () -> Unit,
    onOpen: (Note?) -> Unit,
) {
    val scope = rememberCoroutineScope()
    var search by remember { mutableStateOf("") }
    var notes by remember { mutableStateOf<List<Note>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    var notice by remember { mutableStateOf<String?>(null) }
    // 只有一页拉满了才暗示后面还有：让"更早的"在到底时还亮着，点下去什么也不发生，
    // 用户会以为是网络问题。
    var canPage by remember { mutableStateOf(false) }
    var reload by remember { mutableStateOf(0) }

    // 不缓存业务数据（ADR-0001）：从编辑页回来这一屏重进组合，effect 自然再拉一次。
    // 过滤交给服务端，客户端不本地筛——两边结果不一致时，"手机上看不到服务器上有的那条"
    // 是最难解释的差异。
    LaunchedEffect(search, reload) {
        if (search.isNotEmpty()) delay(300)
        loading = true
        notice = null
        try {
            val page = api.listNotes(q = search)
            notes = page.notes
            canPage = page.notes.size >= MemoApi.LIST_PAGE
        } catch (e: ApiError) {
            if (e.kind == ApiError.Kind.Unauthorized) onUnauthorized() else notice = describe(e)
        } finally {
            loading = false
        }
    }

    Column(Modifier.fillMaxSize().padding(horizontal = 12.dp)) {
        Row(
            Modifier.fillMaxWidth().padding(top = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            OutlinedTextField(
                value = search,
                onValueChange = { search = it },
                label = { Text("搜关键词") },
                singleLine = true,
                modifier = Modifier.weight(1f),
            )
            Button(onClick = { onOpen(null) }) { Text("记一条") }
            // 另一台设备刚记的那条，这台不会自己知道（没有推送）。用按钮而不是下拉刷新：
            // 后者在 material3 里还是实验 API，而 FR-01 只要求"能看到同一条"。
            TextButton(onClick = { reload++ }, enabled = !loading) { Text("刷新") }
        }

        notice?.let {
            Text(it, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(top = 8.dp))
        }

        when {
            loading && notes.isEmpty() -> Text("读记录…", Modifier.padding(16.dp))
            notes.isEmpty() -> Text(
                if (search.isEmpty()) "还没有记录" else "没有匹配的记录",
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.padding(16.dp),
            )

            else -> LazyColumn(
                Modifier.fillMaxSize().padding(top = 8.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                items(notes, key = { it.id }) { note ->
                    NoteRow(note) { onOpen(note) }
                }
                if (canPage) item {
                    OutlinedButton(
                        onClick = {
                            scope.launch {
                                val last = notes.lastOrNull()?.createdAt ?: return@launch
                                loading = true
                                try {
                                    val page = api.listNotes(q = search, before = last)
                                    notes = notes + page.notes
                                    canPage = page.notes.size >= MemoApi.LIST_PAGE
                                } catch (e: ApiError) {
                                    if (e.kind == ApiError.Kind.Unauthorized) onUnauthorized()
                                    else notice = describe(e)
                                } finally {
                                    loading = false
                                }
                            }
                        },
                        modifier = Modifier.fillMaxWidth().padding(bottom = 16.dp),
                    ) {
                        Text("更早的")
                    }
                }
            }
        }
    }
}

@Composable
private fun NoteRow(note: Note, onClick: () -> Unit) {
    Card(Modifier.fillMaxWidth().clickable(onClick = onClick)) {
        Column(Modifier.padding(12.dp)) {
            Text(note.body, style = MaterialTheme.typography.bodyLarge, maxLines = 4)
            Text(
                "${formatInstant(note.createdAt)}　归 ${note.weekStart} 那周",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}
