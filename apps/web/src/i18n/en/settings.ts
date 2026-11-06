import type { Messages } from "../../lib/i18n";

export default {
  "settings.language": "Language",
  "settings.languageAuto": "As the browser",
  "settings.betterTranslation": "Better translation",
  "settings.betterTranslationHint": "Translates messages with a larger model on this device (Hy-MT2). It downloads about 1.4 GB once and needs a graphics card (WebGPU).",
  "settings.betterTranslationNoGpu": "Not available here: this browser or graphics card has no WebGPU with 16-bit numbers.",

  // Categories (lib/settingsSections) and the dialog around them.
  "settings.groupUser": "User settings",
  "settings.groupApp": "App settings",
  "settings.sectionProfile": "Profile",
  "settings.sectionAccount": "Account",
  "settings.sectionVoice": "Voice & video",
  "settings.sectionNotifications": "Notifications",
  "settings.sectionAppearance": "Appearance",
  "settings.categoriesLabel": "{title} categories",
  "settings.categoryHeading": "{title}: {category}",
  "settings.editProfile": "Edit profile ›",
  "settings.logOut": "Log out",
  "settings.footer": "Kallisti: a Mumble server and client. {link}",

  // Profile.
  "settings.aboutMe": "About me",
  "settings.bioHint": "Everyone on the server sees it, in Mumble apps too.",
  "settings.myEmoji": "My emoji",
  "settings.myEmojiHint":
    "Type :name: to use one. Everyone sees them in your messages and reactions; Mumble apps show :name:. To add one from a message, click it.",
  "settings.profilePicture": "Profile picture",
  "settings.changePicture": "Change picture",
  "settings.choosePicture": "Choose picture",
  "settings.pictureError": "Could not use this picture. Try a PNG, JPEG, GIF or WebP file.",
  "settings.pictureRegisterHint": "Register an account to set a profile picture.",

  // Account.
  "settings.keepAccount": "Keep this account",
  "settings.guestAccountHint":
    "This is a guest account: it is deleted a day after your last visit. With a password it stays, and you can log in on other devices.",
  "settings.setPasswordTitle": "Set a password",
  "settings.fediOnlyHint":
    "You log in with your fediverse account. A password also works in Mumble apps, with your name. First you confirm with your fediverse server.",
  "settings.changePassword": "Change password",
  "settings.setPassword": "Set password",
  "settings.currentPassword": "Current password",
  "settings.newPassword": "New password (8 or more characters)",
  "settings.passwordSetKept": "Password set: your account stays.",
  "settings.passwordSetFirst": "Password set: you can also log in with your name. Other devices are logged out.",
  "settings.passwordChanged": "Password changed. Other devices are logged out.",
  "settings.confirmWith": "Confirm with {host}",
  "settings.otherDevice": "Log in on another device",
  "settings.otherDeviceHint": "Show a code here and scan it with your phone: no password to type there.",
  "settings.otherDeviceScanHint": "Or scan the code another device shows on its login screen.",
  "settings.showLoginCode": "Show a login code",
  "settings.scanCode": "Scan a code",
  "settings.logOutGuestHint": "Logging out ends this guest account (set a password above to keep it).",
  "settings.logOutEverywhereHint":
    "\"Everywhere\" also logs out your other browsers and devices, for example after you used a shared computer.",
  "settings.logOutEverywhere": "Log out everywhere",

  // Fediverse account.
  "settings.fediAccount": "Fediverse account",
  "settings.fediPublic": "Show it on my profile, as verified",
  "settings.fediUnlink": "Unlink",
  "settings.fediUnlinking": "Unlinking…",
  "settings.fediOnlyLogin": "It is your only way to log in: set a password above to unlink it.",
  "settings.fediLinkHint": "Log in with your Mastodon, Pleroma, GoToSocial or other fediverse account too.",
  "settings.fediPlaceholder": "mastodon.social or @you@mastodon.social",
  "settings.fediAddressLabel": "Your fediverse server or address",
  "settings.fediLink": "Link account",
  "settings.fediOpening": "Opening…",

  // Servers this account logs in to.
  "settings.allowedSites": "Servers you log in to with this account",
  "settings.allowedSitesNotLoaded": "The list did not load.",
  "settings.removing": "Removing…",
  "settings.allowedSitesHint": "A removed server asks for your OK again at your next login there. Your account there stays.",

  // Login code (QR).
  "settings.qrLoginTitle": "Log in with a QR code",
  "settings.qrMaking": "Making a code…",
  "settings.qrCodeLabel": "Login code",
  "settings.qrOfferHint": "Scan this code with the Kallisti app (or the camera) on the device you want to log in. You confirm here.",
  "settings.qrRequestHint":
    "Scan this code with the Kallisti app (or the camera) on a phone where you are logged in, and confirm there.",
  "settings.qrValid": "The code is valid for 2 minutes.",
  "settings.qrAsk": "Log in on {device}?",
  "settings.qrAskHint": "Only if you scanned the code yourself, just now.",
  "settings.qrLogIn": "Log in",
  "settings.qrConfirmOn": "Confirm on {device}…",
  "settings.qrLoggingIn": "Logging in on {device}…",
  "settings.qrLoggedInOn": "Logged in on {device}.",
  "settings.qrLoggedIn": "Logged in.",
  "settings.qrRefused": "Not logged in: the login was refused.",
  "settings.qrExpired": "The code expired.",
  "settings.qrNewCode": "New code",

  // Voice & video.
  "settings.inputDevice": "Input device",
  "settings.deviceDefault": "Default",
  "settings.microphone": "Microphone",
  "settings.micFellBack": "The chosen microphone is not available. Using {device}.",
  "settings.micFellBackDefault": "The chosen microphone is not available. Using the default microphone.",
  "settings.micInUse": "In use: {device}",
  "settings.camera": "Camera",
  "settings.cameraNumbered": "Camera {number}",
  "settings.cameraListHint": "The list fills once you have turned the camera on in voice.",
  "settings.noiseSuppression": "Noise suppression",
  "settings.noiseStandard": "Standard (the browser's)",
  "settings.noiseStrong": "Strong (RNNoise)",
  "settings.noiseStrongest": "Strongest (DeepFilterNet3)",
  "settings.noiseOffMusic": "Off in music mode.",
  "settings.noiseRunning": "{filter} is running.",
  "settings.noiseLoading": "Loading DeepFilterNet3 (about 12 MB, once)…",
  "settings.noiseHint":
    "Strong removes fan and other steady noise; typing alone does not start sending. Strongest also removes most typing, but loads about 12 MB, uses more CPU and adds a little delay.",
  "settings.hearYourself": "Hear yourself",
  "settings.echoTestStart": "Start echo test",
  "settings.echoTestStop": "Stop",
  "settings.echoTestHint": "Plays your microphone as the others hear it, after noise suppression. Use headphones.",
  "settings.outputDevice": "Output device",
  "settings.speakers": "Speakers",
  "settings.playbackBuffer": "Playback buffer",
  "settings.playbackBufferHint":
    "How long others' voices wait before they play. More fills gaps on a bad connection, but adds delay. Music always gets at least 500 ms.",
  "settings.inputMode": "Input mode",
  "settings.voiceActivity": "Voice activity",
  "settings.pushToTalk": "Push to talk",
  "settings.pttKey": "Push-to-talk key",
  "settings.pttRecording": "Press a key… (Esc to cancel)",
  "settings.pttHintDesktop": "Works in the background too. The key is reserved for Kallisti while the app runs.",
  "settings.pttHintBrowser": "Works only while this tab has focus.",
  "settings.keySpace": "Space",
  "settings.keyCapsLock": "Caps Lock",
  "settings.keyLeft": "Left {key}",
  "settings.keyRight": "Right {key}",
  "settings.sensitivity": "Sensitivity",
  "settings.vadThreshold": "Voice activity threshold",
  "settings.decibels": "{value} dB",
  "settings.sensitivityHint": "Speak normally: the bar should turn green when you talk, and stay grey when you are quiet.",
  "settings.echoCancellation": "Echo cancellation",
  "settings.echoCancellationHint":
    "Needed with speakers. With a headset you can turn it off: on a Mac, the desktop app and Safari otherwise switch Bluetooth headsets to their call mode, with worse sound.",
  "settings.musicMode": "Music mode",
  "settings.musicModeHint":
    "For playing music: no noise suppression, echo cancellation or automatic volume, and better sound (stereo if the microphone has two channels). Use a headset.",
  "settings.closeMic": "Close the microphone when muted",
  "settings.closeMicHint": "The browser then stops showing it as recording, but the sound stops for a moment when you mute or unmute.",
  "settings.streamPreviews": "Show a preview of my screen share and camera",
  "settings.streamPreviewsHint": "People who are not watching see a small picture, updated every few seconds.",

  // Streams (lib/stream/quality) and OBS.
  "settings.qualityLabel": "{height}p {fps} fps",
  "settings.streamScreen": "{name}'s screen",
  "settings.streamCamera": "{name}'s camera",
  "settings.obsTitle": "Stream from OBS",
  "settings.obsLive": "Your stream from OBS is live.",
  "settings.obsSteps":
    "OBS 30 or newer can stream into your voice channel: it shows like a screen share. Be connected here and in a voice channel where you may speak. In OBS, Settings → Stream: Service \"WHIP\", Server {url}, Bearer Token: your stream key.",
  "settings.obsOutput":
    "Settings → Output (Advanced): H.264 video, keyframe interval 1 or 2 s, profile baseline or main, 1080p and 8 Mbit/s at most.",
  "settings.obsBFrames":
    "No B-frames (we forward the video as it is; with them the picture breaks up or turns pink): with x264, type {option} in \"x264 Options\"; with NVENC, QuickSync or AMF, set B-Frames to 0.",
  "settings.obsNewKey": "Your stream key (shown only now): {key}",
  "settings.obsMakeNewKey": "Make a new key",
  "settings.obsMakeKey": "Make a stream key",
  "settings.obsMaking": "Making…",
  "settings.obsRemoveKey": "Remove the key",
  "settings.obsRemoved": "Removed",
  "settings.obsKeyHint":
    "A new key, removing it, a new password or \"log out everywhere\" stop the old key (and a stream that uses it).",

  // Notifications.
  "settings.desktopNotifications": "Desktop notifications (mentions and direct messages; per channel in its menu)",
  "settings.pushNotifications": "Push notifications on this device",
  "settings.pushHint": "Mentions and direct messages also while Kallisti is closed. They say who and where, not the message.",
  "settings.pushInstallHint":
    "For push notifications on this iPhone or iPad: tap Share, then \"Add to Home Screen\", and open Kallisti from there (iOS 16.4 or later).",
  "settings.pushNotChanged": "The browser did not change it.",
  "settings.sounds": "Sounds",
  "settings.soundsHint": "When you mute or deafen yourself, and undeafen.",
  "settings.soundVolume": "Sound volume",
  "settings.percent": "{value}%",
  // Per channel (lib/channelPrefs).
  "settings.notifyAll": "All messages",
  "settings.notifyMentions": "Only @mentions",
  "settings.notifyNothing": "Nothing",
  "settings.muteFor15Minutes": "For 15 minutes",
  "settings.muteFor1Hour": "For 1 hour",
  "settings.muteFor8Hours": "For 8 hours",
  "settings.muteFor24Hours": "For 24 hours",
  "settings.muteUntilOn": "Until I turn it back on",

  // Members of a private channel.
  "settings.accessTitle": "Members of {name}",
  "settings.accessNote": "Only members, admins and moderators see this channel.",
  "settings.accessGroupHeading": "{group} — {count}",

  // Appearance.
  "settings.theme": "Theme",
  "settings.themeDark": "Dark",
  "settings.themeLight": "Light",
  "settings.themeSystem": "As the system",
  "settings.timeFormat": "Time format",
  "settings.timeAuto": "As the language",
  "settings.time24": "24-hour (19:05)",
  "settings.time12": "12-hour (7:05 PM)",
} as const satisfies Messages;
