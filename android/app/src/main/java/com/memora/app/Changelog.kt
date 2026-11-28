package com.memora.app

/**
 * 关于页的更新记录（ADR-0033）。写死在包里：离线可看、跟版本走；
 * 发版时在列表顶上补一条，版本名与 build.gradle.kts 的 versionName 对齐。
 */
data class ReleaseNote(
    val version: String,
    val date: String,
    val changes: List<String>,
)

val APP_RELEASES = listOf(
    ReleaseNote(
        version = "0.12",
        date = "2026-11-28",
        changes = listOf(
            "日报停笔片刻自动记下，翻日期或从日历跳走不再丢字",
            "日历上多了日报墨点，点开就落到那天",
            "日报的措辞换成写给自己的话",
        ),
    ),
    ReleaseNote(
        version = "0.11",
        date = "2026-11-28",
        changes = listOf(
            "新增手写日报：今天要紧的、做成了什么、卡在哪儿了",
            "日报按日期进入周报月报的素材",
        ),
    ),
    ReleaseNote(
        version = "0.10",
        date = "2026-11-28",
        changes = listOf("长待办可以挂阶段，勾掉一个就写进当周报表"),
    ),
    ReleaseNote(
        version = "0.9",
        date = "2026-11-28",
        changes = listOf(
            "新增读书清单：书名、作者、读完年月与一句心得",
            "主屏导航改成左侧目录抽屉",
        ),
    ),
)

// 0.1–0.8 挤在发版前两天里，逐条列只会变成流水账，合并一句。
const val EARLY_RELEASES_LINE = "0.1–0.8 · 记录、待办、日历、报表与更新通道的初版和打磨"