// 镜像放在前面、官方仓库留在后面兜底：Gradle 在第一个仓库找不到就往下一个试，
// 所以这样写在能直连官方的机器上行为不变，只在连不上的机器上起作用。
// 阿里云两个地址各管一片，逐个验过：google 那个有 AGP 8.7.3 和全部 androidx 构件、但没有 Kotlin；
// public 那个有 Kotlin 2.0.21 的四个插件标记和 okhttp、kotlinx-serialization。
// 不加 gradle-plugin 那个：它能取到的 public 全都能取到，反而在 AGP 标记上 404，只会多一次空跑。
//
// pluginManagement 必须是这个文件里的第一个语句，所以 URL 只能就地写，提不成变量。
pluginManagement {
    repositories {
        maven { url = uri("https://maven.aliyun.com/repository/google") }
        maven { url = uri("https://maven.aliyun.com/repository/public") }
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        maven { url = uri("https://maven.aliyun.com/repository/google") }
        maven { url = uri("https://maven.aliyun.com/repository/public") }
        google()
        mavenCentral()
    }
}

rootProject.name = "Memora"
include(":app")
