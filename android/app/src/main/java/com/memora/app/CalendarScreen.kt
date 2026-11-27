package com.memora.app

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import java.time.DayOfWeek
import java.time.LocalDate
import java.time.YearMonth

/**
 * 日历屏。它不是第二份数据，是同一批待办按 due_on 分天（ADR-0021）；
 * 和待办页一样，每次进屏都重拉 GET /v1/tasks，客户端不留跨页缓存。
 */
@Composable
fun CalendarScreen(api: MemoApi, onUnauthorized: () -> Unit) {
    val scope = rememberCoroutineScope()
    val todayDate = today()
    var selectedDay by rememberSaveable { mutableStateOf(todayDate.toString()) }
    val selected = parseDay(selectedDay) ?: todayDate
    val month = YearMonth.from(selected)

    var tasks by remember { mutableStateOf<List<Task>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    // 写入口只留一条在飞：勾选与新增都会改同一份列表，交叠出去就说不清以谁为准。
    var busy by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableStateOf(0) }
    var draft by remember { mutableStateOf("") }

    LaunchedEffect(reload) {
        loading = true
        notice = null
        try {
            tasks = api.listTasks().tasks
        } catch (e: ApiError) {
            if (e.kind == ApiError.Kind.Unauthorized) onUnauthorized() else notice = describe(e)
        } finally {
            loading = false
        }
    }

    fun runWrite(block: suspend () -> Unit) {
        scope.launch {
            busy = true
            notice = null
            try {
                block()
            } catch (e: ApiError) {
                if (e.kind == ApiError.Kind.Unauthorized) {
                    onUnauthorized()
                } else {
                    notice = describe(e)
                    // 本地那份已经跟服务端不一致了，回滚之后重拉一份真的。
                    reload++
                }
            } finally {
                busy = false
            }
        }
    }

    fun addTask() {
        val text = draft.trim()
        if (text.isEmpty() || busy) return
        // 捕获点下那一刻的选中日：请求在飞时用户可能已经翻到别的日期。
        val day = selected.toString()
        runWrite {
            val created = api.createTask(text, day)
            // 新条目是服务端的第一名，在它自己那一天里也排最前。
            tasks = listOf(created) + tasks
            draft = ""
        }
    }

    fun toggle(task: Task) {
        if (busy) return
        val snapshot = tasks
        val toDone = !task.done
        tasks = tasks.map {
            if (it.id == task.id) it.copy(done = toDone) else it
        }
        runWrite {
            try {
                val got = api.setTaskDone(task.id, toDone)
                tasks = tasks.map {
                    if (it.id == got.id) {
                        it.copy(done = got.done, doneAt = got.doneAt, doneWeekStart = got.doneWeekStart)
                    } else {
                        it
                    }
                }
            } catch (e: ApiError) {
                tasks = snapshot
                throw e
            }
        }
    }

    fun showMonth(delta: Long) {
        notice = null
        // 切月时选中落到那个月的 1 号（design：选中日永远在看的这个月里）。
        selectedDay = month.plusMonths(delta).atDay(1).toString()
    }

    fun pick(date: LocalDate) {
        notice = null
        // 相邻月的格子也能点：点它就等于切到那个月并选中这一天。
        selectedDay = date.toString()
    }

    val byDay = remember(tasks) { tasks.groupBy { it.dueOn } }
    val selectedTasks = byDay[selected.toString()].orEmpty()
    val first = month.atDay(1)
    val gridStart = first.minusDays((first.dayOfWeek.value - 1).toLong())
    val cells = (0 until 42).map { gridStart.plusDays(it.toLong()) }
    val listScroll = rememberScrollState()
    LaunchedEffect(selectedDay) { listScroll.scrollTo(0) }

    Column(Modifier.fillMaxSize()) {
        Row(
            Modifier.fillMaxWidth().padding(start = 20.dp, end = 8.dp, top = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text(
                    "${month.year}年${month.monthValue}月",
                    style = MaterialTheme.typography.titleLarge,
                    color = Ink,
                )
                Text(
                    "周一开始 · 点日期看当天安排",
                    style = MaterialTheme.typography.labelSmall,
                    color = InkSoft,
                )
            }
            if (month != YearMonth.from(todayDate)) {
                TextAction("今天", onClick = { pick(todayDate) })
                Spacer(Modifier.width(4.dp))
            }
            IconAction(DeskIcon.Prev, "上一月", onClick = { showMonth(-1) })
            IconAction(DeskIcon.Next, "下一月", onClick = { showMonth(1) })
        }

        Row(Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 2.dp)) {
            listOf("周一", "周二", "周三", "周四", "周五", "周六", "周日").forEach {
                Text(
                    it,
                    style = MaterialTheme.typography.labelSmall,
                    color = InkSoft,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.weight(1f),
                )
            }
        }

        Column(Modifier.fillMaxWidth().padding(horizontal = 20.dp)) {
            cells.chunked(7).forEach { week ->
                Row(Modifier.fillMaxWidth()) {
                    week.forEach { date ->
                        DayCell(
                            date = date,
                            month = month,
                            selected = selected,
                            todayDate = todayDate,
                            tasks = byDay[date.toString()].orEmpty(),
                            modifier = Modifier.weight(1f),
                            onClick = { pick(date) },
                        )
                    }
                }
            }
        }

        Hairline(Modifier.padding(top = 10.dp))

        Row(
            Modifier.fillMaxWidth().padding(start = 20.dp, end = 20.dp, top = 10.dp, bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(dayTitle(selected), style = MaterialTheme.typography.titleSmall, color = Ink)
            if (selected == todayDate) {
                Spacer(Modifier.width(8.dp))
                Text("今天", style = MaterialTheme.typography.labelSmall, color = Vermilion)
            }
            Spacer(Modifier.weight(1f))
            Text(
                "${selectedTasks.size} 条",
                style = MaterialTheme.typography.labelSmall,
                color = InkSoft,
            )
        }

        notice?.let {
            Notice(it, Modifier.padding(horizontal = 20.dp, vertical = 6.dp))
        }

        when {
            loading && tasks.isEmpty() -> Box(
                Modifier.weight(1f).fillMaxWidth().padding(horizontal = 20.dp, vertical = 20.dp),
            ) {
                Text("读待办…", style = MaterialTheme.typography.bodySmall, color = InkSoft)
            }

            selectedTasks.isEmpty() -> Column(
                Modifier.weight(1f).fillMaxWidth().padding(horizontal = 20.dp),
                verticalArrangement = Arrangement.Center,
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Text(
                    "这一天还空着",
                    style = MaterialTheme.typography.bodyMedium,
                    color = InkSoft,
                )
                Text(
                    "在下面写一条，日期就是这一天",
                    style = MaterialTheme.typography.labelSmall,
                    color = InkSoft,
                )
            }

            else -> Column(
                Modifier
                    .weight(1f)
                    .fillMaxWidth()
                    .verticalScroll(listScroll),
            ) {
                selectedTasks.forEach { task ->
                    DayTaskRow(task, enabled = !busy) { toggle(task) }
                }
            }
        }

        Hairline(Modifier.padding(horizontal = 20.dp))
        Row(
            Modifier.fillMaxWidth().padding(start = 20.dp, end = 8.dp, bottom = 2.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            UnderlineField(
                value = draft,
                onValueChange = { draft = it },
                placeholder = "记在${dueText(selected.toString())}，回车记下",
                modifier = Modifier.weight(1f),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                onSubmit = { addTask() },
                textStyle = MaterialTheme.typography.bodyMedium,
            )
            IconAction(
                DeskIcon.Add,
                "加一条",
                enabled = !busy && draft.isNotBlank(),
                onClick = { addTask() },
            )
        }
    }
}

// 一格最多三枚记号：装不下就两条短横加一个 +N，N 是还藏着几条，格高是死的。
@Composable
private fun DayCell(
    date: LocalDate,
    month: YearMonth,
    selected: LocalDate,
    todayDate: LocalDate,
    tasks: List<Task>,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    val inMonth = YearMonth.from(date) == month
    val isSelected = date == selected
    val isToday = date == todayDate
    val digitColor = when {
        isSelected -> Paper
        !inMonth -> InkSoft.copy(alpha = 0.4f)
        isToday -> Vermilion
        else -> Ink
    }
    val openBar = when {
        isSelected -> Paper
        !inMonth -> InkSoft.copy(alpha = 0.25f)
        else -> Ink
    }
    val doneBar = when {
        isSelected -> Paper.copy(alpha = 0.45f)
        !inMonth -> RuleLine.copy(alpha = 0.55f)
        else -> RuleLine
    }
    val weekStyle = MaterialTheme.typography.labelSmall.copy(fontSize = 8.sp, lineHeight = 8.sp)
    val subColor = if (isSelected) Paper.copy(alpha = 0.85f) else InkSoft

    Box(
        modifier
            .height(48.dp)
            .padding(1.dp)
            .background(if (isSelected) Vermilion else Color.Transparent)
            // 今天没选中时不靠颜色深浅区分：朱色数字加 1dp 朱框，和选中是两种形状。
            .then(if (isToday && !isSelected) Modifier.border(1.dp, Vermilion) else Modifier)
            .clickable(onClick = onClick)
            .padding(horizontal = 3.dp, vertical = 2.dp),
    ) {
        Column(
            Modifier.fillMaxSize(),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                "${date.dayOfMonth}",
                style = MaterialTheme.typography.labelMedium,
                color = digitColor,
            )
            if (date.dayOfWeek == DayOfWeek.MONDAY && inMonth) {
                Text("第${isoWeek(date)}周", style = weekStyle, color = subColor)
            }
            Spacer(Modifier.height(2.dp))
            val shown = if (tasks.size > 3) 2 else tasks.size
            repeat(shown) { i ->
                Box(
                    Modifier
                        .fillMaxWidth()
                        .padding(vertical = 1.dp)
                        .height(2.dp)
                        .background(if (tasks[i].done) doneBar else openBar),
                )
            }
            if (tasks.size > 3) {
                Text("+${tasks.size - 2}", style = weekStyle, color = subColor)
            }
        }
    }
}

@Composable
private fun DayTaskRow(task: Task, enabled: Boolean, onToggle: () -> Unit) {
    Column(Modifier.fillMaxWidth()) {
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 11.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            TaskCheckbox(checked = task.done, onToggle = onToggle, enabled = enabled)
            Spacer(Modifier.width(12.dp))
            Text(
                task.body,
                style = MaterialTheme.typography.bodyLarge,
                color = if (task.done) InkSoft else Ink,
                textDecoration = if (task.done) TextDecoration.LineThrough else null,
            )
        }
        Hairline(Modifier.padding(horizontal = 20.dp))
    }
}