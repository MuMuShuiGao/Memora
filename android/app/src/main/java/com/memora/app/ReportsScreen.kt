package com.memora.app

import androidx.compose.foundation.background
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
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.firstOrNull
import kotlinx.coroutines.launch

private const val KIND_WEEK = "week"
private const val KIND_MONTH = "month"

@Composable
fun ReportsScreen(api: MemoApi, local: LocalStore, onUnauthorized: () -> Unit) {
    val scope = rememberCoroutineScope()
    val clipboard = LocalClipboardManager.current

    var kind by remember { mutableStateOf<String?>(null) }
    var periods by remember { mutableStateOf<Periods?>(null) }
    var reports by remember { mutableStateOf<List<Report>>(emptyList()) }
    var current by remember { mutableStateOf<Report?>(null) }
    var currentStart by remember { mutableStateOf("") }
    var viewing by remember { mutableStateOf<String?>(null) }
    var loading by remember { mutableStateOf(true) }
    var generating by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableStateOf(0) }
    var copied by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) {
        kind = local.lastKind.firstOrNull() ?: KIND_WEEK
    }

    // kind 还没从 DataStore 读出来之前不能进下面的 effect——那会先按"周"发一轮请求，
    // 用户上次看的是月。先显示一帧 Busy，读到了再拉数据。
    val chosen = kind
    if (chosen == null) {
        Busy("读本地配置…")
        return
    }

    LaunchedEffect(chosen, reload) {
        loading = true
        notice = null
        try {
            val p = api.periods()
            periods = p
            val start = if (chosen == KIND_WEEK) p.weekStart else p.monthStart
            currentStart = start
            // 本期这条单独取：它多半还没生成，getReport 回 404 not_generated，
            // 那不是错误，是"这个周期还没生成过"这个事实，界面要照常往下画。
            current = runCatching { api.getReport(chosen, start) }
                .getOrElse { e ->
                    if (e is ApiError && e.kind == ApiError.Kind.NotGenerated) null else throw e
                }
            reports = api.listReports(chosen).reports
            viewing = null
        } catch (e: ApiError) {
            if (e.kind == ApiError.Kind.Unauthorized) onUnauthorized() else notice = describe(e)
        } finally {
            loading = false
        }
    }

    fun generate(periodStart: String) {
        scope.launch {
            generating = true
            notice = null
            try {
                val r = api.generateReport(chosen, periodStart)
                if (periodStart == currentStart) current = r
                reports = api.listReports(chosen).reports
            } catch (e: ApiError) {
                when (e.kind) {
                    ApiError.Kind.Unauthorized -> onUnauthorized()
                    // 生成失败必须把 fail_reason 摊开显示（FR-05）：只说"失败了"等于没说。
                    else -> notice = describe(e)
                }
                if (periodStart == currentStart) {
                    current = e.failedReport ?: current
                }
            } finally {
                generating = false
            }
        }
    }

    val shown = when {
        viewing == null -> current
        else -> reports.firstOrNull { it.periodStart == viewing }
    }

    LaunchedEffect(shown?.periodStart) { copied = false }
    LaunchedEffect(copied) {
        if (copied) {
            delay(2000)
            copied = false
        }
    }

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 20.dp),
    ) {
        Row(
            Modifier.fillMaxWidth().padding(top = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            UnderlineTab("自然周", chosen == KIND_WEEK) {
                kind = KIND_WEEK
                viewing = null
                periods = null
                scope.launch { local.setLastKind(KIND_WEEK) }
            }
            Spacer(Modifier.width(20.dp))
            UnderlineTab("自然月", chosen == KIND_MONTH) {
                kind = KIND_MONTH
                viewing = null
                periods = null
                scope.launch { local.setLastKind(KIND_MONTH) }
            }
            Spacer(Modifier.weight(1f))
            // 定时归档是服务端在跑的（ADR-0004），手机上没有推送——想看刚生成的那份只能重拉。
            TextAction("刷新", onClick = { reload++ }, enabled = !loading)
        }

        notice?.let {
            Notice(it, Modifier.padding(top = 12.dp))
        }

        if (loading && periods == null) {
            Text(
                "读报表…",
                style = MaterialTheme.typography.bodySmall,
                color = InkSoft,
                modifier = Modifier.padding(top = 16.dp),
            )
        }

        periods?.let { p ->
            val start = if (chosen == KIND_WEEK) p.weekStart else p.monthStart
            val end = if (chosen == KIND_WEEK) p.weekEnd else p.monthEnd

            Column(Modifier.padding(top = 18.dp)) {
                Text(
                    "${if (chosen == KIND_WEEK) "本周" else "本月"} $start → $end",
                    style = MaterialTheme.typography.titleMedium,
                    color = Ink,
                )
                Text(
                    "按服务端时区 ${p.localTz} 归档",
                    style = MaterialTheme.typography.labelSmall,
                    color = InkSoft,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }

            Row(
                Modifier.fillMaxWidth().padding(top = 26.dp),
                verticalAlignment = Alignment.Top,
            ) {
                Text(
                    if (viewing == null) "本期报表" else "$viewing 那期",
                    style = MaterialTheme.typography.titleLarge,
                    color = Ink,
                    modifier = Modifier.weight(1f),
                )
                shown?.let { StatusStamp(statusText(it.status), statusColor(it.status)) }
            }
            SectionRule(Modifier.padding(top = 10.dp))

            Box(Modifier.padding(top = 14.dp)) {
                when {
                    // 未生成是常态而不是错误：本期还在进行中，服务端也没拦着手动生成（ADR-0004）。
                    shown == null -> Text(
                        "这个周期还没生成过报表。点「生成」按现在记下的内容写一份。",
                        style = MaterialTheme.typography.bodyMedium,
                        color = InkSoft,
                    )

                    shown.status == "ready" -> Column {
                        Text(
                            "用了 ${shown.sourceCount ?: 0} 条记录，生成于 ${formatInstant(shown.generatedAt ?: "")}",
                            style = MaterialTheme.typography.labelSmall,
                            color = InkSoft,
                        )
                        val body = shown.body.orEmpty()
                        if (body.isBlank()) {
                            Text(
                                "状态是已生成，但正文是空的",
                                style = MaterialTheme.typography.bodyMedium,
                                color = InkSoft,
                                modifier = Modifier.padding(top = 12.dp),
                            )
                        } else {
                            // 报表是"见刊稿"：单栏、宋体、行距放宽，只让正文占据版面。
                            Text(
                                body,
                                style = MaterialTheme.typography.bodyLarge,
                                color = Ink,
                                modifier = Modifier.padding(top = 12.dp),
                            )
                            OutlineAction(
                                if (copied) "已复制" else "复制全文",
                                onClick = {
                                    clipboard.setText(AnnotatedString(body))
                                    copied = true
                                },
                                modifier = Modifier.padding(top = 18.dp),
                            )
                        }
                    }

                    shown.status == "failed" -> Text(
                        "上次生成失败：${shown.failReason ?: "没留下原因"}",
                        style = MaterialTheme.typography.bodyMedium,
                        color = Vermilion,
                    )

                    else -> Text(
                        "状态：${statusText(shown.status)}",
                        style = MaterialTheme.typography.bodyMedium,
                        color = InkSoft,
                    )
                }
            }

            Row(
                Modifier.fillMaxWidth().padding(top = 20.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (generating) {
                    Text(
                        "正在生成，最长等一分钟",
                        style = MaterialTheme.typography.labelSmall,
                        color = InkSoft,
                    )
                }
                Spacer(Modifier.weight(1f))
                PrimaryAction(
                    when {
                        generating -> "生成中…"
                        shown != null -> "重新生成"
                        else -> "生成"
                    },
                    onClick = { generate(viewing ?: currentStart) },
                    enabled = !generating,
                )
            }
        }

        Text(
            "生成过的周期",
            style = MaterialTheme.typography.titleMedium,
            color = Ink,
            modifier = Modifier.padding(top = 32.dp),
        )
        if (reports.isEmpty() && !loading) {
            Text(
                "还没有生成过报表",
                style = MaterialTheme.typography.bodyMedium,
                color = InkSoft,
                modifier = Modifier.padding(top = 8.dp),
            )
        }
        reports.forEach { r ->
            Row(
                Modifier
                    .fillMaxWidth()
                    .clickable { viewing = r.periodStart }
                    .padding(vertical = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                // 选中哪一期由页边的一条朱线标记，不靠加粗或底色。
                Box(
                    Modifier
                        .width(2.dp)
                        .height(26.dp)
                        .background(if (viewing == r.periodStart) Vermilion else Color.Transparent),
                )
                Spacer(Modifier.width(12.dp))
                Column(Modifier.weight(1f)) {
                    Text(
                        r.periodStart,
                        style = MaterialTheme.typography.bodySmall,
                        color = if (viewing == r.periodStart) Ink else InkSoft,
                    )
                    Text(
                        statusText(r.status),
                        style = MaterialTheme.typography.labelSmall,
                        color = if (r.status == "failed") Vermilion else InkSoft,
                    )
                }
                TextAction(
                    "重生成",
                    onClick = { generate(r.periodStart) },
                    enabled = !generating,
                )
            }
            Hairline()
        }

        if (viewing != null) {
            TextAction(
                "回到本期",
                onClick = { viewing = null },
                color = InkSoft,
                modifier = Modifier.padding(top = 8.dp),
            )
        }
        Spacer(Modifier.height(24.dp))
    }
}

private fun statusText(status: String) = when (status) {
    "ready" -> "已生成"
    "failed" -> "失败"
    "running" -> "生成中"
    "pending" -> "待生成"
    "" -> "无"
    else -> status
}

// 朱色只落在"已经发生过的事"（生成好/失败）上；半途状态用淡墨。
private fun statusColor(status: String) =
    if (status == "ready" || status == "failed") Vermilion else InkSoft