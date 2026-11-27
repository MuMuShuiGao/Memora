import java.io.File

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

// 所有变体共用一把签名钥匙：手机上换 debug/release 不用卸载重装，覆盖更新才成立（ADR-0015）。
// keystore 与口令都在仓库外，写在用户级 ~/.gradle/gradle.properties；缺了就走 AGP 默认行为
// （debug 用调试钥匙、release 不签名），刚 clone 下来的人不会被这条卡住。
val keystorePath = providers.gradleProperty("memoraKeystore").orNull?.takeIf { it.isNotBlank() }
val keystorePassword = providers.gradleProperty("memoraKeystorePassword").orNull?.takeIf { it.isNotBlank() }
val memoraKeystoreFile: File? =
    if (keystorePath != null && keystorePassword != null && file(keystorePath).exists()) file(keystorePath) else null

android {
    namespace = "com.memora.app"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.memora.app"
        // Keystore 里的 AES/GCM 从 23 起可用，26 是省掉一堆兼容分支的起点。
        minSdk = 26
        targetSdk = 35
        versionCode = 9
        versionName = "0.9"
    }

    // 两个环境只差两样东西：默认地址，和打进包的那张证书（design/android-client.md §6）。
    flavorDimensions += "env"
    productFlavors {
        create("local") {
            dimension = "env"
            applicationIdSuffix = ".local"
            // 模拟器访问宿主机走这个固定别名地址，指向 Windows 上跑的 memora.exe。
            buildConfigField("String", "DEFAULT_BASE_URL", "\"https://10.0.2.2:8443\"")
        }
        create("server") {
            dimension = "env"
            // 空串：公网地址必须由使用者在连接页填，值既不进仓库也不进 APK（writing-rules §9）。
            buildConfigField("String", "DEFAULT_BASE_URL", "\"\"")
        }
    }

    signingConfigs {
        if (memoraKeystoreFile != null) {
            create("memora") {
                storeFile = memoraKeystoreFile
                storePassword = keystorePassword
                keyAlias = "memora"
                keyPassword = keystorePassword
            }
        }
    }

    buildTypes {
        debug {
            if (memoraKeystoreFile != null) signingConfig = signingConfigs.getByName("memora")
        }
        release {
            // 不做混淆：出问题时"代码看着对但没编译过"这件事已经够难查，再加一层改名不值得。
            isMinifyEnabled = false
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"))
            if (memoraKeystoreFile != null) signingConfig = signingConfigs.getByName("memora")
        }
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }
}

dependencies {
    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.foundation)
    implementation(libs.compose.material3)
    implementation(libs.activity.compose)
    implementation(libs.core.ktx)
    implementation(libs.okhttp)
    implementation(libs.serialization.json)
    implementation(libs.datastore.preferences)
}
