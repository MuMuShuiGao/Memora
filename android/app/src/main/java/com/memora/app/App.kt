package com.memora.app

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.Text
import androidx.compose.material3.rememberDrawerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.saveable.rememberSaveableStateHolder
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

private const val SECTION_TASKS = "tasks"
private const val SECTION_CALENDAR = "calendar"
private const val SECTION_NOTES = "notes"
private const val SECTION_BOOKS = "books"
private const val SECTION_REPORTS = "reports"

// 抽屉里的顺序就是日常顺序：待办是落点，报表在最后（ADR-0028）。
private val SECTIONS = listOf(
    SECTION_TASKS to "待办",
    SECTION_CALENDAR to "日历",
    SECTION_NOTES to "记录",
    SECTION_BOOKS to "读书",
    SECTION_REPORTS to "报表",
)

// 编辑页的目标：note 为 null 是新建，否则是改这一条。
data class Editing(val note: Note?)

@Composable
fun AppRoot(local: LocalStore, credentials: CredentialStore) {
    var baseUrl by remember { mutableStateOf<String?>(null) }
    var passphrase by remember { mutableStateOf(credentials.passphrase()) }

    LaunchedEffect(Unit) {
        local.baseUrl.collect { baseUrl = it }
    }

    Box(
        Modifier
            .fillMaxSize()
            .background(Paper)
            .windowInsetsPadding(WindowInsets.safeDrawing),
    ) {
        val url = baseUrl
        val secret = passphrase
        when {
            // DataStore 第一次读要等一帧，这中间不能跳到任何一屏。
            url == null -> Busy("读本地配置…")
            secret.isNullOrBlank() -> ConnectScreen(
                initialUrl = url,
                // 返回 null 表示一切正常，否则是一句要显示给用户的提醒。
                onSave = { newUrl, newSecret ->
                    local.setBaseUrl(newUrl)
                    passphrase = newSecret
                    if (credentials.save(newSecret)) {
                        null
                    } else {
                        "这台设备的 Keystore 存不住口令，能用，但每次启动都要重输一遍"
                    }
                },
            )

            else -> {
                val api = remember(url, secret) { MemoApi(url).also { it.passphrase = secret } }
                Workspace(
                    api = api,
                    local = local,
                    onForgetPassphrase = {
                        // 401 之后不重试、不排队：口令已经是错的了，重试只会喂限速计数（ADR-0002）。
                        credentials.clear()
                        passphrase = null
                    },
                )
            }
        }
    }
}

@Composable
private fun Workspace(api: MemoApi, local: LocalStore, onForgetPassphrase: () -> Unit) {
    var section by rememberSaveable { mutableStateOf(SECTION_TASKS) }
    var editing by remember { mutableStateOf<Editing?>(null) }
    var editingBook by remember { mutableStateOf<Book?>(null) }
    // "正在看更新页"不进 savedInstanceState：装完包进程会被系统换掉，
    // 重启后回到正常首屏，那句"刚装好"由更新页自己的状态带出来。
    var showUpdate by remember { mutableStateOf(false) }
    // 切区块、进编辑页都会把当前屏整块拆掉；把 rememberSaveable 的那部分（选中日、
    // 展开状态、没发出去的草稿）按区块存住，回来时不用重新挑一遍（ADR-0028）。
    val sectionStates = rememberSaveableStateHolder()

    val target = editing
    if (target != null) {
        EditorScreen(
            api = api,
            local = local,
            note = target.note,
            onDone = { editing = null },
            onUnauthorized = onForgetPassphrase,
        )
        return
    }

    val book = editingBook
    if (book != null) {
        BookEditorScreen(
            api = api,
            book = book,
            onDone = { editingBook = null },
            onUnauthorized = onForgetPassphrase,
        )
        return
    }

    // checker 挂在这一层，所以下载中切区块再回来，进度不会丢。
    val update = rememberUpdateChecker(api)
    if (showUpdate) {
        UpdatePage(update, onBack = { showUpdate = false })
        return
    }

    val drawerState = rememberDrawerState(DrawerValue.Closed)
    val scope = rememberCoroutineScope()
    // 抽屉开着时，返回键先合上抽屉，不退出应用。
    BackHandler(enabled = drawerState.isOpen) {
        scope.launch { drawerState.close() }
    }

    ModalNavigationDrawer(
        drawerState = drawerState,
        drawerContent = {
            MemoraDrawer(
                current = section,
                onSelect = { next ->
                    section = next
                    scope.launch { drawerState.close() }
                },
            )
        },
    ) {
        Column(Modifier.fillMaxSize()) {
            Masthead(
                sectionLabel = SECTIONS.firstOrNull { it.first == section }?.second.orEmpty(),
                onMenu = { scope.launch { drawerState.open() } },
            )
            // 更新提醒不弹窗，占报头下面一行，点了才进更新页（ADR-0015）。
            update.hintText()?.let {
                Notice(
                    it,
                    Modifier
                        .padding(horizontal = 20.dp, vertical = 6.dp)
                        .clickable(onClick = { showUpdate = true }),
                )
            }
            sectionStates.SaveableStateProvider(section) {
                when (section) {
                    SECTION_NOTES -> NotesScreen(api, onForgetPassphrase) { note ->
                        editing = Editing(note)
                    }

                    SECTION_CALENDAR -> CalendarScreen(api, onForgetPassphrase)

                    SECTION_BOOKS -> BooksScreen(api, onForgetPassphrase) { picked ->
                        editingBook = picked
                    }

                    SECTION_REPORTS -> ReportsScreen(api, local, onForgetPassphrase)

                    else -> TasksScreen(api, onForgetPassphrase)
                }
            }
        }
    }
}

