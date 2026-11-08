"""Static bundle checks only. Do not open camera or microphone devices."""
import json
from pathlib import Path
import plistlib
import unittest

BUNDLE = Path(__file__).resolve().parents[1]


class MacCameraPermissions(unittest.TestCase):
    def test_configured_info_plist_declares_camera_usage(self):
        config = json.loads((BUNDLE / "tauri.conf.json").read_text())
        path = config["bundle"]["macOS"]["infoPlist"]
        self.assertEqual(path, "Info.plist")
        with (BUNDLE / path).open("rb") as source:
            info = plistlib.load(source)
        self.assertTrue(info.get("NSCameraUsageDescription", "").strip())
        self.assertTrue(info["NSMicrophoneUsageDescription"].strip())

    def test_configured_entitlements_allow_camera(self):
        config = json.loads((BUNDLE / "tauri.conf.json").read_text())
        path = config["bundle"]["macOS"]["entitlements"]
        self.assertEqual(path, "Entitlements.plist")
        with (BUNDLE / path).open("rb") as source:
            entitlements = plistlib.load(source)
        self.assertIs(entitlements.get("com.apple.security.device.camera"), True)
        self.assertIs(entitlements["com.apple.security.device.audio-input"], True)


if __name__ == "__main__":
    unittest.main()
