package com.memora.app

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectVerticalDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.zIndex
import kotlinx.coroutines.launch

// 屏上只留这么多条未完成，其余靠展开而不是继续滚动（FR-09）。
private const val TOP_COUNT = 5

/**
 * 待办屏。顺序的权威在服务端（ADR-0001），这一屏做的全部是"先把界面挪到位，
 * 再看服务端要不要把它退回来"。
 */
@Composable
fun TasksScreen(api: MemoApi, onUnauthorized: () -> Unit) {
    val scope = rememberCoroutineScope()
    var draft by remember { mutableStateOf("") }
    var tasks by remember { mutableStateOf<List<Task>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    // 有一条写在飞的时候锁住所有写入口：待办的顺序是整列一次提交的（design §5.1），
    // 两次写交叠出去会留下一个服务端对不上的排列。
    var busy by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableStateOf(0) }
    var expanded by rememberSaveable { mutableStateOf(false) }
    // 删走编辑页那套连点两次，记的是"已经点过一次"的那几条。
    var armed by remember { mutableStateOf<Set<Long>>(emptySet()) }

    val drag = remember { DragState() }

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
                    // 本地那份此刻已经跟服务端不一致了。退回现状比留着半截假顺序好解释。
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
        runWrite {
            val created = api.createTask(text)
            // 新建插队首，其余顺延（ADR-0014 的连带决定）。位置就是顺序，rank 由服务端重编。
            tasks = listOf(created) + tasks.filter { !it.done } + tasks.filter { it.done }
            draft = ""
        }
    }

    fun toggle(task: Task) {
        if (busy) return
        val snapshot = tasks
        val toDone = !task.done
        tasks = snapshot.relocate(task.copy(done = toDone))
        runWrite {
            try {
                tasks = tasks.relocate(api.setTaskDone(task.id, toDone))
            } catch (e: ApiError) {
                tasks = snapshot
                throw e
            }
        }
    }

    fun removeTask(task: Task) {
        if (busy) return
        val snapshot = tasks
        armed = armed - task.id
        tasks = snapshot.filterNot { it.id == task.id }
        runWrite {
            try {
                api.deleteTask(task.id)
            } catch (e: ApiError) {
                tasks = snapshot
                throw e
            }
        }
    }

    fun commitOrder(from: Int, to: Int) {
        if (busy || from == to) return
        // 现取而不是用组合里派生好的那份：松手这一帧之前列表可能已经换过。
        val order = tasks.filter { !it.done }.toMutableList()
        if (from !in order.indices || to !in order.indices) return
        val snapshot = tasks
        order.add(to, order.removeAt(from))
        tasks = order + tasks.filter { it.done }
        runWrite {
            try {
                tasks = api.reorderTasks(order.map { it.id }).tasks
            } catch (e: ApiError) {
                tasks = snapshot
                throw e
            }
        }
    }

    val open = tasks.filter { !it.done }
    val done = tasks.filter { it.done }
    val visible = if (expanded) open else open.take(TOP_COUNT)

    Column(Modifier.fillMaxSize()) {
        Row(
            Modifier.fillMaxWidth().padding(start = 20.dp, end = 8.dp, top = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            UnderlineField(
                value = draft,
                onValueChange = { draft = it },
                placeholder = "要做的事，回车记下",
                modifier = Modifier.weight(1f),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                onSubmit = { addTask() },
                textStyle = MaterialTheme.typography.bodyMedium,
            )
            Spacer(Modifier.width(4.dp))
            TextAction("加", enabled = !busy && draft.isNotBlank(), onClick = { addTask() })
            TextAction("刷新", enabled = !busy && !loading, onClick = { reload++ })
        }
        Text(
            "未完成 ${open.size} · 已完成 ${done.size}",
            style = MaterialTheme.typography.labelSmall,
            color = InkSoft,
            modifier = Modifier.padding(start = 20.dp, top = 6.dp),
        )

        notice?.let {
            Notice(it, Modifier.padding(horizontal = 20.dp, vertical = 10.dp))
        }

        when {
            loading && tasks.isEmpty() -> Text(
                "读待办…",
                style = MaterialTheme.typography.bodySmall,
                color = InkSoft,
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 24.dp),
            )

            tasks.isEmpty() -> Text(
                "先在上面记一条要做的事",
                style = MaterialTheme.typography.bodyMedium,
                color = InkSoft,
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 28.dp),
            )

            else -> Column(
                Modifier
                    .weight(1f)
                    .fillMaxWidth()
                    .verticalScroll(rememberScrollState()),
            ) {
                visible.forEachIndexed { index, task ->
                    TaskRow(
                        task = task,
                        index = index,
                        drag = drag,
                        ids = visible.map { it.id },
                        draggable = visible.size > 1 && !busy,
                        enabled = !busy,
                        armed = task.id in armed,
                        onArm = { armed = armed + task.id },
                        onClear = { armed = armed - task.id },
                        onDelete = { removeTask(task) },
                        onToggle = { toggle(task) },
                        onCommit = { from, to -> commitOrder(from, to) },
                    )
                }

                if (open.size > TOP_COUNT) {
                    Row(
                        Modifier.padding(start = 12.dp, top = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        if (!expanded) {
                            Text(
                                "下面还有 ${open.size - TOP_COUNT} 条",
                                style = MaterialTheme.typography.bodySmall,
                                color = InkSoft,
                            )
                        }
                        TextAction(
                            if (expanded) "收起" else "展开",
                            onClick = { expanded = !expanded },
                            enabled = !busy,
                        )
                    }
                }

                if (done.isNotEmpty()) {
                    SectionRule(Modifier.padding(top = 20.dp))
                    Text(
                        "已完成",
                        style = MaterialTheme.typography.labelSmall,
                        color = InkSoft,
                        modifier = Modifier.padding(start = 20.dp, top = 8.dp, bottom = 2.dp),
                    )
                    done.forEach { task ->
                        TaskRow(
                            task = task,
                            index = -1,
                            drag = drag,
                            ids = emptyList(),
                            draggable = false,
                            enabled = !busy,
                            armed = task.id in armed,
                            onArm = { armed = armed + task.id },
                            onClear = { armed = armed - task.id },
                            onDelete = { removeTask(task) },
                            onToggle = { toggle(task) },
                            onCommit = { _, _ -> },
                        )
                    }
                }
            }
        }
    }
}

