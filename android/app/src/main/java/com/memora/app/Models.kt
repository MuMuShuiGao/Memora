package com.memora.app

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// 字段与服务端 JSON 一一对应（docs/design/memo-and-report.md §5）。
// 服务端多出 created_at / updated_at 等字段不影响：解析时忽略未知键。

@Serializable
data class Note(
    val id: Long,
    val body: String,
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("week_start") val weekStart: String = "",
    @SerialName("month_start") val monthStart: String = "",
)

@Serializable
data class NoteList(val notes: List<Note> = emptyList(), val count: Int = 0)

// 待办（ADR-0014）：rank 是服务端未完成序列里的名次，客户端只当展示顺序用。
// doneAt 可空——没勾掉的就没有完成时刻，报表归属跟着它走。
@Serializable
data class Task(
    val id: Long,
    val body: String,
    val done: Boolean = false,
    val rank: Int = 0,
    @SerialName("due_on") val dueOn: String = "",
    // 完成心得（ADR-0027）：空串就是没写；取消勾选不会清掉它。
    val takeaway: String = "",
    @SerialName("done_at") val doneAt: String? = null,
    @SerialName("done_week_start") val doneWeekStart: String = "",
    // 阶段（ADR-0030）：随任务一起返回，按加入顺序，最多 5 个。
    val stages: List<Stage> = emptyList(),
)

// 阶段是待办下面的汇报点：没有日期、没有名次，勾掉按当周进报表。
@Serializable
data class Stage(
    val id: Long,
    @SerialName("task_id") val taskId: Long = 0,
    val body: String = "",
    val done: Boolean = false,
    @SerialName("done_at") val doneAt: String? = null,
    val takeaway: String = "",
)

@Serializable
data class TaskList(
    val tasks: List<Task> = emptyList(),
    @SerialName("open_count") val openCount: Int = 0,
    @SerialName("done_count") val doneCount: Int = 0,
)

// body / sourceCount / failReason / generatedAt 在服务端是"可能没有值"的，
// 这里的可空必须保留：ready 但正文为空串，和还没生成好，是界面要分开的两件事（FR-05）。
@Serializable
data class Report(
    val id: Long = 0,
    val kind: String = "",
    @SerialName("period_start") val periodStart: String = "",
    @SerialName("period_end") val periodEnd: String = "",
    val status: String = "",
    val body: String? = null,
    @SerialName("source_count") val sourceCount: Int? = null,
    @SerialName("fail_reason") val failReason: String? = null,
    @SerialName("generated_at") val generatedAt: String? = null,
)

@Serializable
data class ReportList(val reports: List<Report> = emptyList(), val count: Int = 0)

// 读过的书（ADR-0029）：finishedMonth 是字面年月 YYYY-MM，不跟时间戳换算时区。
// 作者与心得都可空：空串就是没填，心得清空也不删这本书。
@Serializable
data class Book(
    val id: Long,
    val title: String,
    val author: String = "",
    @SerialName("finished_month") val finishedMonth: String = "",
    val takeaway: String = "",
)

@Serializable
data class BookList(val books: List<Book> = emptyList(), val count: Int = 0)

// 更新通道（ADR-0015）：versionCode 是唯一的判据，服务端没有已发布版本时这个类根本不出现。
// size 由服务端 stat 出来，不是发布时手填的，所以"约多少 MB"不会和真实文件对不上。
@Serializable
data class ReleaseInfo(
    @SerialName("version_code") val versionCode: Int = 0,
    @SerialName("version_name") val versionName: String = "",
    val notes: String = "",
    val size: Long = 0,
)

@Serializable
data class Periods(
    val now: String = "",
    @SerialName("local_tz") val localTz: String = "",
    @SerialName("week_start") val weekStart: String = "",
    @SerialName("week_end") val weekEnd: String = "",
    @SerialName("month_start") val monthStart: String = "",
    @SerialName("month_end") val monthEnd: String = "",
)
