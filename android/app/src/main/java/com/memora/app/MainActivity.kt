package com.memora.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.core.view.WindowCompat

class MainActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // 纸色一直铺到状态栏和手势条后面；深色图标是固定的，不跟随系统深色模式。
        enableEdgeToEdge()
        WindowCompat.getInsetsController(window, window.decorView).apply {
            isAppearanceLightStatusBars = true
            isAppearanceLightNavigationBars = true
        }

        val localStore = LocalStore(this)
        val credentials = CredentialStore(this)

        setContent {
            MemoraTheme {
                AppRoot(localStore, credentials)
            }
        }
    }
}