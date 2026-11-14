package chat.kallisti.mobile

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder

// While in voice: a foreground service, so Android lets the app play on (and
// record) in the background and with the screen off. Of type microphone when
// the microphone is allowed, else mediaPlayback: a listener without it would
// get no service, and Android freezes the app a few seconds after it leaves
// the screen. Its notification ("In voice: #channel") opens the app.
class VoiceService : Service() {
  override fun onBind(intent: Intent?): IBinder? = null

  override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    // Stopped meanwhile (a start overtaken by a stop): no service.
    val channel = wanted
    if (channel == null) {
      stopSelf()
      return START_NOT_STICKY
    }
    val notification = notification(this, channel)
    val mic = micAllowed(this)
    try {
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
        val type = if (mic) ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE else ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK
        startForeground(NOTIFICATION_ID, notification, type)
      } else {
        startForeground(NOTIFICATION_ID, notification)
      }
      running = true
      recording = mic
    } catch (e: Exception) {
      // E.g. the microphone not allowed (14+): no service, no crash.
      android.util.Log.w("Kallisti", "voice service not in the foreground", e)
      stopSelf()
    }
    return START_NOT_STICKY
  }

  override fun onDestroy() {
    running = false
    super.onDestroy()
  }

  companion object {
    private const val CHANNEL_ID = "voice"
    private const val NOTIFICATION_ID = 1

    // The channel while in voice (null: off), set by VoicePlugin.
    @Volatile var wanted: String? = null

    // In the foreground now: a new channel only updates the notification
    // (no new start, which Android allows only in front).
    @Volatile var running = false

    // Running as type microphone (else mediaPlayback: see VoicePlugin).
    @Volatile var recording = false

    fun micAllowed(context: Context) =
      context.checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED

    fun update(context: Context, channel: String) {
      context.getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, notification(context, channel))
    }

    private fun notification(context: Context, channel: String): Notification {
      val manager = context.getSystemService(NotificationManager::class.java)
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
        manager.createNotificationChannel(NotificationChannel(CHANNEL_ID, "Voice", NotificationManager.IMPORTANCE_LOW))
      }
      val open = PendingIntent.getActivity(
        context, 0,
        Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
      )
      val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) Notification.Builder(context, CHANNEL_ID) else Notification.Builder(context)
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) builder.setForegroundServiceBehavior(Notification.FOREGROUND_SERVICE_IMMEDIATE)
      return builder
        .setSmallIcon(android.R.drawable.ic_btn_speak_now)
        .setContentTitle(if (channel.isEmpty()) "In voice" else "In voice: $channel")
        .setContentIntent(open)
        .setOngoing(true)
        .build()
    }
  }
}
