package com.memora.app

import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter

private val TIMESTAMP = DateTimeFormatter.ofPattern("MM-dd HH:mm")

// 服务端给的是 UTC 时间戳（RFC3339Nano），界面按手机时区显示。
// 但归属周期绝不这么算——那是服务端算好返回的，见 docs/design/android-client.md。
fun formatInstant(iso: String): String =
    try {
        OffsetDateTime.parse(iso).atZoneSameInstant(ZoneId.systemDefault()).format(TIMESTAMP)
    } catch (e: Exception) {
        iso
    }
