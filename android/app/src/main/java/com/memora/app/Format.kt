package com.memora.app

import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.ZonedDateTime
import java.time.format.DateTimeFormatter

private val TIMESTAMP = DateTimeFormatter.ofPattern("MM-dd HH:mm")
private val DAY = DateTimeFormatter.ofPattern("MM-dd")
private val TIME = DateTimeFormatter.ofPattern("HH:mm")

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