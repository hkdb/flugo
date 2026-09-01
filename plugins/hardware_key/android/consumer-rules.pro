# flugo hardware_key — consumer ProGuard rules. Applied to apps that
# depend on this plugin and have R8/ProGuard enabled (default for
# release builds).
#
# yubikit-android transitively references SpotBugs annotations that
# aren't on the runtime classpath. R8 errors out by default; tell it
# to ignore the missing class.
-dontwarn edu.umd.cs.findbugs.annotations.**

# Keep yubikit public API surface — reflection from our Kotlin code +
# the SDK's own reflective device-feature checks.
-keep class com.yubico.yubikit.** { *; }
-keep interface com.yubico.yubikit.** { *; }
