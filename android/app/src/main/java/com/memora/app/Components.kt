package com.memora.app

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.spring
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.sin

@Composable
fun Hairline(modifier: Modifier = Modifier, color: Color = RuleLine) {
    HorizontalDivider(modifier = modifier, thickness = 1.dp, color = color)
}

// 2dp 墨线把页面切成"报头 / 正文"，是结构而不是装饰。
@Composable
fun SectionRule(modifier: Modifier = Modifier) {
    HorizontalDivider(modifier = modifier, thickness = 2.dp, color = Ink)
}

// 账目式输入：没有方框，只有一条基线；聚焦时线变重。
@Composable
fun UnderlineField(
    value: String,
    onValueChange: (String) -> Unit,
    modifier: Modifier = Modifier,
    label: String? = null,
    placeholder: String = "",
    singleLine: Boolean = true,
    minLines: Int = 1,
    maxLines: Int = if (singleLine) 1 else Int.MAX_VALUE,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
    // 回车即交出去那条。默认什么都不做，所以搜索框那类调用方不必管它。
    onSubmit: () -> Unit = {},
    visualTransformation: VisualTransformation = VisualTransformation.None,
    textStyle: TextStyle = MaterialTheme.typography.bodyLarge,
    // 提示默认跟正文同权；日报这类写字屏要把它降一级，只做空表单时的引子。
    placeholderStyle: TextStyle = textStyle,
    focusRequester: FocusRequester? = null,
) {
    val interactions = remember { MutableInteractionSource() }
    val focused by interactions.collectIsFocusedAsState()
    // 多行框的提示贴第一行：居中会在 minLines > 1 时漂到中间，与首行光标错位（ADR-0034）。
    val placeholderAtTop = !singleLine && minLines > 1

    Column(modifier) {
        if (label != null) {
            Text(label, style = MaterialTheme.typography.labelSmall, color = InkSoft)
            Spacer(Modifier.height(2.dp))
        }
        BasicTextField(
            value = value,
            onValueChange = onValueChange,
            singleLine = singleLine,
            minLines = minLines,
            maxLines = maxLines,
            textStyle = textStyle.copy(color = Ink),
            cursorBrush = SolidColor(Vermilion),
            keyboardOptions = keyboardOptions,
            keyboardActions = KeyboardActions(onDone = { onSubmit() }),
            visualTransformation = visualTransformation,
            interactionSource = interactions,
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = 40.dp)
                .padding(top = 8.dp)
                .then(if (focusRequester != null) Modifier.focusRequester(focusRequester) else Modifier),
            decorationBox = { inner ->
                Box(
                    contentAlignment = if (placeholderAtTop) Alignment.TopStart else Alignment.CenterStart,
                ) {
                    if (value.isEmpty() && placeholder.isNotEmpty()) {
                        Text(
                            placeholder,
                            style = placeholderStyle,
                            color = InkSoft.copy(alpha = 0.6f),
                            maxLines = if (singleLine) 1 else 2,
                        )
                    }
                    inner()
                }
            },
        )
        Spacer(Modifier.height(4.dp))
        HorizontalDivider(
            thickness = if (focused) 2.dp else 1.dp,
            color = if (focused) Ink else RuleLine,
        )
    }
}

@Composable
fun PrimaryAction(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    Button(
        onClick = onClick,
        modifier = modifier,
        enabled = enabled,
        shape = MaterialTheme.shapes.small,
        colors = ButtonDefaults.buttonColors(
            containerColor = Vermilion,
            contentColor = Paper,
            disabledContainerColor = RuleLine.copy(alpha = 0.6f),
            disabledContentColor = InkSoft,
        ),
        elevation = ButtonDefaults.buttonElevation(
            defaultElevation = 0.dp,
            pressedElevation = 0.dp,
            disabledElevation = 0.dp,
        ),
        contentPadding = PaddingValues(horizontal = 24.dp, vertical = 14.dp),
    ) {
        Text(text, style = MaterialTheme.typography.labelLarge)
    }
}

@Composable
fun OutlineAction(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    OutlinedButton(
        onClick = onClick,
        modifier = modifier,
        enabled = enabled,
        shape = MaterialTheme.shapes.small,
        border = BorderStroke(1.dp, if (enabled) Ink else RuleLine),
        colors = ButtonDefaults.outlinedButtonColors(
            contentColor = Ink,
            disabledContentColor = InkSoft.copy(alpha = 0.5f),
        ),
        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 10.dp),
    ) {
        Text(text, style = MaterialTheme.typography.labelMedium)
    }
}

@Composable
fun TextAction(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    color: Color = Ink,
) {
    TextButton(
        onClick = onClick,
        modifier = modifier,
        enabled = enabled,
        colors = ButtonDefaults.textButtonColors(
            contentColor = color,
            disabledContentColor = InkSoft.copy(alpha = 0.5f),
        ),
        contentPadding = PaddingValues(horizontal = 8.dp, vertical = 0.dp),
    ) {
        Text(text, style = MaterialTheme.typography.labelMedium)
    }
}

