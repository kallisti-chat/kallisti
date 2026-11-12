package chat.kallisti.mobile

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Handler
import android.os.Looper
import app.tauri.annotation.Command
import app.tauri.annotation.InvokeArg
import app.tauri.annotation.TauriPlugin
import app.tauri.plugin.Invoke
import app.tauri.plugin.Plugin

@InvokeArg
class StartArgs {
  var channel: String? = null
}

// Starts and stops VoiceService (see android.rs: voice_service). The
// commands only set what is wanted (VoiceService.wanted); reconcile brings
// the service there, again when the app comes to the front.
@TauriPlugin
class VoicePlugin(private val activity: Activity) : Plugin(activity) {
  private val main = Handler(Looper.getMainLooper())
  private var askedNotifications = false

  @Command
  fun start(invoke: Invoke) {
    VoiceService.wanted = invoke.parseArgs(StartArgs::class.java).channel ?: ""
    main.post { reconcile() }
    invoke.resolve()
  }

  @Command
  fun stop(invoke: Invoke) {
    VoiceService.wanted = null
    main.post { reconcile() }
    invoke.resolve()
  }

  override fun onResume() {
    reconcile()
  }

  // On the main thread.
  private fun reconcile() {
    val channel = VoiceService.wanted
    if (channel == null) {
      // A start still on its way stops itself (VoiceService.onStartCommand).
      if (VoiceService.running) {
        VoiceService.running = false // a start right after starts it again
        activity.stopService(Intent(activity, VoiceService::class.java))
      }
      return
    }
    // At once, without the microphone too (as mediaPlayback). Allowed
    // later (its dialog closed: onResume), it starts again as microphone,
    // which Android allows only in front, as here.
    val mic = VoiceService.micAllowed(activity)
    if (VoiceService.running && (VoiceService.recording || !mic)) {
      VoiceService.update(activity, channel)
      return
    }
    // After the microphone (one permission request at a time), once:
    // without it the service runs, but its notification hides (13+).
    if (mic && Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU && !askedNotifications &&
      activity.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
    ) {
      askedNotifications = true
      activity.requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 0)
    }
    try {
      val intent = Intent(activity, VoiceService::class.java)
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) activity.startForegroundService(intent) else activity.startService(intent)
    } catch (e: Exception) {
      // Not allowed from the background (12+): again when back in front.
      android.util.Log.w("Kallisti", "voice service not started", e)
    }
  }
}
