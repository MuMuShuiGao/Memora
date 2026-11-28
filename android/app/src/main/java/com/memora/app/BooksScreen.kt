package com.memora.app

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import kotlinx.coroutines.launch
import java.time.YearMonth

/**
 * 读书屏（ADR-0029）：上面输入书名记一本，下面按读完年份分组。
 * 作者、月份、心得都在编辑页里改——列表只做两件事：看一眼读过什么、点开。
 */
@Composable
fun BooksScreen(api: MemoApi, onUnauthorized: () -> Unit, onOpen: (Book) -> Unit) {
    val scope = rememberCoroutineScope()
    var draft by rememberSaveable { mutableStateOf("") }
    var books by remember { mutableStateOf<List<Book>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    var busy by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf<String?>(null) }
    var loadError by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableStateOf(0) }

    LaunchedEffect(reload) {
        loading = true
        loadError = null
        try {
            books = api.listBooks().books
            // 只有读成功才撤掉上一条写错误：失败时由 error 分支统一改写。
            notice = null
        } catch (e: ApiError) {
            if (e.kind == ApiError.Kind.Unauthorized) {
                onUnauthorized()
            } else {
                loadError = describe(e)
            }
        } finally {
            loading = false
        }
    }

    fun addBook() {
        val title = draft.trim()
        if (title.isEmpty() || busy) return
        busy = true
        notice = null
        scope.launch {
            try {
                // 月份按手机当前年月显式给，不让服务端猜（ADR-0029）。
                val created = api.createBook(title, nowMonth())
                books = books.insertFresh(created)
                draft = ""
            } catch (e: ApiError) {
                if (e.kind == ApiError.Kind.Unauthorized) onUnauthorized() else notice = describe(e)
            } finally {
                busy = false
            }
        }
    }

    Column(Modifier.fillMaxSize()) {
        Row(
            Modifier.fillMaxWidth().padding(start = 20.dp, end = 8.dp, top = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            UnderlineField(
                value = draft,
                onValueChange = { draft = clampCodePoints(it, MemoApi.MAX_BOOK_TITLE_CHARS) },
                placeholder = "读完的书，回车记下",
                modifier = Modifier.weight(1f),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                onSubmit = { addBook() },
                textStyle = MaterialTheme.typography.bodyMedium,
            )
            IconAction(
                DeskIcon.Add,
                "记一本",
                enabled = !busy && draft.isNotBlank(),
                onClick = { addBook() },
            )
        }
        Text(
            if (books.isEmpty()) "读完一本记一本，按年月归档" else "共 ${books.size} 本 · 按读完年月归档",
            style = MaterialTheme.typography.labelSmall,
            color = InkSoft,
            modifier = Modifier.padding(start = 20.dp, top = 6.dp),
        )

        notice?.let {
            Notice(it, Modifier.padding(horizontal = 20.dp, vertical = 10.dp))
        }
        loadError?.let {
            Notice(
                it,
                Modifier.padding(horizontal = 20.dp, vertical = 10.dp),
                actionLabel = "重试",
                onAction = { reload++ },
            )
        }

        when {
            loading && books.isEmpty() -> Text(
                "读书单…",
                style = MaterialTheme.typography.bodySmall,
                color = InkSoft,
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 24.dp),
            )

            books.isEmpty() && loadError != null -> Text(
                "书单没读出来，点上面的重试再读一次",
                style = MaterialTheme.typography.bodyMedium,
                color = InkSoft,
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 28.dp),
            )

            books.isEmpty() -> Column(Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 28.dp)) {
                Text(
                    "还没有读过的书",
                    style = MaterialTheme.typography.titleMedium,
                    color = Ink,
                )
                Spacer(Modifier.height(8.dp))
                Text(
                    "读完一本就在上面记下书名，心得想写再补。",
                    style = MaterialTheme.typography.bodyMedium,
                    color = InkSoft,
                )
            }

            else -> {
                // 服务端按 finished_month 倒序给，groupBy 保持相遇顺序，年份自然也是新的在前。
                val byYear = books.groupBy { it.finishedMonth.take(4) }
                LazyColumn(Modifier.weight(1f).fillMaxWidth()) {
                    byYear.forEach { (year, group) ->
                        item(key = "year-$year") {
                            Column(Modifier.fillMaxWidth()) {
                                SectionRule(Modifier.padding(top = 18.dp))
                                Row(
                                    Modifier
                                        .fillMaxWidth()
                                        .padding(horizontal = 20.dp, vertical = 8.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                ) {
                                    Text(
                                        year,
                                        style = MaterialTheme.typography.titleLarge,
                                        color = Ink,
                                        modifier = Modifier.weight(1f),
                                    )
                                    Text(
                                        "${group.size} 本",
                                        style = MaterialTheme.typography.labelSmall,
                                        color = InkSoft,
                                    )
                                }
                            }
                        }
                        items(group, key = { it.id }) { book ->
                            BookRow(book) { onOpen(book) }
                        }
                    }
                    item { Spacer(Modifier.height(24.dp)) }
                }
            }
        }
    }
}

