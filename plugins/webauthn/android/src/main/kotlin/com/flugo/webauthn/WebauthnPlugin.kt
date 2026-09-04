// flugo webauthn — Android plugin (Play-Services-FREE, any-brand security keys).
//
// Talks CTAP2 DIRECTLY to the key over USB/NFC via yubikit-android's `fido`
// module — NO Google Play Services, so it works on GrapheneOS without GApps.
// yubikit provides the CTAP2 engine + clientPIN/UV crypto (Ctap2Session +
// Ctap2Client); we supply VID-agnostic discovery (mirrors hardware_key) and
// build clientDataJSON ourselves with the cloud origin (self-asserted, like the
// desktop libfido2 authenticator) — so no assetlinks / apk-key-hash needed.
//
// API reconciled against yubikit fido 3.1.0: the WebAuthn client is
// `Ctap2Client(Ctap2Session)` (the 2.x `BasicWebAuthnClient` was renamed), and
// getAssertion/makeCredential take a `ClientDataProvider` (built from our own
// clientDataJSON bytes via `ClientDataProvider.fromClientDataJson`), the typed
// options (`PublicKeyCredential{Request,Creation}Options.fromMap`), the RP id,
// the PIN, and a CommandState.

package com.flugo.webauthn

import android.content.Context
import android.content.pm.PackageManager
import android.os.Handler
import android.os.Looper
import android.util.Log
import com.yubico.yubikit.android.YubiKitManager
import com.yubico.yubikit.android.transport.nfc.NfcConfiguration
import com.yubico.yubikit.android.transport.nfc.NfcNotAvailable
import com.yubico.yubikit.android.transport.usb.UsbConfiguration
import com.yubico.yubikit.core.Transport
import com.yubico.yubikit.core.YubiKeyDevice
import com.yubico.yubikit.core.fido.FidoConnection
import com.yubico.yubikit.core.smartcard.SmartCardConnection
import com.yubico.yubikit.fido.client.Ctap2Client
import com.yubico.yubikit.fido.client.clientdata.ClientDataProvider
import com.yubico.yubikit.fido.ctap.Ctap2Session
import com.yubico.yubikit.fido.webauthn.PublicKeyCredential
import com.yubico.yubikit.fido.webauthn.PublicKeyCredentialCreationOptions
import com.yubico.yubikit.fido.webauthn.PublicKeyCredentialRequestOptions
import com.yubico.yubikit.fido.webauthn.SerializationType
import io.flutter.embedding.engine.plugins.FlutterPlugin
import io.flutter.embedding.engine.plugins.activity.ActivityAware
import io.flutter.embedding.engine.plugins.activity.ActivityPluginBinding
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import org.json.JSONArray
import org.json.JSONObject

class WebauthnPlugin : FlutterPlugin, MethodChannel.MethodCallHandler, ActivityAware {

    private lateinit var channel: MethodChannel
    private lateinit var appContext: Context
    private var yubikit: YubiKitManager? = null
    private var activityBinding: ActivityPluginBinding? = null

    // A CTAP2 ceremony (probe / getAssertion / makeCredential): the discovery
    // fires the pending action when a key arrives; results go back on the main
    // thread. One in flight at a time.
    private val lock = Any()
    private var pendingResult: MethodChannel.Result? = null
    private var pendingAction: ((Ctap2Session) -> String)? = null
    private var discoveryActive = false
    private var timeoutRunnable: Runnable? = null
    private val mainHandler = Handler(Looper.getMainLooper())

    // Overall ceremony watchdog: if no key is ever presented and the user never
    // taps Cancel, finish the run with a timeout so pendingResult can't wedge the
    // channel into a permanent "busy" state.
    private val ceremonyTimeoutMs = 120_000L

