package com.memora.app

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
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
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDefaults
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.rememberDatePickerState
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
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.semantics.Role
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
    val focusManager = LocalFocusManager.current
    var draft by rememberSaveable { mutableStateOf("") }
    var tasks by remember { mutableStateOf<List<Task>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    // 结构写（加、删、拖序）仍然互斥：顺序是整列一次提交的，两次交叠会留下服务端对不上的排列。
    var busy by remember { mutableStateOf(false) }
    // 勾选不跟着 busy 一起吞点击：一次只锁住正在飞的那一行，别行照常勾。
    var togglingIds by remember { mutableStateOf<Set<Long>>(emptySet()) }
    var notice by remember { mutableStateOf<String?>(null) }
    var loadError by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableStateOf(0) }
    var expanded by rememberSaveable { mutableStateOf(false) }
    var doneExpanded by rememberSaveable { mutableStateOf(false) }
    // 删走编辑页那套连点两次，记的是"已经点过一次、正等确认"的那一条。
    // 同时只认一条：点别处或点上另一只垃圾桶，旧的那次确认就作废。
    var armedId by remember { mutableStateOf<Long?>(null) }
    // 正改着的那一条、它的草稿和改期中的日期。草稿不落盘：一句话的修改不值得再建一套草稿机制（ADR-0020）。
    var editingId by remember { mutableStateOf<Long?>(null) }
    var editDraft by remember { mutableStateOf("") }
    var editDueOn by remember { mutableStateOf("") }
    var pickingDate by remember { mutableStateOf(false) }
    // 完成心得（ADR-0027）：屏底一条可跳过的提示，同一时刻只认一条待办。
    // 正在写时不抢——新的完成不会换掉手上的草稿；草稿本就不落盘，切走即丢。
    var takeawayId by remember { mutableStateOf<Long?>(null) }
    var takeawayEditing by remember { mutableStateOf(false) }
    var takeawayDraft by remember { mutableStateOf("") }

    val drag = remember { DragState() }
    // 结构写要让路给还在飞的勾选；勾选本身只锁自己那一行。
    val structuralEnabled = !busy && togglingIds.isEmpty()

    LaunchedEffect(reload) {
        loading = true
        loadError = null
        try {
            tasks = api.listTasks().tasks
            // 只有读成功才撤掉上一条写错误：失败时由 error 分支统一改写。
            notice = null
        } catch (e: ApiError) {
            if (e.kind == ApiError.Kind.Unauthorized) {
                onUnauthorized()
            } else {
                loadError = describe(e)
                notice = null
            }
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
        if (text.isEmpty() || !structuralEnabled) return
        armedId = null
        runWrite {
            val created = api.createTask(text, today().toString())
            // 新建插队首，其余顺延（ADR-0014 的连带决定）。位置就是顺序，rank 由服务端重编。
            tasks = listOf(created) + tasks.filter { !it.done } + tasks.filter { it.done }
            draft = ""
        }
    }

    // 改正文与日期不碰顺序，不进 busy；勾选那一路可能同时在飞，所以只合自己发出去的字段。
    fun commitEdit() {
        val id = editingId ?: return
        val original = tasks.firstOrNull { it.id == id }
        val text = editDraft.trim()
        val due = editDueOn.ifEmpty { original?.dueOn.orEmpty() }
        editingId = null
        editDraft = ""
        editDueOn = ""
        pickingDate = false
        if (original == null || text.isEmpty()) return
        val bodyChanged = text != original.body
        val dueChanged = due.isNotEmpty() && due != original.dueOn
        if (!bodyChanged && !dueChanged) return
        tasks = tasks.map {
            if (it.id == id) it.copy(
                body = if (bodyChanged) text else it.body,
                dueOn = if (dueChanged) due else it.dueOn,
            ) else it
        }
        scope.launch {
            notice = null
            try {
                val got = api.updateTask(
                    id,
                    body = if (bodyChanged) text else null,
                    dueOn = if (dueChanged) due else null,
                )
                // 只合发出去的那两项：勾选那一路的结果不能被整条覆盖抹回去。
                tasks = tasks.map {
                    if (it.id == id) it.copy(
                        body = got.body,
                        dueOn = got.dueOn,
                    ) else it
                }
            } catch (e: ApiError) {
                if (e.kind == ApiError.Kind.Unauthorized) {
                    onUnauthorized()
                } else {
                    notice = describe(e)
                    reload++
                }
            }
        }
    }

    fun cancelEdit() {
        editingId = null
        editDraft = ""
        editDueOn = ""
        pickingDate = false
    }

    fun startEdit(task: Task) {
        // 结构写进行中不翻编辑；只是别行在勾选时，照常可以点开改字（PATCH 按字段更新）。
        if (busy || task.id in togglingIds || editingId == task.id) return
        // 换一条改时先把上一条落定，否则那段字会随编辑态一起无声消失。
        commitEdit()
        // 手里拿着删除确认又点开正文，说明意图已经换了；不清掉的话下一次点垃圾桶就是真删。
        armedId = null
        editingId = task.id
        editDraft = task.body
        editDueOn = task.dueOn
        pickingDate = false
    }

    fun closeTakeaway() {
        takeawayId = null
        takeawayEditing = false
        takeawayDraft = ""
    }

    fun startTakeaway(task: Task) {
        if (busy || task.id in togglingIds) return
        // 换到心得就是换了意图：正文那边的草稿先落定，删除确认作废。
        if (editingId != null) commitEdit()
        armedId = null
        takeawayId = task.id
        takeawayEditing = true
        takeawayDraft = task.takeaway
    }

    fun offerTakeaway(task: Task) {
        // 已经写过的不再问；正在写不打断。跳过、或写完一条之后再勾，下一条照常会问。
        if (task.takeaway.isNotEmpty() || takeawayEditing) return
        takeawayId = task.id
        takeawayEditing = false
        takeawayDraft = ""
    }

    fun saveTakeaway() {
        val task = tasks.firstOrNull { it.id == takeawayId } ?: run {
            closeTakeaway()
            return
        }
        val text = takeawayDraft.trim()
        closeTakeaway()
        if (text == task.takeaway) return
        // 先记账再发请求：写完这一句，提示就该走开，不必等往返。
        tasks = tasks.map { if (it.id == task.id) it.copy(takeaway = text) else it }
        scope.launch {
            notice = null
            try {
                val got = api.updateTask(task.id, takeaway = text)
                tasks = tasks.map { if (it.id == got.id) it.copy(takeaway = got.takeaway) else it }
            } catch (e: ApiError) {
                if (e.kind == ApiError.Kind.Unauthorized) {
                    onUnauthorized()
                } else {
                    tasks = tasks.map { if (it.id == task.id) it.copy(takeaway = task.takeaway) else it }
                    notice = describe(e)
                    reload++
                }
            }
        }
    }

    fun toggle(task: Task) {
        // 一次只锁这一行：别行还能继续勾，连点同一行也不会叠出两次 PATCH。
        if (busy || task.id in togglingIds) return
        // 勾选也是"点了别处"：删除确认在等的是一个和勾选无关的第二次点击。
        armedId = null
        // 勾的是正改着的那条：先把字存了再勾，否则用户勾完会发现刚才的修改没落。
        if (editingId == task.id) commitEdit()
        // 现取而不是用组合里那份快照：这一行的正文可能刚在 commitEdit 里换过。
        val current = tasks.firstOrNull { it.id == task.id } ?: return
        val toDone = !current.done
        tasks = tasks.relocate(current.copy(done = toDone))
        togglingIds = togglingIds + task.id
        scope.launch {
            notice = null
            try {
                val got = api.setTaskDone(current.id, toDone)
                // 只合完成态那几列，别把正文那条可能还在飞的 PATCH 抹回去。
                val merged = tasks.firstOrNull { it.id == got.id }
                    ?.copy(done = got.done, doneAt = got.doneAt, doneWeekStart = got.doneWeekStart)
                    ?: got
                tasks = tasks.relocate(merged)
                if (merged.done) {
                    offerTakeaway(merged)
                } else if (takeawayId == merged.id) {
                    // 取消勾选不清心得，但正对着它的那条提示已经无话可说。
                    closeTakeaway()
                }
            } catch (e: ApiError) {
                if (e.kind == ApiError.Kind.Unauthorized) {
                    onUnauthorized()
                } else {
                    // 退回完成态，正文保留此刻用户可能已经改过的那份；顺序交给随后的重拉。
                    val reverted = tasks.firstOrNull { it.id == current.id }?.copy(
                        done = current.done,
                        doneAt = current.doneAt,
                        doneWeekStart = current.doneWeekStart,
                    )
                    if (reverted != null) tasks = tasks.relocate(reverted)
                    notice = describe(e)
                    reload++
                }
            } finally {
                togglingIds = togglingIds - task.id
            }
        }
    }

    fun removeTask(task: Task) {
        if (!structuralEnabled) return
        // 删的就是正改着的那条：不必先存一遍再删。
        if (editingId == task.id) cancelEdit()
        if (takeawayId == task.id) closeTakeaway()
        val snapshot = tasks
        armedId = null
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
        if (!structuralEnabled || from == to) return
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

    // 日历弹窗先吃一次返回，别让返回键把整屏编辑一起丢掉。
    BackHandler(enabled = pickingDate) { pickingDate = false }
    // 返回键放弃这次修改；键盘开着时那一下先由系统收键盘。
    BackHandler(enabled = !pickingDate && editingId != null) { cancelEdit() }
    // 删除确认也吃一次返回：正等着第二次点击时，返回键不该直接退出应用。
    BackHandler(enabled = !pickingDate && editingId == null && armedId != null) { armedId = null }
    // 心得编辑开着时，返回键先放弃这句草稿，不退出应用。
    BackHandler(enabled = !pickingDate && editingId == null && armedId == null && takeawayEditing) {
        closeTakeaway()
    }

    val open = tasks.filter { !it.done }
    val done = tasks.filter { it.done }
    val visible = if (expanded) open else open.take(TOP_COUNT)
    // 提示条只认还留在已完成区的那一条：被删掉或取消勾选之后它自动消失。
    val takeawayTask = tasks.firstOrNull { it.id == takeawayId && it.done }
    LaunchedEffect(takeawayTask?.id) {
        if (takeawayId != null && takeawayTask == null) closeTakeaway()
    }

    Column(
        Modifier
            .fillMaxSize()
            // 空白处、标题、发丝线这些没人接的点击也算"点了别处"，撤掉删除确认。
            // 子控件会把各自的点击吃掉，所以这里接到的本来就是剩下那部分。
            .pointerInput(armedId) {
                if (armedId == null) return@pointerInput
                detectTapGestures { armedId = null }
            },
    ) {
        Row(
            Modifier.fillMaxWidth().padding(start = 20.dp, end = 8.dp, top = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            UnderlineField(
                value = draft,
                onValueChange = { draft = it },
                placeholder = "要做的事，回车记下",
                // 点输入框本身就是换了意图：撤掉删除确认，输入与光标照常。
                modifier = Modifier
                    .weight(1f)
                    .dismissOnTouch(armedId != null) { armedId = null },
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                onSubmit = { addTask() },
                textStyle = MaterialTheme.typography.bodyMedium,
            )
            IconAction(
                DeskIcon.Add,
                "加一条",
                enabled = structuralEnabled && draft.isNotBlank(),
                onClick = { addTask() },
            )
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
        loadError?.let {
            Notice(
                it,
                Modifier.padding(horizontal = 20.dp, vertical = 10.dp),
                actionLabel = "重试",
                onAction = { reload++ },
            )
        }

        when {
            loading && tasks.isEmpty() -> Text(
                "读待办…",
                style = MaterialTheme.typography.bodySmall,
                color = InkSoft,
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 24.dp),
            )

            tasks.isEmpty() && loadError != null -> Text(
                "待办没读出来，点上面的重试再读一次",
                style = MaterialTheme.typography.bodyMedium,
                color = InkSoft,
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 28.dp),
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
                        draggable = visible.size > 1 && structuralEnabled,
                        enabled = !busy,
                        structuralEnabled = structuralEnabled,
                        togglePending = task.id in togglingIds,
                        armed = armedId == task.id,
                        editing = editingId == task.id,
                        editDraft = editDraft,
                        editDueOn = editDueOn,
                        onEditStart = { startEdit(task) },
                        onEditChange = { editDraft = it },
                        onEditCommit = { commitEdit() },
                        onPickDate = {
                            // 只调键盘的 hide() 不够：输入框还攥着焦点，日期面板会和键盘抢同一块地方。
                            focusManager.clearFocus(force = true)
                            pickingDate = true
                        },
                        onTakeawayStart = { startTakeaway(task) },
                        onArm = { armedId = task.id },
                        onClear = { armedId = null },
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
                            onClick = {
                                armedId = null
                                expanded = !expanded
                            },
                            enabled = !busy,
                        )
                    }
                }

                if (done.isNotEmpty()) {
                    SectionRule(Modifier.padding(top = 20.dp))
                    Row(
                        Modifier.fillMaxWidth().padding(start = 20.dp, end = 8.dp, top = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            "已完成 ${done.size}",
                            style = MaterialTheme.typography.labelSmall,
                            color = InkSoft,
                            modifier = Modifier.weight(1f),
                        )
                        TextAction(
                            if (doneExpanded) "收起" else "展开",
                            onClick = {
                                if (doneExpanded) {
                                    // 收起时把那一条正在改的字一起收掉，别让编辑态悬在屏外。
                                    if (editingId != null && done.any { it.id == editingId }) cancelEdit()
                                    armedId = null
                                }
                                doneExpanded = !doneExpanded
                            },
                            enabled = !busy,
                        )
                    }
                    if (doneExpanded) {
                        done.forEach { task ->
                            TaskRow(
                                task = task,
                                index = -1,
                                drag = drag,
                                ids = emptyList(),
                                draggable = false,
                                enabled = !busy,
                                structuralEnabled = structuralEnabled,
                                togglePending = task.id in togglingIds,
                                armed = armedId == task.id,
                                editing = editingId == task.id,
                                editDraft = editDraft,
                                editDueOn = editDueOn,
                                onEditStart = { startEdit(task) },
                                onEditChange = { editDraft = it },
                                onEditCommit = { commitEdit() },
                                onPickDate = {
                                    focusManager.clearFocus(force = true)
                                    pickingDate = true
                                },
                                onTakeawayStart = { startTakeaway(task) },
                                onArm = { armedId = task.id },
                                onClear = { armedId = null },
                                onDelete = { removeTask(task) },
                                onToggle = { toggle(task) },
                                onCommit = { _, _ -> },
                            )
                        }
                    }
                }
            }
        }

        takeawayTask?.let { task ->
            Hairline(Modifier.padding(horizontal = 20.dp))
            if (!takeawayEditing) {
                Row(
                    Modifier.fillMaxWidth().padding(start = 20.dp, end = 8.dp, top = 6.dp, bottom = 2.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(Modifier.weight(1f)) {
                        Text(
                            "这条做完了，留一句心得？",
                            style = MaterialTheme.typography.bodyMedium,
                            color = Ink,
                        )
                        Text(
                            task.body,
                            style = MaterialTheme.typography.labelSmall,
                            color = InkSoft,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                    TextAction("先不写", onClick = { closeTakeaway() }, color = InkSoft)
                    TextAction("写一句", onClick = { startTakeaway(task) }, color = Vermilion)
                }
            } else {
                val focus = remember { FocusRequester() }
                LaunchedEffect(task.id) { focus.requestFocus() }
                val takeawayChars = takeawayDraft.codePointCount(0, takeawayDraft.length)
                Column(Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 6.dp)) {
                    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            "心得 · 最多 ${MemoApi.MAX_TAKEAWAY_CHARS} 字",
                            style = MaterialTheme.typography.labelSmall,
                            color = InkSoft,
                            modifier = Modifier.weight(1f),
                        )
                        if (takeawayChars > 40) {
                            Text(
                                "$takeawayChars/${MemoApi.MAX_TAKEAWAY_CHARS}",
                                style = MaterialTheme.typography.labelSmall,
                                color = if (takeawayChars >= MemoApi.MAX_TAKEAWAY_CHARS) Vermilion else InkSoft,
                            )
                        }
                    }
                    UnderlineField(
                        value = takeawayDraft,
                        onValueChange = { takeawayDraft = clampTakeaway(it) },
                        placeholder = "比如：比预期多花半天，评审一次过了",
                        focusRequester = focus,
                        singleLine = false,
                        minLines = 1,
                        maxLines = 3,
                        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                        onSubmit = { saveTakeaway() },
                        textStyle = MaterialTheme.typography.bodyMedium,
                    )
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                        TextAction("取消", onClick = { closeTakeaway() }, color = InkSoft)
                        TextAction("存下", onClick = { saveTakeaway() }, color = Vermilion)
                    }
                }
            }
        }
    }

    if (pickingDate) {
        val fallback = tasks.firstOrNull { it.id == editingId }?.dueOn ?: today().toString()
        DueDateDialog(
            initialDay = editDueOn.ifEmpty { fallback },
            onPick = {
                editDueOn = it
                pickingDate = false
            },
            onDismiss = { pickingDate = false },
        )
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
    structuralEnabled: Boolean,
    togglePending: Boolean,
    armed: Boolean,
    editing: Boolean,
    editDraft: String,
    editDueOn: String,
    onEditStart: () -> Unit,
    onEditChange: (String) -> Unit,
    onEditCommit: () -> Unit,
    onPickDate: () -> Unit,
    onTakeawayStart: () -> Unit,
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
            // 复选框的触摸区是 32dp，视觉仍是 18dp：左边让出 13dp 加上框内留白，
            // 18dp 方框还是落在原来的 20dp 线上，正文也从原来的 49dp 起排。
            Modifier.fillMaxWidth().padding(start = 13.dp, end = 4.dp, top = 5.dp, bottom = 5.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            // 带心得的已完成条目会有三四行，居中会把框浮到心得那一行去；
            // 勾选框认正文第一行，右边那组操作仍随整行居中。
            Box(Modifier.align(Alignment.Top)) {
                TaskCheckbox(checked = task.done, onToggle = onToggle, enabled = enabled)
            }
            Spacer(Modifier.width(4.dp))
            Column(Modifier.weight(1f)) {
                if (editing) {
                    // 点正文就地改：行尾的对勾是显式落定，多行不再靠回车（ADR-0023）。
                    val focus = remember { FocusRequester() }
                    LaunchedEffect(Unit) { focus.requestFocus() }
                    UnderlineField(
                        value = editDraft,
                        onValueChange = onEditChange,
                        placeholder = "改这一句",
                        focusRequester = focus,
                        singleLine = false,
                        minLines = 1,
                        maxLines = 4,
                        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                        onSubmit = onEditCommit,
                        textStyle = MaterialTheme.typography.bodyLarge,
                    )
                    val due = editDueOn.ifEmpty { task.dueOn }
                    TextAction(
                        "改期 · ${dueText(due)}",
                        onClick = onPickDate,
                        enabled = enabled,
                        color = if (isOverdue(due, task.done)) Vermilion else InkSoft,
                    )
                } else {
                    Text(
                        task.body,
                        style = MaterialTheme.typography.bodyLarge,
                        color = if (task.done) InkSoft else Ink,
                        textDecoration = if (task.done) TextDecoration.LineThrough else null,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.clickable(enabled = enabled && !togglePending, onClick = onEditStart),
                    )
                    // 已完成的那条：心得在正文下面，像稿纸上的一行批注；没写的留一个安静的入口。
                    if (task.done) {
                        if (task.takeaway.isNotEmpty()) {
                            TakeawayNote(
                                text = task.takeaway,
                                enabled = enabled && !togglePending,
                                onClick = onTakeawayStart,
                            )
                        } else {
                            TextAction(
                                "补一句心得",
                                onClick = onTakeawayStart,
                                enabled = enabled && !togglePending,
                                color = InkSoft,
                            )
                        }
                    }
                    task.doneAt?.let {
                        // 只给日期：归哪一周是服务端在勾掉那一刻算死的（design §2），
                        // 界面上再精确到分钟也帮不上决定，只会挤掉正文。
                        Text(
                            "完成于 ${formatDay(it)}",
                            style = MaterialTheme.typography.labelSmall,
                            color = InkSoft,
                        )
                    }
                    // 安排日期不是今天才写出来：今天的条目已经站在今天这一屏里，
                    // 再印一遍日期只是噪音。过期的未完成条目标朱，点它直接开改期。
                    if (task.dueOn.isNotEmpty() && task.dueOn != today().toString()) {
                        val overdue = isOverdue(task.dueOn, task.done)
                        Text(
                            if (overdue) "已过期 · ${dueText(task.dueOn)}" else "安排 · ${dueText(task.dueOn)}",
                            style = MaterialTheme.typography.labelSmall,
                            color = if (overdue) Vermilion else InkSoft,
                            modifier = Modifier.clickable(
                                enabled = enabled && !togglePending,
                                onClick = {
                                    onEditStart()
                                    onPickDate()
                                },
                            ),
                        )
                    }
                }
            }
            if (!editing) {
                IconAction(
                    DeskIcon.Delete,
                    if (armed) "确认删" else "删",
                    armed = armed,
                    enabled = structuralEnabled,
                    color = InkSoft,
                    onClick = {
                        if (!armed) onArm() else {
                            onClear()
                            onDelete()
                        }
                    },
                )
                if (draggable) {
                    DragHandle(
                        rowId = task.id,
                        rowIndex = index,
                        active = dragging,
                        onStart = {
                            // 抓起把手也是换了意图，删除确认作废。
                            onClear()
                            drag.heights[task.id]?.let { drag.begin(index, it, ids) }
                        },
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
            } else {
                IconAction(
                    DeskIcon.Check,
                    "保存",
                    enabled = enabled,
                    onClick = onEditCommit,
                )
            }
        }
        Hairline(Modifier.padding(horizontal = 20.dp))
    }
}

// 外圈 32dp 是触摸区，里面 18dp 空心方框才是视觉：手指不用瞄准一个小方格。
// 勾上填墨色再压一道纸色对勾。不用 Material 的 Checkbox：它的圆角底和水波纹跟这片纸不搭（ADR-0013）。
// 对勾是画出来的，不是字符——换字体字符会变成方框。
@Composable
fun TaskCheckbox(checked: Boolean, onToggle: () -> Unit, enabled: Boolean) {
    Box(
        Modifier
            .size(32.dp)
            .toggleable(
                value = checked,
                enabled = enabled,
                role = Role.Checkbox,
                onValueChange = { onToggle() },
            ),
        contentAlignment = Alignment.Center,
    ) {
        Box(
            Modifier
                .size(18.dp)
                .background(if (checked) Ink else Color.Transparent)
                // 直角：印章和方框保持直角，2dp 圆角只留给按钮（ADR-0013）。
                .border(width = if (checked) 0.dp else 1.dp, color = if (enabled) Ink else RuleLine),
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
}

// 只借系统的日期网格，颜色与直角沿用案头这套；选中即回填，落定仍走行尾的对勾。
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun DueDateDialog(
    initialDay: String,
    onPick: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    val state = rememberDatePickerState(
        initialSelectedDateMillis = dayToUtcMillis(initialDay) ?: dayToUtcMillis(today().toString()),
    )
    DatePickerDialog(
        onDismissRequest = onDismiss,
        confirmButton = {
            TextAction(
                "改到这一天",
                onClick = { state.selectedDateMillis?.let { onPick(millisToDay(it)) } },
                enabled = state.selectedDateMillis != null,
            )
        },
        dismissButton = {
            TextAction("取消", onClick = onDismiss, color = InkSoft)
        },
    ) {
        DatePicker(
            state = state,
            title = null,
            headline = null,
            showModeToggle = false,
            colors = DatePickerDefaults.colors(
                containerColor = Paper,
                titleContentColor = Ink,
                headlineContentColor = Ink,
                weekdayContentColor = InkSoft,
                dayContentColor = Ink,
                selectedDayContentColor = Paper,
                selectedDayContainerColor = Vermilion,
                todayContentColor = Vermilion,
                todayDateBorderColor = Vermilion,
            ),
        )
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

// 按码点截住输入，和服务端数的是同一件事（ADR-0027）；多粘贴出来的部分不进草稿。
private fun clampTakeaway(text: String): String {
    if (text.codePointCount(0, text.length) <= MemoApi.MAX_TAKEAWAY_CHARS) return text
    return text.substring(0, text.offsetByCodePoints(0, MemoApi.MAX_TAKEAWAY_CHARS))
}