@Composable
private fun BookRow(book: Book, onClick: () -> Unit) {
    Column(Modifier.fillMaxWidth()) {
        Column(
            Modifier
                .fillMaxWidth()
                .clickable(onClick = onClick)
                .padding(horizontal = 20.dp, vertical = 12.dp),
        ) {
            Text(
                book.title,
                style = MaterialTheme.typography.bodyLarge,
                color = Ink,
            )
            Text(
                buildString {
                    if (book.author.isNotEmpty()) {
                        append(book.author)
                        append(" · ")
                    }
                    append(monthShort(book.finishedMonth))
                    append("读完")
                },
                style = MaterialTheme.typography.labelSmall,
                color = InkSoft,
            )
            if (book.takeaway.isNotEmpty()) {
                TakeawayNote(book.takeaway, enabled = true, onClick = onClick, maxLines = 4)
            } else {
                Text(
                    "补一句心得",
                    style = MaterialTheme.typography.labelSmall,
                    color = InkSoft,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
        }
        Hairline(Modifier.padding(horizontal = 20.dp))
    }
}

/**
 * 一本书的编辑页。四样东西都能改，删要连点两次——和记录编辑页同一套确认方式。
 * 返回键直接放弃未存的修改，和记录编辑页一致；草稿不落盘，书不是边写边想的东西。
 */
@Composable
fun BookEditorScreen(
    api: MemoApi,
    book: Book,
    onDone: () -> Unit,
    onUnauthorized: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    var title by remember(book.id) { mutableStateOf(book.title) }
    var author by remember(book.id) { mutableStateOf(book.author) }
    var month by remember(book.id) { mutableStateOf(book.finishedMonth) }
    var takeaway by remember(book.id) { mutableStateOf(book.takeaway) }
    var pickingMonth by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var askDelete by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf<String?>(null) }

    fun remove() {
        busy = true
        notice = null
        scope.launch {
            try {
                api.deleteBook(book.id)
                onDone()
            } catch (e: ApiError) {
                when (e.kind) {
                    ApiError.Kind.Unauthorized -> onUnauthorized()
                    // 这条在别处被删了，结果和删成功一样。
                    ApiError.Kind.NotFound -> onDone()
                    else -> {
                        notice = describe(e)
                        busy = false
                    }
                }
            }
        }
    }

    fun save() {
        if (busy) return
        askDelete = false
        val newTitle = title.trim()
        if (newTitle.isEmpty()) {
            notice = "书名不能空着"
            return
        }
        val newAuthor = author.trim()
        val newTakeaway = takeaway.trim()
        val titleArg = newTitle.takeIf { it != book.title }
        val authorArg = newAuthor.takeIf { it != book.author }
        val monthArg = month.takeIf { it != book.finishedMonth }
        val takeawayArg = newTakeaway.takeIf { it != book.takeaway }
        if (titleArg == null && authorArg == null && monthArg == null && takeawayArg == null) {
            onDone()
            return
        }
        busy = true
        notice = null
        scope.launch {
            try {
                api.updateBook(book.id, titleArg, authorArg, monthArg, takeawayArg)
                onDone()
            } catch (e: ApiError) {
                when (e.kind) {
                    ApiError.Kind.Unauthorized -> onUnauthorized()
                    ApiError.Kind.NotFound -> onDone()
                    else -> {
                        notice = describe(e)
                        busy = false
                    }
                }
            }
        }
    }

    BackHandler(enabled = !pickingMonth) { onDone() }

    Column(
        Modifier
            .fillMaxSize()
            // 空白处、标题这些没人接的点击也算"点了别处"，撤掉删除确认。
            .pointerInput(askDelete) {
                if (!askDelete) return@pointerInput
                detectTapGestures { askDelete = false }
            },
    ) {
        Row(
            Modifier.fillMaxWidth().padding(start = 20.dp, end = 8.dp, top = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                "改一本书",
                style = MaterialTheme.typography.headlineSmall,
                color = Ink,
                modifier = Modifier.weight(1f),
            )
            TextAction("取消", onClick = onDone, color = InkSoft)
        }
        SectionRule(Modifier.padding(top = 4.dp))

        Column(
            Modifier
                .weight(1f)
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 20.dp),
        ) {
            Spacer(Modifier.height(20.dp))
            UnderlineField(
                value = title,
                onValueChange = { title = clampCodePoints(it, MemoApi.MAX_BOOK_TITLE_CHARS) },
                label = "书名",
                textStyle = MaterialTheme.typography.bodyLarge,
            )
            Spacer(Modifier.height(18.dp))
            UnderlineField(
                value = author,
                onValueChange = { author = clampCodePoints(it, MemoApi.MAX_BOOK_AUTHOR_CHARS) },
                label = "作者",
                placeholder = "可不填",
                textStyle = MaterialTheme.typography.bodyMedium,
            )
            Spacer(Modifier.height(18.dp))
            Text("读完", style = MaterialTheme.typography.labelSmall, color = InkSoft)
            Row(
                Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    monthTitle(month),
                    style = MaterialTheme.typography.bodyLarge,
                    color = Ink,
                    modifier = Modifier
                        .weight(1f)
                        .clickable { pickingMonth = true }
                        .padding(vertical = 8.dp),
                )
                TextAction("改月份", onClick = { pickingMonth = true }, color = InkSoft)
            }
            Hairline()
            Spacer(Modifier.height(24.dp))
            Text(
                "心得 · 最多 ${MemoApi.MAX_BOOK_TAKEAWAY_CHARS} 字",
                style = MaterialTheme.typography.labelSmall,
                color = InkSoft,
            )
            UnderlineField(
                value = takeaway,
                onValueChange = { takeaway = clampCodePoints(it, MemoApi.MAX_BOOK_TAKEAWAY_CHARS) },
                placeholder = "读完留下了什么，值得以后再翻回来看",
                singleLine = false,
                minLines = 4,
                maxLines = 10,
                textStyle = MaterialTheme.typography.bodyMedium,
            )
            val chars = takeaway.codePointCount(0, takeaway.length)
            if (chars > MemoApi.MAX_BOOK_TAKEAWAY_CHARS - 20) {
                Text(
                    "$chars/${MemoApi.MAX_BOOK_TAKEAWAY_CHARS}",
                    style = MaterialTheme.typography.labelSmall,
                    color = if (chars >= MemoApi.MAX_BOOK_TAKEAWAY_CHARS) Vermilion else InkSoft,
                    textAlign = TextAlign.End,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            Spacer(Modifier.height(32.dp))
        }

        notice?.let {
            Notice(it, Modifier.padding(horizontal = 20.dp, vertical = 6.dp))
        }
        Hairline(Modifier.padding(horizontal = 20.dp))
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconAction(
                DeskIcon.Delete,
                if (askDelete) "确认删" else "删掉这本书",
                armed = askDelete,
                enabled = !busy,
                color = InkSoft,
                onClick = {
                    if (!askDelete) askDelete = true else remove()
                },
            )
            Spacer(Modifier.weight(1f))
            PrimaryAction(
                if (busy) "存…" else "存下",
                enabled = !busy && title.isNotBlank(),
                onClick = { save() },
            )
        }
    }

    if (pickingMonth) {
        MonthPickerDialog(
            initialMonth = month,
            onPick = {
                month = it
                pickingMonth = false
            },
            onDismiss = { pickingMonth = false },
        )
    }
}

