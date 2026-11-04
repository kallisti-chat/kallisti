import type { Messages } from "../../lib/i18n";

// Words many places use. Area files keep their own texts (also the same
// word, where the context differs: translators see the key).
export default {
  "common.cancel": "Cancel",
  "common.save": "Save",
  "common.saving": "Saving…",
  "common.saved": "Saved",
  "common.close": "Close",
  "common.delete": "Delete",
  "common.remove": "Remove",
  "common.edit": "Edit",
  "common.back": "Back",
  "common.add": "Add",
  "common.create": "Create",
  "common.rename": "Rename",
  "common.copy": "Copy",
  "common.copied": "Copied",
  "common.send": "Send",
  "common.search": "Search",
  "common.loading": "Loading…",
  "common.retry": "Try again",
  "common.no": "No",
  "common.settings": "Settings",
  "common.on": "On",
  "common.off": "Off",
} as const satisfies Messages;
