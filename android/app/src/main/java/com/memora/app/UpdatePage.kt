package com.memora.app

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp

/**
 * 更新页（ADR-0015）：一屏做完"有没有新版—下载—安装"，入口是主屏报头下面那一行提醒。
 *
 * 这页不起后台任务也不轮询；离开再回来用的还是同一次检查结果，因为 checker 挂在 Workspace 上。
 */
@Composable
fun UpdatePage(checker: UpdateChecker, onBack: () -> Unit) {
    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 20.dp, vertical = 12.dp),
    ) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text("更新", style = MaterialTheme.typography.titleLarge, color = Ink)
            Spacer(Modifier.weight(1f))
            TextAction("返回", onClick = onBack, color = InkSoft)
        }
        Spacer(Modifier.height(8.dp))
        SectionRule()
        Spacer(Modifier.height(16.dp))

        Column(Modifier.widthIn(max = 640.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            when (checker.stage) {
                UpdateStage.Idle, UpdateStage.Checking ->
                    Line("正在问服务器有没有新版…", InkSoft)

                UpdateStage.UpToDate -> {
                    Headline("已是最新版")
                    Line("本机 v${BuildConfig.VERSION_NAME}，服务器上没有比它更新的版本。")
                    OutlineAction("重新检查", onClick = { checker.check() })
                }

                UpdateStage.Installed -> {
                    Headline("刚装好 v${BuildConfig.VERSION_NAME}")
                    Line("这一版是从服务器上拉下来装的，之前那个包已经用不上了。")
                    OutlineAction("再查一次", onClick = { checker.check() })
                }

                UpdateStage.Available -> {
                    Headline("v${checker.versionName} 可以装")
                    Line("本机 v${BuildConfig.VERSION_NAME} → v${checker.versionName}，约 ${UpdateChecker.aboutMb(checker.sizeBytes)}。")
                    if (checker.notes.isNotBlank()) Line(checker.notes)
                    PrimaryAction("下载", onClick = { checker.download() })
                }

                UpdateStage.Downloading -> {
                    Headline("正在下载 v${checker.versionName}")
                    Bar(checker.percent)
                    Line(
                        if (checker.percent >= 0)
                            "${checker.percent}%，${UpdateChecker.aboutMb(checker.downloadedBytes)} / ${UpdateChecker.aboutMb(checker.sizeBytes)}"
                        else
                            "已下 ${UpdateChecker.aboutMb(checker.downloadedBytes)}，服务端没报总长",
                    )
                }

                UpdateStage.Downloaded -> {
                    Headline("包已下好")
                    Line("接下来交给系统的安装页。装完这台手机上的记录与待办不会丢——数据在服务器上，本机只存地址和口令。")
                    PrimaryAction("安装", onClick = { checker.install() })
                }

                UpdateStage.NeedsPermission -> {
                    Headline("要先允许 Memora 装应用")
                    Line("刚才应该已经跳到系统设置那一页：打开“允许安装未知应用”，回来再点一次就行。这个开关按应用记，开一次就一直有效。")
                    OutlineAction("我开好了，继续安装", onClick = { checker.install() })
                }

                UpdateStage.Installing -> {
                    Headline("等安装页")
                    Line("在安装页里点“更新”。如果不小心退出来了，可以再叫它一次。")
                    OutlineAction("接着装", onClick = { checker.install() })
                }

                UpdateStage.Failed -> {
                    Headline("更新没成")
                    Notice(checker.error ?: "服务端没答上话")
                    OutlineAction("重试", onClick = { checker.check() })
                }
            }

            Spacer(Modifier.height(6.dp))
            Hairline()
            Line(
                "检查更新只在进主屏时做一次。发版是在服务器上放 app.apk 与 release.json 两个文件，" +
                    "版本号每发一次要往上加一（ADR-0015）。",
                InkSoft,
            )
        }
    }
}

@Composable
private fun Headline(text: String) {
    Text(text, style = MaterialTheme.typography.headlineSmall, color = Ink)
}

@Composable
private fun Line(text: String, color: Color = Ink) {
    Text(text, style = MaterialTheme.typography.bodyMedium, color = color, modifier = Modifier.fillMaxWidth())
}

// 自己画的一根进度线：material3 的 LinearProgressIndicator 签名在小版本间动过，
// 而这里只需要一条朱色细线。
@Composable
private fun Bar(percent: Int) {
    Box(Modifier.fillMaxWidth().height(4.dp).background(RuleLine)) {
        Box(
            Modifier
                .fillMaxWidth(if (percent > 0) percent / 100f else 0f)
                .height(4.dp)
                .background(Vermilion),
        )
    }
}
