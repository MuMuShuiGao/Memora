package com.memora.app

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

/**
 * 关于页（ADR-0033）：版本、检查更新与更新记录，从抽屉底部进来。
 *
 * 检查更新复用同一个 checker：自动检查的结果、进行中的下载都直接显示；
 * 下载与安装仍在更新页做，这页只给一句状态和一个入口。
 */
@Composable
fun AboutScreen(checker: UpdateChecker, onBack: () -> Unit, onOpenUpdate: () -> Unit) {
    BackHandler(onBack = onBack)
    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 20.dp, vertical = 12.dp),
    ) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text("关于", style = MaterialTheme.typography.titleLarge, color = Ink)
            Spacer(Modifier.weight(1f))
            TextAction("返回", onClick = onBack, color = InkSoft)
        }
        Spacer(Modifier.height(8.dp))
        SectionRule()
        Spacer(Modifier.height(16.dp))

        Column(Modifier.widthIn(max = 640.dp)) {
            Text("Memora", style = MaterialTheme.typography.headlineSmall, color = Ink)
            Spacer(Modifier.height(2.dp))
            Text(
                "v${BuildConfig.VERSION_NAME}（${BuildConfig.VERSION_CODE}） · ${buildTag()}",
                style = MaterialTheme.typography.labelSmall,
                color = InkSoft,
            )
            Spacer(Modifier.height(6.dp))
            Text("记下的想法，会变成周报和月报。", style = MaterialTheme.typography.bodySmall, color = InkSoft)

            Spacer(Modifier.height(20.dp))
            Hairline()
            Spacer(Modifier.height(14.dp))

            Text("版本更新", style = MaterialTheme.typography.labelLarge, color = Ink)
            Spacer(Modifier.height(4.dp))
            UpdateRow(checker, onOpenUpdate)

            Spacer(Modifier.height(18.dp))
            Hairline()
            Spacer(Modifier.height(14.dp))

            Text("更新记录", style = MaterialTheme.typography.labelLarge, color = Ink)
            Spacer(Modifier.height(4.dp))
            APP_RELEASES.forEachIndexed { index, note ->
                if (index > 0) Hairline()
                ReleaseBlock(note)
            }
            Spacer(Modifier.height(10.dp))
            Text(EARLY_RELEASES_LINE, style = MaterialTheme.typography.labelSmall, color = InkSoft)
        }
    }
}

private fun buildTag(): String =
    if (BuildConfig.APPLICATION_ID.endsWith(".local")) "本地构建" else "正式构建"

@Composable
private fun UpdateRow(checker: UpdateChecker, onOpenUpdate: () -> Unit) {
    Row(Modifier.fillMaxWidth().padding(top = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(
            statusText(checker),
            style = MaterialTheme.typography.bodySmall,
            color = if (checker.stage == UpdateStage.Failed) Vermilion else InkSoft,
            modifier = Modifier.weight(1f),
        )
        when (checker.stage) {
            UpdateStage.Checking -> Unit

            UpdateStage.Available, UpdateStage.Downloading, UpdateStage.Downloaded,
            UpdateStage.NeedsPermission, UpdateStage.Installing,
            -> TextAction("去更新", onClick = onOpenUpdate)

            UpdateStage.Failed -> TextAction("重试", onClick = { checker.check() })
            else -> TextAction("检查更新", onClick = { checker.check() })
        }
    }
}

private fun statusText(checker: UpdateChecker): String = when (checker.stage) {
    UpdateStage.Idle, UpdateStage.Checking -> "正在问服务器有没有新版…"
    UpdateStage.UpToDate -> "已是最新，本机 v${BuildConfig.VERSION_NAME}"
    UpdateStage.Installed -> "v${checker.versionName} 刚装好"
    UpdateStage.Available -> "有新版 v${checker.versionName}，${UpdateChecker.aboutMb(checker.sizeBytes)}"
    UpdateStage.Downloading ->
        "正在下载 v${checker.versionName}：" +
            if (checker.percent >= 0) "${checker.percent}%" else UpdateChecker.aboutMb(checker.downloadedBytes)

    UpdateStage.Downloaded -> "v${checker.versionName} 已下载，还差安装"
    UpdateStage.NeedsPermission -> "还要在系统设置里允许 Memora 装应用"
    UpdateStage.Installing -> "等系统安装页装 v${checker.versionName}"
    UpdateStage.Failed -> "更新没成：${checker.error ?: "服务端没答上话"}"
}

@Composable
private fun ReleaseBlock(note: ReleaseNote) {
    Column(Modifier.fillMaxWidth().padding(vertical = 10.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("v${note.version}", style = MaterialTheme.typography.titleSmall, color = Ink)
            Spacer(Modifier.width(8.dp))
            Text(note.date, style = MaterialTheme.typography.labelSmall, color = InkSoft)
        }
        Spacer(Modifier.height(2.dp))
        note.changes.forEach { change ->
            Text(
                "· $change",
                style = MaterialTheme.typography.bodyMedium,
                color = Ink,
                modifier = Modifier.padding(top = 2.dp),
            )
        }
    }
}