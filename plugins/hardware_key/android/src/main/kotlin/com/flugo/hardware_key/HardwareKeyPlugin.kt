// flugo hardware_key — Android plugin.
//
// Bridges a Flutter MethodChannel to com.yubico.yubikit:yubiotp for
// HMAC-SHA1 challenge-response. Listens for NFC and USB-OTG YubiKey
// devices while the plugin is attached to a foreground activity; the
// MethodChannel call sets pending challenge state that the tag callback
// consumes when a device arrives.
//
// Why discovery runs at activity lifecycle (not on-demand):
//   Engaging NFC reader mode on-demand inside the method handler
//   creates a race window where the user's tap arrives BEFORE
//   enableReaderMode has taken effect. The system's default NDEF
//   dispatch then wins and opens the YubiKey's default URL in the
//   browser. Reference impls (Yubico AndroidDemo, Kunzisoft, yubikey_
//   flutter) all engage reader mode in onResume / onListen for the same
//   reason. We mirror that pattern, tied to ActivityAware attach/detach.

package com.flugo.hardware_key

import android.content.Context
import android.nfc.NfcAdapter
import android.os.Handler
import android.os.Looper
import io.flutter.embedding.engine.plugins.FlutterPlugin
import io.flutter.embedding.engine.plugins.activity.ActivityAware
import io.flutter.embedding.engine.plugins.activity.ActivityPluginBinding
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel

import com.yubico.yubikit.android.YubiKitManager
import com.yubico.yubikit.android.transport.nfc.NfcConfiguration
import com.yubico.yubikit.android.transport.nfc.NfcNotAvailable
import com.yubico.yubikit.android.transport.usb.UsbConfiguration
import com.yubico.yubikit.core.YubiKeyDevice
import com.yubico.yubikit.core.smartcard.SmartCardConnection
import com.yubico.yubikit.yubiotp.Slot
import com.yubico.yubikit.yubiotp.YubiOtpSession

class HardwareKeyPlugin : FlutterPlugin, MethodChannel.MethodCallHandler, ActivityAware {

    private lateinit var channel: MethodChannel
    private lateinit var appContext: Context
    private var yubikit: YubiKitManager? = null

    // Holds the Flutter result for the in-flight challengeResponse call
    // so the NFC/USB callback (which fires on a separate thread) can
    // reply once a device responds. Guarded by `lock`.
    private val lock = Any()
    private var pendingResult: MethodChannel.Result? = null
    private var pendingChallenge: ByteArray? = null
    private var pendingSlot: Slot = Slot.TWO
    private val mainHandler = Handler(Looper.getMainLooper())

    // Tracks whether yubikit's NFC + USB discovery is currently engaged.
    // Set by startDiscoveryIfNeeded after a successful start; cleared by
    // stopDiscovery. Used to avoid double-start and to know whether
    // stopDiscovery needs to call into yubikit.
    private var isDiscoveryActive = false
    private var activityBinding: ActivityPluginBinding? = null

