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
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.time.temporal.ChronoUnit

/**
 * 日报屏（ADR-0031）：一天一页交接班记录，三段手写。
 * 日期、今天与右箭头边界都取服务端 periods/now 的 today，不拿手机日期另算一份。
 */
@Composable
fun DailyScreen(api: MemoApi, onUnauthorized: () -> Unit) {
    val scope = rememberCoroutineScope()
    val focusManager = LocalFocusManager.current

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
    var notice by remember { mutableStateOf<String?>(null) }
    var loadError by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableStateOf(0) }
    var savedFlash by remember { mutableStateOf(false) }
    var flashKey by remember { mutableStateOf(0) }

    LaunchedEffect(flashKey) {
        if (flashKey > 0) {
            savedFlash = true
            delay(2000)
            savedFlash = false
        }
    }

    LaunchedEffect(reload) {
        loading = true
        loadError = null
        try {
            val p = api.periods()
            today = p.today
            val target = day.ifEmpty { p.today }
            if (target.isEmpty()) {
                loadError = "服务端没有给出今天"
                return@LaunchedEffect
            }
            day = target
            // 带入按钮要用最新顺序，每次进屏都重拉；拿到手就换掉上一份。
            tasks = api.listTasks().tasks
            if (loadedFor != target) {
                val d = api.getDaily(target)
                priorities = d?.priorities.orEmpty()
                doneText = d?.done.orEmpty()
                risk = d?.risk.orEmpty()
                savedPriorities = priorities
                savedDone = doneText
                savedRisk = risk
                loadedFor = target
            }
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

    fun isDirty(): Boolean =
        priorities.trim() != savedPriorities ||
            doneText.trim() != savedDone ||
            risk.trim() != savedRisk

    fun openDay(target: String) {
        focusManager.clearFocus()
        day = target
        loadedFor = ""
        loadError = null
        notice = null
        scope.launch {
            loading = true
            try {
                val d = api.getDaily(target)
                priorities = d?.priorities.orEmpty()
                doneText = d?.done.orEmpty()
                risk = d?.risk.orEmpty()
                savedPriorities = priorities
                savedDone = doneText
                savedRisk = risk
                loadedFor = target
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
    }

    fun shiftDay(delta: Long) {
        if (loading) return
        val current = parseDay(day) ?: return
        val next = current.plusDays(delta)
        val ceiling = parseDay(today.orEmpty()) ?: return
        if (next.isAfter(ceiling)) return
        if (isDirty()) {
            notice = "先把这天存下，再翻日期"
            return
        }
        openDay(next.toString())
    }

    fun save() {
        if (saving || loading || day.isEmpty()) return
        focusManager.clearFocus()
        scope.launch {
            saving = true
            notice = null
            try {
                val got = api.putDaily(day, priorities.trim(), doneText.trim(), risk.trim())
                // 服务端会 trim，把回包写回草稿与基线，免得"存完还是未保存"。
                priorities = got.priorities
                doneText = got.done
                risk = got.risk
                savedPriorities = got.priorities
                savedDone = got.done
                savedRisk = got.risk
                flashKey++
            } catch (e: ApiError) {
                if (e.kind == ApiError.Kind.Unauthorized) {
                    onUnauthorized()
                } else {
                    notice = describe(e)
                }
            } finally {
                saving = false
            }
        }
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

    val formReady = day.isNotEmpty() && loadedFor == day
    val dirty = isDirty()
    val canBringTasks = tasks.any { !it.done }
    val canBringDone = tasks.any { task ->
        (task.done && localDayOf(task.doneAt.orEmpty()) == day) ||
            task.stages.any { it.done && localDayOf(it.doneAt.orEmpty()) == day }
    }
    val date = parseDay(day)
    val dateTitle = if (date != null) dayTitle(date) else day
    val relative = relativeDay(day, ceiling)

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
                Text(dateTitle, style = MaterialTheme.typography.titleLarge, color = Ink)
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
                    DailySection(
                        label = "优先级",
                        hint = "今天最重要的 1–3 件事",
                        value = priorities,
                        onValueChange = {
                            priorities = clampCodePoints(it, MemoApi.MAX_DAILY_CHARS)
                        },
                        actionLabel = "从待办带入",
                        actionEnabled = canBringTasks && !loading,
                        onAction = { bringTasks() },
                    )
                    DailySection(
                        label = "今天做成了什么",
                        hint = "做成了什么，结果优先",
                        value = doneText,
                        onValueChange = {
                            doneText = clampCodePoints(it, MemoApi.MAX_DAILY_CHARS)
                        },
                        actionLabel = "带入当天完成",
                        actionEnabled = canBringDone && !loading,
                        onAction = { bringDone() },
                    )
                    DailySection(
                        label = "卡点与没做完的",
                        hint = "卡在谁、等什么决定；没做完的挪到明天",
                        value = risk,
                        onValueChange = {
                            risk = clampCodePoints(it, MemoApi.MAX_DAILY_CHARS)
                        },
                    )
                    Spacer(Modifier.height(16.dp))
                }

                notice?.let {
                    Notice(it, Modifier.padding(horizontal = 20.dp, vertical = 6.dp))
                }
                Row(
                    Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    PrimaryAction(
                        if (saving) "存…" else "存",
                        onClick = { save() },
                        enabled = !saving && dirty,
                    )
                    Spacer(Modifier.width(12.dp))
                    when {
                        saving -> Text(
                            "正在存…",
                            style = MaterialTheme.typography.labelSmall,
                            color = InkSoft,
                        )

                        savedFlash -> Text(
                            "已保存",
                            style = MaterialTheme.typography.labelSmall,
                            color = Vermilion,
                        )

                        dirty -> Text(
                            "有改动未保存",
                            style = MaterialTheme.typography.labelSmall,
                            color = InkSoft,
                        )
                    }
                }
            }
        }
    }
}

// 三段共用同一副骨架：无衬线小节标题 + 右侧朱色带入动作 + 多行正文 + 输入线。
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
    Column(Modifier.fillMaxWidth()) {
        Row(
            Modifier
                .fillMaxWidth()
                .padding(start = 20.dp, end = 12.dp, top = 18.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(label, style = MaterialTheme.typography.labelLarge, color = Ink)
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
            modifier = Modifier.padding(horizontal = 20.dp),
        )
    }
}

// 今天不写相对词；补记才用「昨天 / 前两天 / N 天前」把距离说在前头。
private fun relativeDay(day: String, today: String): String? {
    val d = parseDay(day) ?: return null
    val t = parseDay(today) ?: return null
    val diff = ChronoUnit.DAYS.between(d, t)
    return when {
        diff <= 0L -> null
        diff == 1L -> "昨天"
        diff == 2L -> "前两天"
        else -> "$diff 天前"
    }
}

// 带入只补还没出现的行，不覆盖已经写下的字；抄完即断链，之后改待办不回头改日报。
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