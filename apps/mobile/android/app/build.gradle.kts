import java.util.Properties

plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

// Release signing.
//
// Shipping a release APK signed with the shared, well-known Android debug key
// means anyone can forge an update for this applicationId, so real releases
// must use a private keystore.
//
// To set one up (the keystore itself is NEVER committed - `key.properties` and
// `*.jks` are gitignored):
//
//   keytool -genkey -v -keystore ~/mcremote-upload.jks -keyalg RSA \
//       -keysize 2048 -validity 10000 -alias upload
//
// then create android/key.properties:
//
//   storeFile=/absolute/path/to/mcremote-upload.jks
//   storePassword=...
//   keyPassword=...
//   keyAlias=upload
//
// Without key.properties the build still works and falls back to the debug key
// so `flutter run --release` keeps working for developers; the fallback logs a
// warning and must not be used for anything distributed.
val keystorePropertiesFile = rootProject.file("key.properties")
val keystoreProperties = Properties().apply {
    if (keystorePropertiesFile.exists()) {
        keystorePropertiesFile.inputStream().use { load(it) }
    }
}
val hasReleaseSigning = keystorePropertiesFile.exists() &&
    keystoreProperties.getProperty("storeFile") != null &&
    keystoreProperties.getProperty("keyAlias") != null

android {
    namespace = "com.maccavelli.magic_cli_remote"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        // Required by flutter_local_notifications (java.time backport on older API).
        isCoreLibraryDesugaringEnabled = true
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "com.maccavelli.magic_cli_remote"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = flutter.minSdkVersion
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    signingConfigs {
        if (hasReleaseSigning) {
            create("release") {
                storeFile = file(keystoreProperties.getProperty("storeFile"))
                storePassword = keystoreProperties.getProperty("storePassword")
                keyAlias = keystoreProperties.getProperty("keyAlias")
                keyPassword = keystoreProperties.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            signingConfig = if (hasReleaseSigning) {
                signingConfigs.getByName("release")
            } else {
                logger.warn(
                    "WARNING: android/key.properties not found - signing the " +
                        "release build with the shared debug key. This build " +
                        "MUST NOT be distributed. See the comment at the top " +
                        "of android/app/build.gradle.kts."
                )
                signingConfigs.getByName("debug")
            }
        }
    }

    // lintVital* is very memory-heavy; disable on small build hosts.
    lint {
        checkReleaseBuilds = false
        abortOnError = false
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

dependencies {
    coreLibraryDesugaring("com.android.tools:desugar_jdk_libs:2.1.4")
}

flutter {
    source = "../.."
}