    override fun onAttachedToEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        appContext = binding.applicationContext
        channel = MethodChannel(binding.binaryMessenger, "flugo/hardware_key")
        channel.setMethodCallHandler(this)
        yubikit = YubiKitManager(appContext)
    }

    override fun onDetachedFromEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        channel.setMethodCallHandler(null)
        stopDiscovery()
        yubikit = null
    }

    override fun onAttachedToActivity(binding: ActivityPluginBinding) {
        activityBinding = binding
        startDiscoveryIfNeeded()
    }

    override fun onDetachedFromActivityForConfigChanges() {
        stopDiscovery()
        activityBinding = null
    }

    override fun onReattachedToActivityForConfigChanges(binding: ActivityPluginBinding) {
        activityBinding = binding
        startDiscoveryIfNeeded()
    }

    override fun onDetachedFromActivity() {
        stopDiscovery()
        activityBinding = null
    }

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "isNFCAvailable" -> result.success(NfcAdapter.getDefaultAdapter(appContext) != null)
            "isNFCEnabled" -> {
                val adapter = NfcAdapter.getDefaultAdapter(appContext)
                result.success(adapter != null && adapter.isEnabled)
            }
            "challengeResponse" -> handleChallengeResponse(call, result)
            "cancel" -> {
                // Don't stop discovery — keep it running for the next op.
                // Just clear pending state so any subsequent tag is ignored.
                fail("cancelled", "operation cancelled by user")
                result.success(null)
            }
            else -> result.notImplemented()
        }
    }

    private fun handleChallengeResponse(call: MethodCall, result: MethodChannel.Result) {
        val challenge = call.argument<ByteArray>("challenge")
        val slotInt = call.argument<Int>("slot") ?: 2
        if (challenge == null || challenge.isEmpty()) {
            result.error("bad_args", "challenge bytes required", null)
            return
        }
        synchronized(lock) {
            if (pendingResult != null) {
                result.error("busy", "another challengeResponse is in flight", null)
                return
            }
            pendingResult = result
            pendingChallenge = challenge
            pendingSlot = if (slotInt == 1) Slot.ONE else Slot.TWO
        }
        // Reader mode is already engaged (from onAttachedToActivity).
        // Nothing else to do — the next tag tap triggers onDevice, which
        // will see pendingChallenge != null and run the challenge.
        //
        // If the activity isn't attached (rare edge case), try to start
        // now so the call still works.
        startDiscoveryIfNeeded()
    }

    // startDiscoveryIfNeeded engages yubikit's NFC reader mode + USB
    // host listening on the current foreground activity. Idempotent —
    // safe to call when already active. Tied to activity lifecycle so
    // reader mode is always engaged when the user might tap.
    private fun startDiscoveryIfNeeded() {
        if (isDiscoveryActive) return
        val ykm = yubikit ?: return
        val activity = activityBinding?.activity ?: return

        // USB-OTG: handlePermissions(true) tells yubikit to display the
        // system USB permission prompt automatically on first connect.
        ykm.startUsbDiscovery(UsbConfiguration().handlePermissions(true)) { device ->
            onDevice(device, "usb")
        }

        // NFC: 30s reader-mode timeout. We keep the system's tap beep
        // ON (don't pass disableNfcDiscoverySound) so the user gets
        // audible confirmation that a tag was actually detected —
        // useful since the visual "tap your key" dialog dismisses as
        // soon as we get a response, which can be too fast to register
        // visually that the tap landed.
        try {
            ykm.startNfcDiscovery(
                NfcConfiguration().timeout(30000),
                activity
            ) { device ->
                onDevice(device, "nfc")
            }
        } catch (e: NfcNotAvailable) {
            // USB-OTG path remains; NFC isn't required for this device.
        }
        isDiscoveryActive = true
    }

    private fun stopDiscovery() {
        if (!isDiscoveryActive) return
        val ykm = yubikit ?: return
        val activity = activityBinding?.activity
        try { if (activity != null) ykm.stopNfcDiscovery(activity) } catch (_: Throwable) {}
        try { ykm.stopUsbDiscovery() } catch (_: Throwable) {}
        isDiscoveryActive = false
    }

    private fun onDevice(device: YubiKeyDevice, transport: String) {
        val challenge: ByteArray
        val slot: Slot
        synchronized(lock) {
            // No pending op — the user tapped while we weren't asking.
            // Silently drop the tag; discovery keeps running for future
            // ops.
            if (pendingChallenge == null) return
            challenge = pendingChallenge!!
            slot = pendingSlot
        }

        device.requestConnection(SmartCardConnection::class.java) { connResult ->
            try {
                val conn = connResult.value
                val session = YubiOtpSession(conn)
                // Signature: calculateHmacSha1(Slot, byte[], CommandState).
                // CommandState is for cancellation/touch-prompt callbacks
                // — null is fine for a simple synchronous call.
                val response = session.calculateHmacSha1(slot, challenge, null)
                val serial = try { session.serialNumber.toString() } catch (_: Throwable) { "" }
                succeed(response, serial, "yubikey")
            } catch (e: Throwable) {
                fail("device_error", e.message ?: e.javaClass.simpleName)
            }
        }
    }

    private fun succeed(response: ByteArray, serial: String, family: String) {
        val r: MethodChannel.Result?
        synchronized(lock) {
            r = pendingResult
            pendingResult = null
            pendingChallenge = null
        }
        if (r == null) return
        mainHandler.post {
            r.success(mapOf(
                "response" to response,
                "serial" to serial,
                "family" to family,
            ))
        }
    }

    private fun fail(code: String, message: String) {
        val r: MethodChannel.Result?
        synchronized(lock) {
            r = pendingResult
            pendingResult = null
            pendingChallenge = null
        }
        if (r == null) return
        mainHandler.post { r.error(code, message, null) }
    }
}