/**
 * 一次拖拽的临时账。单独成一个类是为了让"正在拖第几条、偏了多少、每行多高"在一帧里
 * 读得一致。拖动期间列表本身不动，只靠 translationY 把行挪开：改列表等于改布局，
 * 手势协程会读到一半的位置表。
 */
private class DragState {
    // -1 是没在拖。order 是按下那一刻的可见顺序，整段拖拽都以它为准。
    var index by mutableStateOf(-1)
    var target by mutableStateOf(-1)
    var dy by mutableStateOf(0f)
    var height by mutableStateOf(0f)
    var order by mutableStateOf<List<Long>>(emptyList())
    val heights = mutableStateMapOf<Long, Float>()

    private fun topOf(at: Int): Float {
        var y = 0f
        for (i in 0 until at) y += heights[order.getOrElse(i) { 0L }] ?: 0f
        return y
    }

    fun begin(at: Int, rowHeight: Float, ids: List<Long>) {
        index = at
        target = at
        height = rowHeight
        dy = 0f
        order = ids
    }

    // 目标位置 = 有多少"别的行"的中线还在被拖这行的中线上方。这样算出来的数直接就是
    // 松手之后它在新列表里的下标，不用再加一减一。
    fun move(delta: Float) {
        if (index !in order.indices) return
        dy += delta
        val center = topOf(index) + height / 2f + dy
        target = order.indices.count { it != index && topOf(it) + (heights[order[it]] ?: height) / 2f < center }
    }

    // 被拖的行跟手；被它压过去的那些整体挪开一个行高。别处一律不动，也不加补位动画（ADR-0013）。
    fun shiftFor(i: Int): Float = when {
        i == index -> dy
        index < 0 -> 0f
        target > index && i in (index + 1)..target -> -height
        target < index && i in target until index -> height
        else -> 0f
    }

    fun idle() {
        index = -1
        target = -1
        dy = 0f
    }
}