    override fun onAttachedToEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        appContext = binding.applicationContext
        channel = MethodChannel(binding.binaryMessenger, "flugo/webauthn")
        channel.setMethodCallHandler(this)
        yubikit = YubiKitManager(appContext)
    }

    override fun onDetachedFromEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        channel.setMethodCallHandler(null)
        yubikit = null
    }

    override fun onAttachedToActivity(binding: ActivityPluginBinding) { activityBinding = binding }
    override fun onDetachedFromActivityForConfigChanges() { activityBinding = null }
    override fun onReattachedToActivityForConfigChanges(binding: ActivityPluginBinding) { activityBinding = binding }
    override fun onDetachedFromActivity() { activityBinding = null }

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "isAvailable" -> {
                val pm = appContext.packageManager
                result.success(
                    pm.hasSystemFeature(PackageManager.FEATURE_NFC) ||
                        pm.hasSystemFeature(PackageManager.FEATURE_USB_HOST),
                )
            }
            "getAssertion" -> {
                val opts = call.argument<String>("options")
                val origin = call.argument<String>("origin")
                val pin = call.argument<String>("pin") ?: ""
                if (opts == null || origin == null) {
                    result.error("bad_args", "options + origin required", null); return
                }
                startCeremony(result) { session -> runAssertion(session, opts, origin, pin) }
            }
            "makeCredential" -> {
                val opts = call.argument<String>("options")
                val origin = call.argument<String>("origin")
                val pin = call.argument<String>("pin") ?: ""
                if (opts == null || origin == null) {
                    result.error("bad_args", "options + origin required", null); return
                }
                startCeremony(result) { session -> runRegistration(session, opts, origin, pin) }
            }
            "cancelAssertion" -> { finish(null, "cancelled"); result.success(null) }
            else -> result.notImplemented()
        }
    }

    // --- ceremony driver -------------------------------------------------------

    private fun startCeremony(result: MethodChannel.Result, action: (Ctap2Session) -> String) {
        val activity = activityBinding?.activity
        val ykm = yubikit
        if (activity == null || ykm == null) {
            result.error("no_activity", "no foreground activity / yubikit unavailable", null); return
        }
        synchronized(lock) {
            if (pendingResult != null) {
                result.error("busy", "a security-key operation is already in flight", null); return
            }
            pendingResult = result
            pendingAction = action
        }
        try {
            ykm.startUsbDiscovery(UsbConfiguration().handlePermissions(true)) { device -> onDevice(device) }
            try {
                ykm.startNfcDiscovery(NfcConfiguration().timeout(60000), activity) { device -> onDevice(device) }
            } catch (e: NfcNotAvailable) {
                // USB-only device; NFC not required.
            }
            discoveryActive = true
            val watchdog = Runnable { finish(null, "timeout") }
            synchronized(lock) { timeoutRunnable = watchdog }
            mainHandler.postDelayed(watchdog, ceremonyTimeoutMs)
        } catch (e: Throwable) {
            Log.e("flugo/webauthn", "discovery start failed", e)
            finish(null, "discovery_start: ${e.message ?: e.javaClass.simpleName}")
        }
    }

    private fun onDevice(device: YubiKeyDevice) {
        val action = synchronized(lock) { pendingAction } ?: return
        val usb = device.transport == Transport.USB
        // USB keys speak FIDO over HID (FidoConnection); NFC over ISO-DEP (SmartCardConnection).
        val connType = if (usb) FidoConnection::class.java else SmartCardConnection::class.java
        device.requestConnection(connType) { connResult ->
            // Cancelled (or timed out) between discovery and this callback — don't
            // run the ceremony or send the PIN to the key; the result is already
            // delivered.
            if (synchronized(lock) { pendingAction } == null) return@requestConnection
            try {
                val session = when (val conn = connResult.value) {
                    is FidoConnection -> Ctap2Session(conn)
                    is SmartCardConnection -> Ctap2Session(conn)
                    else -> throw IllegalStateException("unexpected connection type")
                }
                finish(action(session), null)
            } catch (e: Throwable) {
                Log.e("flugo/webauthn", "ceremony failed", e)
                finish(null, "${e.javaClass.simpleName}: ${e.message ?: "error"}")
            }
        }
    }

    private fun finish(ok: String?, err: String?) {
        val r: MethodChannel.Result?
        synchronized(lock) {
            r = pendingResult
            pendingResult = null
            pendingAction = null
            timeoutRunnable?.let { mainHandler.removeCallbacks(it) }
            timeoutRunnable = null
        }
        stopDiscovery()
        if (r == null) return
        mainHandler.post {
            if (err != null) r.error(if (err == "cancelled") "cancelled" else "ceremony_failed", err, null)
            else r.success(ok)
        }
    }

    private fun stopDiscovery() {
        if (!discoveryActive) return
        val ykm = yubikit ?: return
        val activity = activityBinding?.activity
        try { if (activity != null) ykm.stopNfcDiscovery(activity) } catch (_: Throwable) {}
        try { ykm.stopUsbDiscovery() } catch (_: Throwable) {}
        discoveryActive = false
    }

    // --- ceremonies (yubikit Ctap2Client) --------------------------------------
    //
    // Response-encoding contract (verify on any yubikit upgrade): the returned
    // JSON is produced by yubikit's `cred.toMap(SerializationType.JSON)`, NOT by
    // us, so its shape + encoding are a yubikit behavior we depend on. The server
    // (go-webauthn) requires:
    //   {id, rawId, type:"public-key",
    //    response:{clientDataJSON, authenticatorData, signature, userHandle?}}   (assertion)
    //   {id, rawId, type:"public-key", response:{clientDataJSON, attestationObject}} (registration)
    // with EVERY binary field base64url, NO padding (no '=', '+' or '/'), and
    // response.clientDataJSON equal to the bytes we passed via ClientDataProvider.
    // This can't be unit-tested from source (yubikit owns the encoding) and fails
    // CLOSED at the server if wrong (a bad response is rejected, never accepted).
    // It is validated end-to-end on-device; if you bump the yubikit version,
    // re-verify by decoding one real assertion response and checking the above.

    private fun runAssertion(session: Ctap2Session, optionsJson: String, origin: String, pin: String): String {
        val pub = publicKey(optionsJson)
        val rpId = pub.optString("rpId", pub.optJSONObject("rp")?.optString("id") ?: "")
        val clientData = ClientDataProvider.fromClientDataJson(
            clientDataJson("webauthn.get", pub.getString("challenge"), origin).toByteArray(Charsets.UTF_8),
        )
        val options = PublicKeyCredentialRequestOptions.fromMap(toMap(pub), SerializationType.JSON)
        val pc = pinChars(pin)
        try {
            val cred: PublicKeyCredential = Ctap2Client(session).getAssertion(
                clientData, options, rpId, pc, null,
            )
            return JSONObject(cred.toMap(SerializationType.JSON)).toString()
        } finally {
            pc?.fill(' ') // zero the PIN buffer promptly (don't wait for GC)
        }
    }

    private fun runRegistration(session: Ctap2Session, optionsJson: String, origin: String, pin: String): String {
        val pub = publicKey(optionsJson)
        val rpId = pub.optJSONObject("rp")?.optString("id") ?: pub.optString("rpId", "")
        val clientData = ClientDataProvider.fromClientDataJson(
            clientDataJson("webauthn.create", pub.getString("challenge"), origin).toByteArray(Charsets.UTF_8),
        )
        val options = PublicKeyCredentialCreationOptions.fromMap(toMap(pub), SerializationType.JSON)
        val pc = pinChars(pin)
        try {
            val cred: PublicKeyCredential = Ctap2Client(session).makeCredential(
                clientData, options, rpId, pc, null, null,
            )
            return JSONObject(cred.toMap(SerializationType.JSON)).toString()
        } finally {
            pc?.fill(' ') // zero the PIN buffer promptly (don't wait for GC)
        }
    }

    // pinChars → null for a key with no PIN configured (empty prompt), else the
    // PIN chars for CTAP2 clientPIN/UV.
    private fun pinChars(pin: String): CharArray? = if (pin.isEmpty()) null else pin.toCharArray()

    // clientDataJson — self-asserted, matching the desktop libfido2 authenticator
    // (icfx/hardware/fido2/fido2.go). challenge is passed through verbatim (already
    // base64url no-pad from the server).
    private fun clientDataJson(type: String, challenge: String, origin: String): String =
        JSONObject()
            .put("type", type)
            .put("challenge", challenge)
            .put("origin", origin)
            .put("crossOrigin", false)
            .toString()

    // publicKey returns the `publicKey` options object (server marshals its options
    // as {"publicKey":{...}}); falls back to the top-level object.
    private fun publicKey(optionsJson: String): JSONObject {
        val obj = JSONObject(optionsJson)
        return obj.optJSONObject("publicKey") ?: obj
    }

    // toMap converts the options JSON into the nested Kotlin Map/List/primitive
    // structure yubikit's fromMap(SerializationType.JSON) expects (binary fields
    // stay as their base64url strings; yubikit decodes them).
    private fun toMap(o: JSONObject): Map<String, Any?> {
        val m = HashMap<String, Any?>()
        for (k in o.keys()) m[k] = fromJson(o.get(k))
        return m
    }

    private fun fromJson(v: Any?): Any? = when (v) {
        is JSONObject -> {
            val m = HashMap<String, Any?>()
            for (k in v.keys()) m[k] = fromJson(v.get(k))
            m
        }
        is JSONArray -> (0 until v.length()).map { fromJson(v.get(it)) }
        JSONObject.NULL -> null
        else -> v
    }
}
