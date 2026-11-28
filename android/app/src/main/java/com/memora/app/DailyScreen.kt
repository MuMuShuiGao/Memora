package com.memora.app

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
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
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.time.temporal.ChronoUnit

/**
 * 日报屏（ADR-0031、ADR-0032）：一天一页交接班记录，三段手写，停顿 1.5 秒自动保存。
 * 日期、今天与右箭头边界都取服务端 periods/now 的 today，不拿手机日期另算一份。
 */
@Composable
fun DailyScreen(
    api: MemoApi,
    onUnauthorized: () -> Unit,
    jumpDay: String? = null,
    onJumpConsumed: () -> Unit = {},
) {
    val scope = rememberCoroutineScope()
    val focusManager = LocalFocusManager.current
    // 两个 PUT 不并发：先到的写完，后到的在锁里重新比一次基线，没变化就不发。
    val saveMutex = remember { Mutex() }

    var today by remember { mutableStateOf<String?>(null) }
    // 草稿与"已保存的那份"都进 saveable：切区块回来不该被重拉覆盖，也不该误报未保存。
    var day by rememberSaveable { mutableStateOf("") }
    var loadedFor by rememberSaveable { mutableStateOf("") }
    var priorities by rememberSaveable { mutableStateOf("") }
    var doneText by rememberSaveable { mutableStateOf("") }
    var risk by rememberSaveable { mutableStateOf("") }
    var savedPriorities by rememberSaveable { mutableStateOf("") }
    var savedDone by rememberSaveable { mutableStateOf("") }
    var savedRisk by rememberSaveable { mutableStateOf("") }

    var tasks by remember { mutableStateOf<List<Task>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    var saving by remember { mutableStateOf(false) }
    var saveError by remember { mutableStateOf<String?>(null) }
    var savedAt by remember { mutableStateOf<String?>(null) }
    var justCleared by remember { mutableStateOf(false) }
    var loadError by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableStateOf(0) }

    fun isDirty(): Boolean =
        priorities.trim() != savedPriorities ||
            doneText.trim() != savedDone ||
            risk.trim() != savedRisk

    // 自动保存一次全量三段。成功只把这次发出去的文字作为新基线，不回写正在编辑的草稿；
    // 失败保留输入，等下一次输入或点重试才再发，不自动无限重试。
    suspend fun flushNow(): Boolean {
        val target = day
        if (target.isEmpty() || loadedFor != target) return true
        return saveMutex.withLock {
            if (day != target || loadedFor != target) return@withLock true
            val p = priorities.trim()
            val d = doneText.trim()
            val r = risk.trim()
            val hadContent = savedPriorities.isNotEmpty() || savedDone.isNotEmpty() || savedRisk.isNotEmpty()
            if (p == savedPriorities && d == savedDone && r == savedRisk) return@withLock true
            // 从没写过的空表单不产生请求；从有字清到全空才发一次。
            if (p.isEmpty() && d.isEmpty() && r.isEmpty() && !hadContent) return@withLock true
            saving = true
            try {
                api.putDaily(target, p, d, r)
                savedPriorities = p
                savedDone = d
                savedRisk = r
                saveError = null
                savedAt = clockNow()
                justCleared = hadContent && p.isEmpty() && d.isEmpty() && r.isEmpty()
                true
            } catch (e: ApiError) {
                if (e.kind == ApiError.Kind.Unauthorized) {
                    onUnauthorized()
                } else {
                    saveError = describe(e)
                }
                false
            } finally {
                saving = false
            }
        }
    }

    fun requestSave() {
        scope.launch { flushNow() }
    }

    suspend fun loadDay(target: String): Boolean {
        loading = true
        loadError = null
        return try {
            val d = api.getDaily(target)
            priorities = d?.priorities.orEmpty()
            doneText = d?.done.orEmpty()
            risk = d?.risk.orEmpty()
            savedPriorities = priorities
            savedDone = doneText
            savedRisk = risk
            day = target
            loadedFor = target
            saveError = null
            savedAt = null
            justCleared = false
            true
        } catch (e: ApiError) {
            if (e.kind == ApiError.Kind.Unauthorized) {
                onUnauthorized()
            } else {
                loadError = describe(e)
            }
            false
        } finally {
            loading = false
        }
    }

    LaunchedEffect(reload) {
        loading = true
        loadError = null
        try {
            val p = api.periods()
            today = p.today
            val target = jumpDay?.takeIf { it.isNotEmpty() } ?: day.ifEmpty { p.today }
            if (target.isEmpty()) {
                loadError = "服务端没有给出今天"
                return@LaunchedEffect
            }
            // 抄入动作要用最新顺序，每次进屏都重拉；拿到手就换掉上一份。
            tasks = api.listTasks().tasks
            // 从日历跳过来时先把原来那天的字补存，失败就留在原页，不静默丢字。
            if (loadedFor != target && isDirty() && !flushNow()) {
                onJumpConsumed()
                return@LaunchedEffect
            }
            if (loadedFor != target) {
                loadDay(target)
            }
            if (!jumpDay.isNullOrEmpty()) {
                onJumpConsumed()
            }
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

    // 输入停顿 1.5 秒落一次盘；保存中又有输入，就等当前请求回来后由锁补一次。
    val formReady = day.isNotEmpty() && loadedFor == day
    LaunchedEffect(priorities, doneText, risk, formReady, day) {
        if (!formReady || !isDirty()) return@LaunchedEffect
        delay(1500)
        requestSave()
    }

    fun openDay(target: String) {
        if (loading) return
        if (target == day && loadedFor == target) return
        focusManager.clearFocus()
        scope.launch {
            // 还有在飞的保存就在锁里等它回来；失败留在原页，不静默丢字。
            if (isDirty() && !flushNow()) return@launch
            loadDay(target)
        }
    }

    fun shiftDay(delta: Long) {
        if (loading || !formReady) return
        val current = parseDay(day) ?: return
        val next = current.plusDays(delta)
        val ceiling = parseDay(today.orEmpty()) ?: return
        if (next.isAfter(ceiling)) return
        openDay(next.toString())
    }

    fun bringTasks() {
        val lines = tasks.filter { !it.done }.take(5).map { it.body }
        if (lines.isEmpty()) return
        priorities = clampCodePoints(mergeLines(priorities, lines), MemoApi.MAX_DAILY_CHARS)
    }

    fun bringDone() {
        val picked = day
        if (picked.isEmpty()) return
        val taskLines = tasks
            .filter { it.done && localDayOf(it.doneAt.orEmpty()) == picked }
            .map { it.body }
        val stageLines = tasks.flatMap { task ->
            task.stages.filter { it.done && localDayOf(it.doneAt.orEmpty()) == picked }
        }.map { it.body }
        val lines = taskLines + stageLines
        if (lines.isEmpty()) return
        doneText = clampCodePoints(mergeLines(doneText, lines), MemoApi.MAX_DAILY_CHARS)
    }

    val ceiling = today.orEmpty()
    if (ceiling.isEmpty()) {
        if (loadError != null) {
            Notice(
                loadError.orEmpty(),
                Modifier.padding(horizontal = 20.dp, vertical = 16.dp),
                actionLabel = "重试",
                onAction = { reload++ },
            )
        } else {
            Busy("读这一天…")
        }
        return
    }

    val dirty = isDirty()
    val canBringTasks = tasks.any { !it.done }
    val canBringDone = tasks.any { task ->
        (task.done && localDayOf(task.doneAt.orEmpty()) == day) ||
            task.stages.any { it.done && localDayOf(it.doneAt.orEmpty()) == day }
    }
    val date = parseDay(day)
    val dateTitle = if (date != null) dayTitle(date) else day
    val relative = relativeDay(day, ceiling)
    val nothingWritten = priorities.isBlank() && doneText.isBlank() && risk.isBlank()

    Column(Modifier.fillMaxSize()) {
        Row(
            Modifier.fillMaxWidth().padding(start = 8.dp, end = 8.dp, top = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconAction(
                DeskIcon.Prev,
                "前一天",
                enabled = !loading && formReady,
                onClick = { shiftDay(-1) },
            )
            Column(
                Modifier.weight(1f),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                if (relative != null) {
                    Text(
                        relative,
                        style = MaterialTheme.typography.labelSmall,
                        color = InkSoft,
                    )
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(dateTitle, style = MaterialTheme.typography.titleLarge, color = Ink)
                    if (relative != null) {
                        Spacer(Modifier.width(4.dp))
                        TextAction(
                            "回今天",
                            onClick = { openDay(ceiling) },
                            enabled = !loading && formReady,
                            color = Vermilion,
                        )
                    }
                }
            }
            IconAction(
                DeskIcon.Next,
                "后一天",
                enabled = !loading && formReady && day < ceiling,
                onClick = { shiftDay(1) },
            )
        }
        Hairline(Modifier.padding(top = 10.dp))

        loadError?.let {
            Notice(
                it,
                Modifier.padding(horizontal = 20.dp, vertical = 10.dp),
                actionLabel = "重试",
                onAction = { reload++ },
            )
        }

        when {
            !formReady && loading -> Text(
                "读这一天…",
                style = MaterialTheme.typography.bodySmall,
                color = InkSoft,
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 24.dp),
            )

            !formReady -> Text(
                "这一天没读出来，点上面的重试再读一次",
                style = MaterialTheme.typography.bodyMedium,
                color = InkSoft,
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 28.dp),
            )

            else -> {
                Column(
                    Modifier
                        .weight(1f)
                        .fillMaxWidth()
                        .verticalScroll(rememberScrollState()),
                ) {
                    if (nothingWritten) {
                        Text(
                            "这天还没落笔",
                            style = MaterialTheme.typography.labelSmall,
                            color = InkSoft,
                            modifier = Modifier.padding(start = 20.dp, top = 14.dp),
                        )
                    }
                    DailySection(
                        label = "今天要紧的",
                        hint = "一两件就够",
                        value = priorities,
                        onValueChange = {
                            priorities = clampCodePoints(it, MemoApi.MAX_DAILY_CHARS)
                        },
                        actionLabel = if (canBringTasks) "从待办抄五条" else null,
                        actionEnabled = !loading,
                        onAction = { bringTasks() },
                    )
                    DailySection(
                        label = "做成了什么",
                        hint = "写结果就行，不用交代过程",
                        value = doneText,
                        onValueChange = {
                            doneText = clampCodePoints(it, MemoApi.MAX_DAILY_CHARS)
                        },
                        actionLabel = if (canBringDone) "抄今天勾掉的" else null,
                        actionEnabled = !loading,
                        onAction = { bringDone() },
                    )
                    DailySection(
                        label = "卡在哪儿了",
                        hint = "在等什么、什么没做完；没做完的明天接着来",
                        value = risk,
                        onValueChange = {
                            risk = clampCodePoints(it, MemoApi.MAX_DAILY_CHARS)
                        },
                    )
                    Spacer(Modifier.height(16.dp))
                }

                when {
                    saving -> Text(
                        "正在记…",
                        style = MaterialTheme.typography.labelSmall,
                        color = InkSoft,
                        modifier = Modifier.padding(horizontal = 20.dp, vertical = 10.dp),
                    )

                    saveError != null -> Notice(
                        "没记下 · " + saveError.orEmpty(),
                        Modifier.padding(horizontal = 20.dp, vertical = 4.dp),
                        actionLabel = "重试",
                        onAction = { requestSave() },
                    )

                    dirty -> Unit

                    justCleared -> Text(
                        "这天的素材已清掉",
                        style = MaterialTheme.typography.labelSmall,
                        color = InkSoft,
                        modifier = Modifier.padding(horizontal = 20.dp, vertical = 10.dp),
                    )

                    savedAt != null -> Text(
                        "已记下 $savedAt",
                        style = MaterialTheme.typography.labelSmall,
                        color = InkSoft,
                        modifier = Modifier.padding(horizontal = 20.dp, vertical = 10.dp),
                    )
                }
            }
        }
    }
}

// 三段共用同一副骨架：无衬线小节标题 + 右侧朱色抄入动作 + 多行正文 + 输入线。
// 提示比正文低一级：13/20 无衬线，不与 17/30 的衬线正文抢注意力。
@Composable
private fun DailySection(
    label: String,
    hint: String,
    value: String,
    onValueChange: (String) -> Unit,
    actionLabel: String? = null,
    actionEnabled: Boolean = true,
    onAction: () -> Unit = {},
) {
    val titleStyle = MaterialTheme.typography.labelLarge.copy(fontSize = 15.sp, lineHeight = 21.sp)
    Column(Modifier.fillMaxWidth()) {
        Row(
            Modifier
                .fillMaxWidth()
                .padding(start = 20.dp, end = 12.dp, top = 18.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(label, style = titleStyle, color = Ink)
            Spacer(Modifier.weight(1f))
            if (actionLabel != null) {
                TextAction(
                    actionLabel,
                    onClick = onAction,
                    enabled = actionEnabled,
                    color = Vermilion,
                )
            }
        }
        UnderlineField(
            value = value,
            onValueChange = onValueChange,
            placeholder = hint,
            singleLine = false,
            minLines = 3,
            maxLines = 8,
            textStyle = MaterialTheme.typography.bodyLarge,
            placeholderStyle = MaterialTheme.typography.bodySmall,
            modifier = Modifier.padding(horizontal = 20.dp),
        )
    }
}

// 今天不写相对词；补记才用「昨天 / 前天 / N 天前」把距离说在前头。
private fun relativeDay(day: String, today: String): String? {
    val d = parseDay(day) ?: return null
    val t = parseDay(today) ?: return null
    val diff = ChronoUnit.DAYS.between(d, t)
    return when {
        diff <= 0L -> null
        diff == 1L -> "昨天"
        diff == 2L -> "前天"
        else -> "$diff 天前"
    }
}

// 抄入只补还没出现的行，不覆盖已经写下的字；抄完即断链，之后改待办不回头改日报。
private fun mergeLines(existing: String, incoming: List<String>): String {
    val have = existing.lineSequence()
        .map { it.trim() }
        .filter { it.isNotEmpty() }
        .toMutableSet()
    val additions = incoming
        .map { it.trim() }
        .filter { it.isNotEmpty() && have.add(it) }
    if (additions.isEmpty()) return existing
    val base = existing.trimEnd()
    return if (base.isEmpty()) {
        additions.joinToString("\n")
    } else {
        base + "\n" + additions.joinToString("\n")
    }
}