enum class DeskIcon { Add, Refresh, Delete, Prev, Next, Check, Menu }

// 按进这块区域的任何地方都先撤掉"等第二次确认"的删除。走 Initial 阶段只看不吃：
// 输入框的光标、选区与点击照常；挂在外层，因为子控件会把 Main 阶段的点击吃掉。
fun Modifier.dismissOnTouch(active: Boolean, dismiss: () -> Unit): Modifier = pointerInput(active) {
    if (!active) return@pointerInput
    awaitPointerEventScope {
        while (true) {
            awaitPointerEvent(PointerEventPass.Initial)
            dismiss()
        }
    }
}

// "加 / 刷新 / 删"这类短动词换成线条符号，省下的横向宽度还给正文。
// 字形自绘而不用 material-icons：那套是实心字形，笔画和这片发丝线不是一套东西
// （同 ADR-0013 不用 Material Checkbox 的理由）。名称走 contentDescription，
// 屏幕上不再出现这几个字。
@Composable
fun IconAction(
    icon: DeskIcon,
    label: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    color: Color = Ink,
    // 删除要连点两次（design §交互）：第一次靠盖一枚朱色方框改变外观，而不是改成另一个字。
    armed: Boolean = false,
) {
    val tint = when {
        !enabled -> InkSoft.copy(alpha = 0.45f)
        armed -> Vermilion
        else -> color
    }
    Box(
        modifier
            .size(40.dp)
            .then(if (armed) Modifier.border(1.5.dp, Vermilion) else Modifier)
            .semantics { contentDescription = label }
            .clickable(enabled = enabled, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Canvas(Modifier.size(20.dp)) {
            when (icon) {
                DeskIcon.Add -> addGlyph(tint)
                DeskIcon.Refresh -> refreshGlyph(tint)
                DeskIcon.Delete -> deleteGlyph(tint)
                DeskIcon.Prev -> chevronGlyph(tint, left = true)
                DeskIcon.Next -> chevronGlyph(tint, left = false)
                DeskIcon.Check -> checkGlyph(tint)
                DeskIcon.Menu -> menuGlyph(tint)
            }
        }
    }
}

private fun DrawScope.addGlyph(color: Color) {
    val s = size.width
    val w = s * 0.09f
    drawLine(color, Offset(s * 0.5f, s * 0.16f), Offset(s * 0.5f, s * 0.84f), w)
    drawLine(color, Offset(s * 0.16f, s * 0.5f), Offset(s * 0.84f, s * 0.5f), w)
}

// 圆留 60° 缺口，弧头朝切线方向压出一枚实心三角。
private fun DrawScope.refreshGlyph(color: Color) {
    val s = size.width
    val w = s * 0.09f
    val r = s * 0.38f
    val c = Offset(s / 2f, s / 2f)
    drawArc(
        color,
        startAngle = 0f,
        sweepAngle = 300f,
        useCenter = false,
        topLeft = Offset(c.x - r, c.y - r),
        size = Size(r * 2f, r * 2f),
        style = Stroke(width = w),
    )
    val a = 300.0 * PI / 180.0
    val nx = cos(a).toFloat()
    val ny = sin(a).toFloat()
    val px = c.x + r * nx
    val py = c.y + r * ny
    // 顺时针走到这一点时的切线
    val tx = -ny
    val ty = nx
    val head = Path().apply {
        moveTo(px + tx * s * 0.22f, py + ty * s * 0.22f)
        lineTo(px - tx * s * 0.05f + nx * s * 0.16f, py - ty * s * 0.05f + ny * s * 0.16f)
        lineTo(px - tx * s * 0.05f - nx * s * 0.16f, py - ty * s * 0.05f - ny * s * 0.16f)
        close()
    }
    drawPath(head, color)
}

// 行内编辑的保存键。和勾选框里的对勾同一笔势，只是放大到图标尺度。
private fun DrawScope.checkGlyph(color: Color) {
    val s = size.width
    val w = s * 0.09f
    drawLine(color, Offset(s * 0.18f, s * 0.52f), Offset(s * 0.42f, s * 0.76f), w, cap = StrokeCap.Round)
    drawLine(color, Offset(s * 0.42f, s * 0.76f), Offset(s * 0.84f, s * 0.26f), w, cap = StrokeCap.Round)
}

// 日历翻月用的尖角，只有两笔，线宽与其它图标一致。
private fun DrawScope.chevronGlyph(color: Color, left: Boolean) {
    val s = size.width
    val w = s * 0.09f
    val near = if (left) 0.62f else 0.38f
    val far = if (left) 0.38f else 0.62f
    drawLine(color, Offset(s * near, s * 0.2f), Offset(s * far, s * 0.5f), w, cap = StrokeCap.Round)
    drawLine(color, Offset(s * far, s * 0.5f), Offset(s * near, s * 0.8f), w, cap = StrokeCap.Round)
}

// 目录图标：三条等长横线。线距与拖拽把手同一笔势，只是收在 20dp 字形里。
private fun DrawScope.menuGlyph(color: Color) {
    val s = size.width
    val w = s * 0.09f
    listOf(0.22f, 0.5f, 0.78f).forEach { y ->
        drawLine(color, Offset(s * 0.14f, s * y), Offset(s * 0.86f, s * y), w)
    }
}

// 盖、提手、上宽下窄的桶身加两道竖纹：轮廓是开口的，跟输入线、发丝线一样只描边。
private fun DrawScope.deleteGlyph(color: Color) {
    val s = size.width
    val w = s * 0.08f
    drawLine(color, Offset(s * 0.15f, s * 0.24f), Offset(s * 0.85f, s * 0.24f), w)
    val handle = Path().apply {
        moveTo(s * 0.37f, s * 0.24f)
        lineTo(s * 0.37f, s * 0.13f)
        lineTo(s * 0.63f, s * 0.13f)
        lineTo(s * 0.63f, s * 0.24f)
    }
    drawPath(handle, color, style = Stroke(width = w))
    val body = Path().apply {
        moveTo(s * 0.26f, s * 0.32f)
        lineTo(s * 0.31f, s * 0.87f)
        lineTo(s * 0.69f, s * 0.87f)
        lineTo(s * 0.74f, s * 0.32f)
    }
    drawPath(body, color, style = Stroke(width = w))
    drawLine(color, Offset(s * 0.42f, s * 0.44f), Offset(s * 0.44f, s * 0.76f), w * 0.8f)
    drawLine(color, Offset(s * 0.58f, s * 0.44f), Offset(s * 0.56f, s * 0.76f), w * 0.8f)
}

// 心得是给翻回去看的人留的：衬线小字，左侧一道朱线，点它再改（ADR-0027、ADR-0029）。
// maxLines 只截显示，不截内容；打开编辑页能看到全文。
@Composable
fun TakeawayNote(
    text: String,
    enabled: Boolean,
    onClick: () -> Unit,
    maxLines: Int = Int.MAX_VALUE,
) {
    Text(
        text,
        style = MaterialTheme.typography.bodyMedium,
        color = InkSoft,
        maxLines = maxLines,
        overflow = TextOverflow.Ellipsis,
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 4.dp)
            .clickable(enabled = enabled, onClickLabel = "改这句心得", onClick = onClick)
            .drawBehind {
                drawRect(
                    color = Vermilion,
                    size = Size(2.dp.toPx(), size.height),
                )
            }
            .padding(start = 10.dp, top = 2.dp, bottom = 2.dp),
    )
}

