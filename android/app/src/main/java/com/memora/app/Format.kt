package com.memora.app

import java.time.Instant
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.YearMonth
import java.time.ZoneOffset
import java.time.ZonedDateTime
import java.time.format.DateTimeFormatter
import java.time.temporal.WeekFields

private val TIMESTAMP = DateTimeFormatter.ofPattern("MM-dd HH:mm")
private val DAY = DateTimeFormatter.ofPattern("MM-dd")
private val TIME = DateTimeFormatter.ofPattern("HH:mm")

private val WEEKDAYS = arrayOf("周一", "周二", "周三", "周四", "周五", "周六", "周日")

// 服务端给的是 UTC 时间戳（RFC3339Nano），界面按手机时区显示。
// 但归属周期绝不这么算——那是服务端算好返回的，见 docs/design/android-client.md。
private fun parse(iso: String): ZonedDateTime? =
    try {
        OffsetDateTime.parse(iso).atZoneSameInstant(ZoneId.systemDefault())
    } catch (e: Exception) {
        null
    }

fun formatInstant(iso: String): String = parse(iso)?.format(TIMESTAMP) ?: iso

fun formatDay(iso: String): String = parse(iso)?.format(DAY) ?: iso

fun formatTime(iso: String): String = parse(iso)?.format(TIME) ?: ""

// 完成时刻落在手机本地哪一天。只服务日报的「带入当天完成」，
// 不拿它推周期归属——那是服务端算好的（ADR-0031）。
fun localDayOf(iso: String): String? = parse(iso)?.toLocalDate()?.toString()

// 安排日期是字面自然日，不跟时间戳一起换算时区（ADR-0021）。
fun today(): LocalDate = LocalDate.now()

fun parseDay(day: String): LocalDate? =
    try {
        LocalDate.parse(day)
    } catch (e: Exception) {
        null
    }

fun weekdayLabel(date: LocalDate): String = WEEKDAYS[date.dayOfWeek.value - 1]

fun dayTitle(date: LocalDate): String = "${date.monthValue}月${date.dayOfMonth}日 ${weekdayLabel(date)}"

// 待办页正文下那一行：今天的条目不用再写一遍日期，其余用相对词优先。
fun dueText(day: String): String {
    val date = parseDay(day) ?: return day
    val now = LocalDate.now()
    return when (date) {
        now -> "今天"
        now.plusDays(1) -> "明天"
        now.minusDays(1) -> "昨天"
        else -> {
            val year = if (date.year == now.year) "" else "${date.year}年"
            "$year${date.monthValue}月${date.dayOfMonth}日 ${weekdayLabel(date)}"
        }
    }
}

fun isoWeek(date: LocalDate): Int = date.get(WeekFields.ISO.weekOfWeekBasedYear())

// 已勾掉的不算过期：它已经不需要再被安排了。
fun isOverdue(day: String, done: Boolean): Boolean {
    if (done) return false
    val date = parseDay(day) ?: return false
    return date.isBefore(LocalDate.now())
}

// Material3 的日期选择器按 UTC 零点记"哪一天"，和 due_on 的字面自然日同一口径。
fun dayToUtcMillis(day: String): Long? =
    parseDay(day)?.atStartOfDay(ZoneOffset.UTC)?.toInstant()?.toEpochMilli()

fun millisToDay(millis: Long): String =
    Instant.ofEpochMilli(millis).atZone(ZoneOffset.UTC).toLocalDate().toString()

// 读完月份是字面年月（ADR-0029）：按手机当前年月给，不换算时区，也不由服务端缺省。
fun nowMonth(): String = YearMonth.now().toString()

fun parseMonth(month: String): YearMonth? =
    try {
        YearMonth.parse(month)
    } catch (e: Exception) {
        null
    }

fun monthTitle(month: String): String {
    val ym = parseMonth(month) ?: return month
    return "${ym.year}年${ym.monthValue}月"
}

fun monthShort(month: String): String {
    val ym = parseMonth(month) ?: return month
    return "${ym.monthValue}月"
}

// 按码点截住输入，和服务端数的是同一件事（ADR-0029/0031）；多粘贴出来的部分不进草稿。
fun clampCodePoints(text: String, max: Int): String {
    if (text.codePointCount(0, text.length) <= max) return text
    return text.substring(0, text.offsetByCodePoints(0, max))
}