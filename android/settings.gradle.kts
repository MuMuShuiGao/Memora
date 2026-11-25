// 镜像放在前面、官方仓库留在后面兜底：Gradle 在第一个仓库找不到就往下一个试，
// 所以这样写在能直连官方的机器上行为不变，只在连不上的机器上起作用。
// 需求方这边 services.gradle.org 直接连接超时，dl.google.com 与 repo1.maven.org 也不稳。
// 阿里云那几个地址逐个验过能取到 AGP 8.7.3、Kotlin 2.0.21、Compose BOM 2024.12.01 的构件。
//
// pluginManagement 必须是这个文件里的第一个语句，所以 URL 只能就地写，提不成变量。
pluginManagement {
    repositories {
        maven { url = uri("https://maven.aliyun.com/repository/gradle-plugin") }
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