// 页签用"墨线 + 朱色底线"表达当前位置，不用 Material 的药丸按钮。
// 主导航已收进抽屉（ADR-0028），这个只服务报表屏里的周/月切换。
@Composable
fun UnderlineTab(text: String, active: Boolean, onClick: () -> Unit) {
    // IntrinsicSize.Max 让底面朱线只跟文字同宽；不加的话 fillMaxWidth 会把整个页签撑满。
    Column(
        modifier = Modifier
            .height(48.dp)
            .width(IntrinsicSize.Max)
            .clickable(onClick = onClick),
        verticalArrangement = Arrangement.Bottom,
    ) {
        Text(
            text,
            style = MaterialTheme.typography.labelLarge,
            color = if (active) Ink else InkSoft,
            fontWeight = if (active) FontWeight.SemiBold else FontWeight.Normal,
        )
        Spacer(Modifier.height(6.dp))
        Box(
            Modifier
                .fillMaxWidth()
                .height(2.dp)
                .background(if (active) Vermilion else Color.Transparent),
        )
    }
}

// 报表状态盖成一枚朱印。状态变化时"按下"一次，是页面上唯一的主动画。
@Composable
fun StatusStamp(text: String, color: Color, modifier: Modifier = Modifier) {
    val scale = remember { Animatable(1f) }
    LaunchedEffect(text) {
        scale.snapTo(1.1f)
        scale.animateTo(1f, spring(dampingRatio = 0.6f, stiffness = 600f))
    }
    Box(
        modifier
            .graphicsLayer {
                scaleX = scale.value
                scaleY = scale.value
            }
            .rotate(-3f)
            .border(1.5.dp, color)
            .padding(horizontal = 10.dp, vertical = 3.dp),
    ) {
        Text(text, style = MaterialTheme.typography.labelMedium, color = color)
    }
}

// 错误与提醒不弹窗，在发生的位置盖一枚小朱点。加载失败时把"重试"留在同一行，
// 不另起一个常驻刷新按钮（ADR-0019 的刷新口径不变）。
@Composable
fun Notice(
    text: String,
    modifier: Modifier = Modifier,
    actionLabel: String? = null,
    onAction: (() -> Unit)? = null,
) {
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.Top) {
        Box(
            Modifier
                .padding(top = 6.dp)
                .width(6.dp)
                .height(6.dp)
                .background(Vermilion),
        )
        Spacer(Modifier.width(8.dp))
        Text(
            text,
            style = MaterialTheme.typography.labelMedium,
            color = Vermilion,
            modifier = Modifier.weight(1f),
        )
        if (actionLabel != null && onAction != null) {
            TextAction(actionLabel, onClick = onAction, color = Vermilion)
        }
    }
}