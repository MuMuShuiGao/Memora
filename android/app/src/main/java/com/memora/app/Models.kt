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

@Serializable
data class Periods(
    val now: String = "",
    @SerialName("local_tz") val localTz: String = "",
    @SerialName("week_start") val weekStart: String = "",
    @SerialName("week_end") val weekEnd: String = "",
    @SerialName("month_start") val monthStart: String = "",
    @SerialName("month_end") val monthEnd: String = "",
)