// 只选年月。系统的日期选择器要点到"日"，这里没有那一天可选（ADR-0029 的"记到月"）。
// 年份用左右尖角翻，月份排成 3×4，朱色底是当前选中的那个。
@Composable
private fun MonthPickerDialog(
    initialMonth: String,
    onPick: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    val initial = parseMonth(initialMonth) ?: YearMonth.now()
    var year by remember { mutableStateOf(initial.year) }

    Dialog(onDismissRequest = onDismiss) {
        Column(
            Modifier
                .width(300.dp)
                .background(Paper)
                .border(1.dp, Ink)
                .padding(20.dp),
        ) {
            Row(
                Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                IconAction(DeskIcon.Prev, "上一年", onClick = { year-- })
                Text(
                    "$year 年",
                    style = MaterialTheme.typography.titleMedium,
                    color = Ink,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.weight(1f),
                )
                IconAction(DeskIcon.Next, "下一年", onClick = { year++ })
            }
            Hairline(Modifier.padding(vertical = 8.dp))
            for (row in 0 until 3) {
                Row(Modifier.fillMaxWidth()) {
                    for (col in 0 until 4) {
                        val m = row * 4 + col + 1
                        val selected = year == initial.year && m == initial.monthValue
                        Box(
                            Modifier
                                .weight(1f)
                                .height(52.dp)
                                .clickable { onPick("$year-${m.toString().padStart(2, '0')}") },
                            contentAlignment = Alignment.Center,
                        ) {
                            Text(
                                "$m 月",
                                style = MaterialTheme.typography.bodyMedium,
                                color = if (selected) Paper else Ink,
                                modifier = Modifier
                                    .then(if (selected) Modifier.background(Vermilion) else Modifier)
                                    .padding(horizontal = 12.dp, vertical = 4.dp),
                            )
                        }
                    }
                }
            }
            Hairline(Modifier.padding(top = 8.dp))
            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.End,
            ) {
                TextAction("取消", onClick = onDismiss, color = InkSoft)
            }
        }
    }
}

// 服务端按 finished_month 倒序；新建的那本要落在同月最前，所以找第一个"不晚于它"的位置。
private fun List<Book>.insertFresh(book: Book): List<Book> {
    val at = indexOfFirst { it.finishedMonth <= book.finishedMonth }
    return if (at < 0) this + book else subList(0, at) + book + subList(at, size)
}