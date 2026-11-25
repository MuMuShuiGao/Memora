package com.memora.app

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.unit.dp
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

    Column(
        Modifier.fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            KindButton("自然周", chosen == KIND_WEEK) {
                kind = KIND_WEEK
                viewing = null
                periods = null
                scope.launch { local.setLastKind(KIND_WEEK) }
            }
            KindButton("自然月", chosen == KIND_MONTH) {
                kind = KIND_MONTH
                viewing = null
                periods = null
                scope.launch { local.setLastKind(KIND_MONTH) }
            }
            Spacer(Modifier.weight(1f))
            // 定时归档是服务端在跑的（ADR-0004），手机上没有推送——想看刚生成的那份只能重拉。
            TextButton(onClick = { reload++ }, enabled = !loading) { Text("刷新") }
        }

        notice?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        if (loading) Text("读…")

        periods?.let { p ->
            val span = if (chosen == KIND_WEEK) "${p.weekStart} → ${p.weekEnd}" else "${p.monthStart} → ${p.monthEnd}"
            Text(
                "${if (chosen == KIND_WEEK) "本周" else "本月"} $span　服务端时区 ${p.localTz}",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            ReportCard(
                title = if (viewing == null) "本期" else "周期 $viewing",
                report = shown,
                busy = generating,
                onGenerate = { generate(viewing ?: currentStart) },
                onCopy = { shown?.body?.let { clipboard.setText(AnnotatedString(it)) } },
            )
        }

        Text("生成过的周期", style = MaterialTheme.typography.titleSmall)
        if (reports.isEmpty() && !loading) Text("还没有生成过报表")
        reports.forEach { r ->
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    "${r.periodStart}　${statusText(r.status)}",
                    modifier = Modifier
                        .weight(1f)
                        .clickable { viewing = r.periodStart },
                    color = if (viewing == r.periodStart)
                        MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface,
                )
                OutlinedButton(onClick = { generate(r.periodStart) }, enabled = !generating) {
                    Text("重生成")
                }
            }
        }

        if (viewing != null) {
            OutlinedButton(onClick = { viewing = null }) { Text("回到本期") }
        }
        Spacer(Modifier.height(24.dp))
    }
}

@Composable
private fun KindButton(label: String, on: Boolean, onClick: () -> Unit) {
    if (on) Button(onClick = onClick) { Text(label) } else OutlinedButton(onClick = onClick) { Text(label) }
}

@Composable
private fun ReportCard(
    title: String,
    report: Report?,
    busy: Boolean,
    onGenerate: () -> Unit,
    onCopy: () -> Unit,
) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(title, style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
                if (busy) Text("生成中，最长等一分钟", style = MaterialTheme.typography.bodySmall)
                Button(onClick = onGenerate, enabled = !busy) {
                    Text(if (report?.status == "ready") "重新生成" else "生成")
                }
            }
            when {
                // 未生成是常态而不是错误：本期还在进行中，服务端也没拦着手动生成（ADR-0004）。
                report == null -> Text(
                    "这个周期还没生成过报表。周期没结束也能点上面那个按钮，按现在记下的内容算。",
                    style = MaterialTheme.typography.bodySmall,
                )

                report.status == "ready" -> {
                    Text("用了 ${report.sourceCount ?: 0} 条记录　${formatInstant(report.generatedAt ?: "")}")
                    val body = report.body.orEmpty()
                    if (body.isBlank()) {
                        Text("状态是已生成，但正文是空的")
                    } else {
                        Text(body, style = MaterialTheme.typography.bodyMedium)
                        OutlinedButton(onClick = onCopy, modifier = Modifier.fillMaxWidth()) {
                            Text("复制全文")
                        }
                    }
                }

                report.status == "failed" -> {
                    Text(
                        "上次生成失败：${report.failReason ?: "没留下原因"}",
                        color = MaterialTheme.colorScheme.error,
                    )
                    OutlinedButton(onClick = onGenerate, enabled = !busy) { Text("再试一次") }
                }

                else -> Text("状态：${statusText(report.status)}")
            }
        }
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
