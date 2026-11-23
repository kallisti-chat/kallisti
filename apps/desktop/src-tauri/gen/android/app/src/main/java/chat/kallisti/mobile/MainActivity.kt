package chat.kallisti.mobile

import android.os.Build
import android.os.Bundle
import android.view.View
import android.webkit.WebView
import androidx.activity.enableEdgeToEdge
import androidx.core.graphics.Insets
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat

class MainActivity : TauriActivity() {
  private var webView: WebView? = null
  private var inVoice = false

  override fun onWebViewCreate(webView: WebView) {
    this.webView = webView
    voicePriority(inVoice)
  }

  // While in voice the page's process keeps its priority when the app is
  // not on the screen: capture, noise suppression and the encoder run
  // there, and Android slows a hidden web view down. Else as usual (the
  // priority is given up when hidden). On the main thread.
  fun voicePriority(on: Boolean) {
    inVoice = on
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
      webView?.setRendererPriorityPolicy(WebView.RENDERER_PRIORITY_IMPORTANT, !on)
    }
  }

  override fun onCreate(savedInstanceState: Bundle?) {
    enableEdgeToEdge()
    super.onCreate(savedInstanceState)
    // Edge to edge (Android 15+) the keyboard no longer shrinks the window:
    // the web view would push the whole page up, the header out of sight.
    // The keyboard's height as padding below it makes the page shorter
    // instead; while it is open, no gesture bar space below the composer.
    ViewCompat.setOnApplyWindowInsetsListener(findViewById<View>(android.R.id.content)) { view, insets ->
      val keyboard = insets.getInsets(WindowInsetsCompat.Type.ime()).bottom
      view.setPadding(0, 0, 0, keyboard)
      if (keyboard == 0) {
        insets
      } else {
        val bars = insets.getInsets(WindowInsetsCompat.Type.navigationBars())
        WindowInsetsCompat.Builder(insets)
          .setInsets(WindowInsetsCompat.Type.navigationBars(), Insets.of(bars.left, bars.top, bars.right, 0))
          .build()
      }
    }
  }
}
