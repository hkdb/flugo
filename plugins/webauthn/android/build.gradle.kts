// flugo webauthn plugin — Android module
//
// Pivoting to a Play-Services-FREE CTAP2 client (works on GrapheneOS without
// GApps): drives ANY security key over USB/NFC directly via yubikit-android's
// `fido` module (the same library family hardware_key already ships). yubikit
// provides the CTAP2 protocol engine (Ctap2Session + BasicWebAuthnClient with
// clientPIN/PIN-UV crypto); we supply VID-agnostic USB/NFC discovery + build
// clientDataJSON like the desktop libfido2 authenticator (self-asserted origin,
// so no assetlinks/association needed). No Google Play Services.

plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.flugo.webauthn"
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
    // Play-Services-free CTAP2 over USB/NFC (any brand). Pinned to match
    // hardware_key's yubikit version; bump all three together if `fido` isn't
    // published at this version.
    implementation("com.yubico.yubikit:android:3.1.0")
    implementation("com.yubico.yubikit:fido:3.1.0")
    implementation("com.yubico.yubikit:core:3.1.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.7.3")
}
