package com.memora.app

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
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

    // 不缓存业务数据（ADR-0001）：从编辑页回来这一屏重进组合，effect 自然再拉一次。
    // 过滤交给服务端，客户端不本地筛——两边结果不一致时，"手机上看不到服务器上有的那条"
    // 是最难解释的差异。
    LaunchedEffect(search) {
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

    Column(Modifier.fillMaxSize()) {
        UnderlineField(
            value = search,
            onValueChange = { search = it },
            placeholder = "搜关键词",
            modifier = Modifier.fillMaxWidth().padding(start = 20.dp, end = 20.dp, top = 10.dp),
            textStyle = MaterialTheme.typography.bodyMedium,
        )

        notice?.let {
            Notice(it, Modifier.padding(horizontal = 20.dp, vertical = 12.dp))
        }

        Box(Modifier.weight(1f).fillMaxWidth()) {
            when {
                loading && notes.isEmpty() -> Text(
                    "读记录…",
                    style = MaterialTheme.typography.bodySmall,
                    color = InkSoft,
                    modifier = Modifier.padding(horizontal = 20.dp, vertical = 24.dp),
                )

                notes.isEmpty() -> EmptyNotes(search)

                else -> LazyColumn(Modifier.fillMaxSize()) {
                    items(notes, key = { it.id }) { note ->
                        NoteRow(note) { onOpen(note) }
                    }
                    if (canPage) item {
                        Box(
                            Modifier.fillMaxWidth().padding(vertical = 4.dp),
                            contentAlignment = Alignment.Center,
                        ) {
                            TextAction(
                                "更早的",
                                enabled = !loading,
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
                            )
                        }
                    }
                }
            }
        }

        Hairline()
        PrimaryAction(
            "记一条",
            onClick = { onOpen(null) },
            modifier = Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 12.dp),
        )
    }
}

@Composable
private fun NoteRow(note: Note, onClick: () -> Unit) {
    Column(Modifier.fillMaxWidth()) {
        Row(
            Modifier
                .fillMaxWidth()
                .clickable(onClick = onClick)
                .padding(horizontal = 20.dp, vertical = 14.dp),
            verticalAlignment = Alignment.Top,
        ) {
            // 日期单独成栏，像账本的页边：同一天的记录一列对齐，扫一眼就知道密度。
            Column(Modifier.width(52.dp)) {
                Text(
                    formatDay(note.createdAt),
                    style = MaterialTheme.typography.bodySmall,
                    color = Ink,
                )
                Text(
                    formatTime(note.createdAt),
                    style = MaterialTheme.typography.labelSmall,
                    color = InkSoft,
                )
            }
            Spacer(Modifier.width(14.dp))
            Column(Modifier.weight(1f)) {
                Text(
                    note.body,
                    style = MaterialTheme.typography.bodyLarge,
                    color = Ink,
                    maxLines = 4,
                    overflow = TextOverflow.Ellipsis,
                )
                Spacer(Modifier.height(6.dp))
                Text(
                    "归 ${note.weekStart} 那周",
                    style = MaterialTheme.typography.labelSmall,
                    color = InkSoft,
                )
            }
        }
        Hairline(Modifier.padding(horizontal = 20.dp))
    }
}

@Composable
private fun EmptyNotes(search: String) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 28.dp)) {
        Text(
            if (search.isEmpty()) "还没有记录" else "没有匹配的记录",
            style = MaterialTheme.typography.titleMedium,
            color = Ink,
        )
        Spacer(Modifier.height(8.dp))
        Text(
            if (search.isEmpty()) "想到什么就写一条，周末它会被整理成报表。" else "换个关键词试试。",
            style = MaterialTheme.typography.bodyMedium,
            color = InkSoft,
        )
    }
}