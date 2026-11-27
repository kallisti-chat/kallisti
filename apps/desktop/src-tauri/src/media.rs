//! Microphone, camera and screen for the page. WebKitGTK (Linux) denies
//! getUserMedia unless the app answers its permission request: our own page
//! gets them, any other page not. macOS and Windows ask the user themselves.
//! The same for web notifications.

/// Whether a page address is on one of the app's origins.
pub fn own_page(uri: &str, origins: &[String]) -> bool {
    origins
        .iter()
        .any(|o| uri.strip_prefix(o.as_str()).is_some_and(|rest| rest.is_empty() || rest.starts_with(['/', '?', '#'])))
}

/// Also turns off HTML full screen (see below), and turns on the web
/// inspector (right click, Inspect Element) when KALLISTI_DEVTOOLS is set,
/// for finding faults on Linux.
#[cfg(target_os = "linux")]
pub fn allow_own_media(window: &tauri::WebviewWindow, origins: Vec<String>) -> tauri::Result<()> {
    use webkit2gtk::glib::prelude::*;
    use webkit2gtk::{NotificationPermissionRequest, PermissionRequestExt, SettingsExt, UserMediaPermissionRequest, WebViewExt};

    window.with_webview(move |w| {
        let view = w.inner();
        if let Some(settings) = view.settings() {
            settings.set_enable_media_stream(true);
            // No HTML full screen: WebKitGTK 2.50 aborts the whole app on it
            // (a release assert in WebFullScreenManagerProxy). The page uses
            // the window's full screen instead (lib/windowFullScreen.ts).
            settings.set_enable_fullscreen(false);
            if std::env::var_os("KALLISTI_DEVTOOLS").is_some() {
                settings.set_enable_developer_extras(true);
            }
        }
        view.connect_permission_request(move |view, request| {
            // Notifications too (the page uses the system's, see lib.rs).
            if !request.is::<UserMediaPermissionRequest>() && !request.is::<NotificationPermissionRequest>() {
                return false; // WebKitGTK's default
            }
            if view.uri().is_some_and(|u| own_page(u.as_str(), &origins)) {
                request.allow();
            } else {
                request.deny();
            }
            true
        });
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn own_pages_only() {
        let origins = vec!["tauri://localhost".to_string(), "http://localhost:5173".to_string()];
        assert!(own_page("tauri://localhost", &origins));
        assert!(own_page("tauri://localhost/", &origins));
        assert!(own_page("http://localhost:5173/index.html", &origins));
        assert!(own_page("tauri://localhost?a#b", &origins));
        assert!(own_page("tauri://localhost#b", &origins));
        assert!(!own_page("tauri://localhost.evil.example/", &origins));
        assert!(!own_page("http://localhost:51730/", &origins));
        assert!(!own_page("https://example.com/", &origins));
    }
}
