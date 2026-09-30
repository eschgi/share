import java.util.Properties

plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

// Release signing comes from android/key.properties, which is never committed:
//   storePassword=…  keyPassword=…  keyAlias=…  storeFile=/path/to/share-release.jks
// One key for both flavors, so a direct APK and the Play build can replace each other.
val keyProperties = Properties().apply {
    val file = rootProject.file("key.properties")
    if (file.exists()) file.inputStream().use { load(it) }
}

// The scheme of the invite link the invite page hands to the app; the server's
// app.link_scheme must say the same.
val linkScheme = "com.eschgi.share"

android {
    namespace = "com.eschgi.share"
    compileSdk = 37
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        applicationId = "com.eschgi.share"
        minSdk = 29
        // Pinned rather than flutter.targetSdkVersion: each Android release changes the rules
        // for background work and local network access, and moving to a new one is a decision.
        targetSdk = 36
        // From pubspec.yaml (version: name+code), so there is one place to bump.
        versionCode = flutter.versionCode
        versionName = flutter.versionName
        manifestPlaceholders["linkScheme"] = linkScheme
        buildConfigField("String", "LINK_SCHEME", "\"$linkScheme\"")
    }

    buildFeatures {
        buildConfig = true
    }

    // direct: the APK the server hands out, which updates itself. play: the Google Play build,
    // without the permission to install packages. Same application id, so either replaces
    // the other.
    flavorDimensions += "store"
    productFlavors {
        create("direct") { dimension = "store" }
        create("play") { dimension = "store" }
    }

    signingConfigs {
        if (keyProperties.getProperty("storeFile") != null) {
            create("release") {
                storeFile = file(keyProperties.getProperty("storeFile"))
                storePassword = keyProperties.getProperty("storePassword")
                keyAlias = keyProperties.getProperty("keyAlias")
                keyPassword = keyProperties.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        debug {
            // Installs next to a release build (and is named "Share (debug)" in src/debug).
            applicationIdSuffix = ".debug"
        }
        release {
            // No fallback to the debug key: an unsigned release build fails instead.
            signingConfig = signingConfigs.findByName("release")
        }
    }

    testOptions {
        unitTests.isReturnDefaultValues = true
    }

    // The language can be picked in the app, so an app bundle must keep all of them rather
    // than only the phone's (the notifications are in the picked one).
    bundle {
        language {
            enableSplit = false
        }
    }
}

gradle.taskGraph.whenReady {
    if (allTasks.any { it.name.startsWith("assemble") && it.name.endsWith("Release") || it.name.startsWith("bundle") && it.name.endsWith("Release") } &&
        android.signingConfigs.findByName("release") == null
    ) {
        throw GradleException("Release builds need android/key.properties with the release key.")
    }
}

dependencies {
    // Downloads (and later uploads) on Android 10 to 13 run as WorkManager foreground work;
    // from 14 on as user-initiated data transfer jobs.
    implementation("androidx.work:work-runtime:2.10.5")
    implementation("androidx.core:core-ktx:1.17.0")
    // The photo picker, with its fallbacks on older phones, and the document picker.
    implementation("androidx.activity:activity:1.13.0")
    // Which way up a photo is, for the thumbnail sent along (HEIF and WebP too).
    implementation("androidx.exifinterface:exifinterface:1.4.2")
    // Scanning invite QR codes without the camera permission; phones without Google Play
    // services paste the link instead.
    implementation("com.google.android.gms:play-services-code-scanner:16.1.0")

    testImplementation("junit:junit:4.13.2")
    // The stock Android JVM test runtime stubs org.json to throw, so the real implementation
    // has to be on the unit-test classpath.
    testImplementation("org.json:json:20250517")
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}
