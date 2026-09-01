// flugo hardware_key plugin — Android module
//
// Wraps Yubico's yubikit-android `yubiotp` module for HMAC-SHA1
// challenge-response over NFC and USB-OTG. Slot programming is out of
// scope — users program slot 2 via Yubico Authenticator, ykman, or
// KeePassXC before plugging in.

plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.flugo.hardware_key"
    compileSdk = 34

    defaultConfig {
        minSdk = 21
        consumerProguardFiles("consumer-rules.pro")
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
    // yubikit-android — current as of 2026-03 release v3.1.0.
    // The `yubiotp` module pulls in `core` and `android` transitively.
    implementation("com.yubico.yubikit:yubiotp:3.1.0")
    implementation("com.yubico.yubikit:android:3.1.0")

    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.7.3")
}
