package com.memora.app

import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// 视觉方向：编辑部案头。备忘是手稿（宋体），报表是排版稿；
// 朱只用于批注/状态，不当装饰色。详见 docs/design/android-client.md 的"视觉"一节。

val Paper = Color(0xFFF3F3ED)
val Ink = Color(0xFF1B1C18)
val InkSoft = Color(0xFF5F6257)
val RuleLine = Color(0xFFCACBBF)
val Vermilion = Color(0xFFB23A2D)

private val Serif = FontFamily.Serif
private val Sans = FontFamily.SansSerif

// 内容（记录、报表正文）用衬线；界面器械（按钮、标签、元信息）用无衬线。
// 小号文字统一用等宽数字，账目式的日期与计数才对得齐。
private fun sans(size: Int, line: Int, weight: FontWeight = FontWeight.Normal) = TextStyle(
    fontFamily = Sans,
    fontWeight = weight,
    fontSize = size.sp,
    lineHeight = line.sp,
    fontFeatureSettings = "tnum",
)

private fun serif(size: Int, line: Int, weight: FontWeight = FontWeight.Normal) = TextStyle(
    fontFamily = Serif,
    fontWeight = weight,
    fontSize = size.sp,
    lineHeight = line.sp,
)

private val MemoraTypography = Typography(
    displaySmall = serif(32, 42, FontWeight.SemiBold),
    headlineSmall = serif(24, 32, FontWeight.SemiBold),
    titleLarge = serif(21, 30, FontWeight.SemiBold),
    titleMedium = serif(18, 26, FontWeight.Medium),
    titleSmall = serif(16, 24, FontWeight.Medium),
    bodyLarge = serif(17, 30),
    bodyMedium = serif(15, 26),
    bodySmall = sans(13, 20),
    labelLarge = sans(14, 20, FontWeight.Medium),
    labelMedium = sans(13, 18, FontWeight.Medium),
    labelSmall = sans(12, 17),
)

private val MemoraColors = lightColorScheme(
    primary = Vermilion,
    onPrimary = Paper,
    primaryContainer = Vermilion,
    onPrimaryContainer = Paper,
    secondary = InkSoft,
    onSecondary = Paper,
    background = Paper,
    onBackground = Ink,
    surface = Paper,
    onSurface = Ink,
    surfaceVariant = Paper,
    onSurfaceVariant = InkSoft,
    outline = RuleLine,
    error = Vermilion,
    onError = Paper,
)

// 圆角只留 2dp 一处档位，印章与输入线保持直角——形状本身也是层级信息。
private val MemoraShapes = Shapes(
    extraSmall = RoundedCornerShape(2.dp),
    small = RoundedCornerShape(2.dp),
    medium = RoundedCornerShape(2.dp),
    large = RoundedCornerShape(2.dp),
    extraLarge = RoundedCornerShape(2.dp),
)

@Composable
fun MemoraTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = MemoraColors,
        typography = MemoraTypography,
        shapes = MemoraShapes,
        content = content,
    )
}