@Composable
private fun TaskRow(
    task: Task,
    index: Int,
    drag: DragState,
    ids: List<Long>,
    draggable: Boolean,
    enabled: Boolean,
    armed: Boolean,
    onArm: () -> Unit,
    onClear: () -> Unit,
    onDelete: () -> Unit,
    onToggle: () -> Unit,
    onCommit: (Int, Int) -> Unit,
) {
    val dragging = index >= 0 && drag.index == index

    Column(
        Modifier
            .fillMaxWidth()
            .graphicsLayer {
                translationY = if (index >= 0) drag.shiftFor(index) else 0f
                if (dragging) alpha = 0.94f
            }
            // 被拖的那行浮起来：它正压着别的行往回走，画在下面会被盖掉一截（zIndex 只改画的顺序，
            // 不改布局，所以别人的位置还是按原序列算出来的）。
            .zIndex(if (dragging) 1f else 0f)
            .onSizeChanged { size -> drag.heights[task.id] = size.height.toFloat() }
    ) {
        Row(
            Modifier.fillMaxWidth().padding(start = 20.dp, end = 4.dp, top = 12.dp, bottom = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            TaskCheckbox(checked = task.done, onToggle = onToggle, enabled = enabled)
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f)) {
                Text(
                    task.body,
                    style = MaterialTheme.typography.bodyLarge,
                    color = if (task.done) InkSoft else Ink,
                    textDecoration = if (task.done) TextDecoration.LineThrough else null,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
                task.doneAt?.let {
                    // 只给日期：归哪一周是服务端在勾掉那一刻算死的（design §2），
                    // 界面上再精确到分钟也帮不上决定，只会挤掉正文。
                    Text(
                        "完成于 ${formatDay(it)}",
                        style = MaterialTheme.typography.labelSmall,
                        color = InkSoft,
                    )
                }
            }
            TextAction(
                if (armed) "确认删" else "删",
                onClick = {
                    if (!armed) onArm() else {
                        onClear()
                        onDelete()
                    }
                },
                enabled = enabled,
                color = if (armed) Vermilion else InkSoft,
            )
            if (draggable) {
                DragHandle(
                    rowId = task.id,
                    rowIndex = index,
                    active = dragging,
                    onStart = { drag.heights[task.id]?.let { drag.begin(index, it, ids) } },
                    onMove = { drag.move(it) },
                    onDrop = {
                        val from = drag.index
                        val to = drag.target
                        // 先清手势状态再交新顺序：列表一换，每行的自然位置都变了，
                        // 这时候留着 translationY 会让全列表一起偏一截。
                        drag.idle()
                        if (from != to) onCommit(from, to)
                    },
                )
            } else {
                Spacer(Modifier.width(26.dp))
            }
        }
        Hairline(Modifier.padding(horizontal = 20.dp))
    }
}

// 18dp 空心方框，勾上填墨色再压一道纸色对勾。不用 Material 的 Checkbox：它的圆角底
// 和水波纹跟这片纸不搭（ADR-0013）。对勾是画出来的，不是字符——换字体字符会变成方框。
@Composable
private fun TaskCheckbox(checked: Boolean, onToggle: () -> Unit, enabled: Boolean) {
    Box(
        Modifier
            .size(18.dp)
            .background(if (checked) Ink else Color.Transparent)
            // 直角：印章和方框保持直角，2dp 圆角只留给按钮（ADR-0013）。
            .border(width = if (checked) 0.dp else 1.dp, color = if (enabled) Ink else RuleLine)
            .clickable(enabled = enabled, onClick = onToggle),
        contentAlignment = Alignment.Center,
    ) {
        if (checked) {
            Canvas(Modifier.size(18.dp)) {
                val s = size.width
                val w = s * 0.1f
                drawLine(Paper, Offset(s * 0.22f, s * 0.52f), Offset(s * 0.42f, s * 0.72f), w, cap = StrokeCap.Round)
                drawLine(Paper, Offset(s * 0.42f, s * 0.72f), Offset(s * 0.8f, s * 0.28f), w, cap = StrokeCap.Round)
            }
        }
    }
}

// 拖拽把手：右侧三条短横。手势只从这里起，正文那一大片留给勾选和删。
// pointerInput 按 (哪条, 第几位) 重启：这两个值变了，说明上一次捕获的回调已经过时。
@Composable
private fun DragHandle(
    rowId: Long,
    rowIndex: Int,
    active: Boolean,
    onStart: () -> Unit,
    onMove: (Float) -> Unit,
    onDrop: () -> Unit,
) {
    Column(
        Modifier
            .width(26.dp)
            .height(44.dp)
            .pointerInput(rowId, rowIndex) {
                detectVerticalDragGestures(
                    onDragStart = { onStart() },
                    onDragEnd = { onDrop() },
                    onDragCancel = { onDrop() },
                ) { change, deltaY ->
                    onMove(deltaY)
                    change.consume()
                }
            },
        verticalArrangement = Arrangement.Center,
    ) {
        val line = if (active) Ink else InkSoft
        repeat(3) {
            Box(
                Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 7.dp, vertical = 2.dp)
                    .height(2.dp)
                    .background(line),
            )
        }
    }
}

/**
 * 服务端回来的那一条挪去它此刻该在的位置，其余原样：勾掉的进已完成最前（那里按完成时刻
 * 倒序），取消勾选的落回未完成末尾（ADR-0014 的连带决定）。rank 收紧不用客户端算——
 * 列表的位置本身就是顺序。
 */
private fun List<Task>.relocate(task: Task): List<Task> {
    val rest = filter { it.id != task.id }
    val open = rest.filter { !it.done }
    val finished = rest.filter { it.done }
    val newOpen = if (task.done) open else open + task
    val newDone = if (task.done) listOf(task) + finished else finished
    return newOpen + newDone
}