@Composable
private fun Masthead(sectionLabel: String, onMenu: () -> Unit) {
    Column(Modifier.fillMaxWidth()) {
        Row(
            Modifier.fillMaxWidth().padding(start = 8.dp, end = 20.dp, top = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconAction(DeskIcon.Menu, "打开目录", onClick = onMenu)
            Text(
                "Memora",
                style = MaterialTheme.typography.headlineSmall,
                color = Ink,
                modifier = Modifier.weight(1f).padding(start = 4.dp),
            )
            // 报头右侧写当前区块，和抽屉里的叫法一致，切过去第一眼就知道自己在哪。
            Text(
                sectionLabel,
                style = MaterialTheme.typography.labelMedium,
                color = InkSoft,
            )
        }
        SectionRule(Modifier.padding(top = 4.dp))
    }
}

// 抽屉做得像一页目录：纸色、墨字、当前项压一道朱线，不引导航库（ADR-0010、ADR-0028）。
@Composable
private fun MemoraDrawer(current: String, onSelect: (String) -> Unit) {
    ModalDrawerSheet(
        modifier = Modifier.width(280.dp).fillMaxHeight(),
        drawerShape = RectangleShape,
        drawerContainerColor = Paper,
        drawerTonalElevation = 0.dp,
        // AppRoot 外层已按安全区留过边，这里再留一次会翻倍。
        windowInsets = WindowInsets(0, 0, 0, 0),
    ) {
        Column(Modifier.fillMaxSize().padding(top = 24.dp)) {
            Text(
                "Memora",
                style = MaterialTheme.typography.headlineSmall,
                color = Ink,
                modifier = Modifier.padding(horizontal = 24.dp),
            )
            Text(
                "目录",
                style = MaterialTheme.typography.labelSmall,
                color = InkSoft,
                modifier = Modifier.padding(start = 24.dp, top = 2.dp),
            )
            Hairline(Modifier.padding(horizontal = 24.dp, vertical = 18.dp))
            SECTIONS.forEach { (key, label) ->
                DrawerItem(label, active = key == current) { onSelect(key) }
            }
        }
    }
}

@Composable
private fun DrawerItem(label: String, active: Boolean, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .height(48.dp)
            .clickable(onClick = onClick)
            .padding(horizontal = 24.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            Modifier
                .width(2.dp)
                .height(18.dp)
                .background(if (active) Vermilion else Color.Transparent),
        )
        Spacer(Modifier.width(12.dp))
        Text(
            label,
            style = MaterialTheme.typography.titleMedium,
            color = if (active) Ink else InkSoft,
        )
    }
}

@Composable
fun Busy(text: String) {
    Column(
        Modifier.fillMaxSize().padding(24.dp),
        verticalArrangement = Arrangement.Center,
    ) {
        Text(text, style = MaterialTheme.typography.bodySmall, color = InkSoft)
    }
}