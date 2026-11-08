# Plugin manifest, version 1 (2026-11-08)

A proposal. Part of [plugin-system.md](plugin-system.md), which has the reasons.
Nothing here is built, so every name can still change.

A manifest is a JSON document a plugin serves at `GET <plugin url>/manifest`.
Kallisti fetches it through the server, checks it, and draws the panel from it.
It says what the panel looks like and what its buttons do. It holds no code.

## Rules

- Plain data. Text is text: no HTML, no markdown, no script.
- Unknown fields are ignored; unknown **component types** are drawn as
  "not supported" so one new type does not break the whole panel.
- `manifestVersion` is `1`. A higher number than the client knows: the panel
  says the client is too old.
- Limits (checked on fetch): 64 KB in total, 8 levels deep, 200 components.
  Over a limit: the panel says "not usable".
- Access (`members`, `staff`, channel list) is **not** in the manifest. The
  admin sets it in Kallisti's configuration, so a plugin cannot widen its own
  reach.
- The user's name is never in a request from the browser. The server adds it
  (`X-Kallisti-User`) from the session.

## Top level

| Field | Type | Meaning |
|---|---|---|
| `manifestVersion` | number | `1` |
| `name` | string | Shown in the header menu and the panel title |
| `version` | string | The plugin's own version, shown in the panel's info line |
| `description` | string, optional | One line, shown in the plugin list |
| `icon` | string | A name from Kallisti's icon set (`music`, `bot`, `radio`, ...). Unknown: a default icon |
| `api` | object | Where state and events come from (below) |
| `commands` | array, optional | Chat commands the plugin knows (below) |
| `panel` | component | The root of the panel |
| `i18n` | object, optional | Texts per language (below) |

### `api`

| Field | Meaning |
|---|---|
| `state` | Path that returns the current state as JSON, e.g. `/state` |
| `events` | Optional. Path of a Server-Sent Events stream; each `state` event carries the whole state (or `{"changed": true}`, then the client asks `state`) |
| `poll` | Optional. Seconds between asking `state` when there is no stream or it dropped. Default 5, at least 2 |

Paths are relative to the plugin's URL. The proxy adds `/api/plugins/<id>`.

### `commands`

Used for `!` completion in the composer and for the generic command form.

```json
{ "name": "volume", "aliases": ["v", "vol"], "help": "Get or set the volume",
  "admin": false,
  "args": [ { "name": "level", "kind": "number", "min": 0, "max": 100 } ] }
```

| Field | Meaning |
|---|---|
| `name`, `aliases` | What a user types after the prefix |
| `prefix` | Optional, default `!` |
| `help` | One line |
| `admin` | The panel hides the command from non-staff; the plugin enforces it anyway |
| `args` | `name`, `kind` (`text`, `number`, `choice`, `flag`), `required`, `choices`, `min`, `max`, `help`. Matches the argument schema planned in gotamusique (task 3-07) |

## State and bindings

The state is any JSON the plugin returns. The manifest reads it with **paths**:
dotted names with array indexes, `nowPlaying.title`, `queue.0.title`. A string
may mix text and paths with `{ }`: `"{nowPlaying.title} by {nowPlaying.artist}"`.
A missing path is an empty string; it never fails the panel.

Conditions use `"when"` on any component: `"when": "nowPlaying"` (the path
exists and is not empty, false, or 0), `"when": "!muted"` for the opposite. No
other expressions: no arithmetic, no comparison. If a plugin needs more, it
sends the finished value in the state.

## Actions

A component's `action` (or `onChange`, for inputs) says what happens.

```json
{ "http": { "method": "POST", "path": "/commands/skip" } }
{ "http": { "method": "POST", "path": "/commands/volume", "body": { "args": "{value}" } } }
{ "chat": "!volume {value}" }
```

- `http`: a call through the proxy. `method` is `GET` or `POST`. `path` and
  the string values in `body` take `{ }` from the component's value (`{value}`)
  or from the state. The answer may be `{ "ok": false, "error": "..." }`, shown
  as a toast; `output` text (what the bot would say in chat) is shown the same
  way. After an action the panel asks the state again, unless the event stream
  is open.
- `chat`: sends the text into the channel the user is looking at, or the
  plugin's `channel` if the configuration sets one. It goes through the normal
  send path (same rules as typing it: time-outs, slow mode, filters). For bots
  with no API.
- `confirm`: optional text. The panel asks first.
- A button is disabled while its action runs.

## Components

Every component has `type` and may have `when`, `label`, `hint` (a tooltip)
and `id`.

| `type` | Fields | Draws |
|---|---|---|
| `stack` | `direction` (`column` default, `row`), `gap`, `children` | A layout row or column |
| `section` | `title`, `collapsed` (start closed), `children` | A titled, collapsible group |
| `text` | `value`, `style` (`title`, `subtitle`, `muted`, `body`) | A line of text |
| `image` | `src` (a path on the plugin; the proxy serves it), `size` | A cover picture. Plugin paths only, never other hosts |
| `button` | `label`, `icon`, `action`, `confirm`, `style` (`normal`, `primary`, `danger`) | A button |
| `buttonGroup` | `children` (buttons) | Buttons side by side, e.g. transport |
| `slider` | `value` (path), `min`, `max`, `step`, `onChange` | A slider. Sends on release, not on every move |
| `select` | `value` (path), `options` (array or `{ "from": "path", "label": "title", "value": "id" }`), `onChange` | A drop-down |
| `toggle` | `value` (path), `onChange` | A switch |
| `list` | `items` (path to an array), `item` (a component, drawn per entry; `{ }` reads the entry), `empty` (text), `current` (path to an index to highlight) | A list, e.g. the queue |
| `search` | `placeholder`, `action` (gets `{query}`), `results` (path the answer is read from), `item` | A search box with results |
| `form` | `fields` (like command `args`), `submit` (a button), `action` | A generic form |
| `commandForm` | `commands` (names; default all non-admin) | A picker plus a form built from the `commands` list. Shows every command with no extra manifest work |
| `divider` | | A line |

Inside `list` and `search` items, `{title}` reads the entry, and `{$state.x}`
reads the panel's state, when both are needed.

A manifest with only `commandForm` is already a useful panel: every command
the bot knows, as a form. That is the cheapest plugin to write.

## i18n

```json
"i18n": { "de": { "Skip": "Weiter", "Volume": "Lautstärke" },
          "fr": { "Skip": "Suivant" } }
```

Every `label`, `hint`, `title`, `placeholder`, `empty` and `text` value is
looked up in the user's language by its plain text; no match: the text itself.
So a plugin can be written in English and translated by whoever adds a block.
Kallisti's own words in the panel (errors, "not reachable") use Kallisti's
dictionaries.

## Capabilities (only for the later iframe mode)

Not used by version 1 panels. A plugin that has to draw its own page would
list what it may ask the host:

```json
"capabilities": ["sendChat", "currentChannel", "currentUser"]
```

The bridge answers only those, with only that data (a channel's name and id,
the user's display name), and never the session, the token or other users.
Anything else is refused. See "A2" in [plugin-system.md](plugin-system.md).

## Example: gotamusique

Based on the API planned in `gotamusique/tasks/phase3/07-web-api.md`
(`/state`, `/commands`, `/commands/{name}`, `/events`, `/presets`,
`/library`). The state is assumed to look like:

```json
{ "nowPlaying": { "title": "Groove Salad", "artist": "SomaFM", "cover": "/cover" },
  "queue": [ { "id": "1", "title": "Groove Salad" } ], "current": 0,
  "volume": 40, "muted": false, "mode": "one-shot",
  "modes": ["one-shot", "repeat", "random", "autoplay"],
  "presets": [ { "name": "jazz", "title": "Jazz Yeah!" } ] }
```

```json
{
  "manifestVersion": 1,
  "name": "Music bot",
  "version": "0.4.0",
  "description": "Play radio and files in the music channel",
  "icon": "music",
  "api": { "state": "/state", "events": "/events", "poll": 5 },
  "commands": [
    { "name": "volume", "aliases": ["v"], "help": "Get or set the volume",
      "args": [ { "name": "level", "kind": "number", "min": 0, "max": 100 } ] },
    { "name": "skip", "help": "Skip to the next item" }
  ],
  "panel": {
    "type": "stack", "gap": 12, "children": [
      { "type": "section", "title": "Now playing", "children": [
        { "type": "image", "src": "{nowPlaying.cover}", "size": "medium", "when": "nowPlaying.cover" },
        { "type": "text", "value": "{nowPlaying.title}", "style": "title", "when": "nowPlaying" },
        { "type": "text", "value": "{nowPlaying.artist}", "style": "muted" },
        { "type": "text", "value": "Nothing is playing", "style": "muted", "when": "!nowPlaying" },
        { "type": "buttonGroup", "children": [
          { "type": "button", "icon": "stop", "label": "Stop",
            "action": { "http": { "method": "POST", "path": "/commands/stop" } } },
          { "type": "button", "icon": "skip", "label": "Skip",
            "action": { "http": { "method": "POST", "path": "/commands/skip" } } },
          { "type": "button", "icon": "mute", "label": "Mute", "when": "!muted",
            "action": { "http": { "method": "POST", "path": "/commands/mute" } } },
          { "type": "button", "icon": "unmute", "label": "Unmute", "when": "muted",
            "action": { "http": { "method": "POST", "path": "/commands/unmute" } } }
        ] },
        { "type": "slider", "label": "Volume", "value": "volume", "min": 0, "max": 100, "step": 5,
          "onChange": { "http": { "method": "POST", "path": "/commands/volume", "body": { "args": "{value}" } } } },
        { "type": "select", "label": "Mode", "value": "mode",
          "options": { "from": "modes" },
          "onChange": { "http": { "method": "POST", "path": "/commands/mode", "body": { "args": "{value}" } } } }
      ] },
      { "type": "section", "title": "Radio", "children": [
        { "type": "list", "items": "presets", "empty": "No stations set",
          "item": { "type": "button", "label": "{title}",
            "action": { "http": { "method": "POST", "path": "/commands/radio", "body": { "args": "{name}" } } } } }
      ] },
      { "type": "section", "title": "Queue", "children": [
        { "type": "list", "items": "queue", "current": "current", "empty": "The queue is empty",
          "item": { "type": "text", "value": "{title}" } },
        { "type": "button", "label": "Clear", "confirm": "Empty the queue?", "style": "danger",
          "action": { "http": { "method": "POST", "path": "/commands/clear" } } }
      ] },
      { "type": "section", "title": "Library", "collapsed": true, "children": [
        { "type": "search", "placeholder": "Search the library",
          "action": { "http": { "method": "GET", "path": "/library?q={query}" } },
          "results": "tracks",
          "item": { "type": "button", "label": "{title}",
            "action": { "http": { "method": "POST", "path": "/commands/file", "body": { "args": "{path}" } } } } }
      ] },
      { "type": "section", "title": "All commands", "collapsed": true,
        "children": [ { "type": "commandForm" } ] }
    ]
  },
  "i18n": { "de": { "Now playing": "Läuft gerade", "Stop": "Stopp", "Skip": "Weiter",
                    "Volume": "Lautstärke", "Queue": "Warteschlange", "Radio": "Radio" } }
}
```

Notes on the example:

- The state in the example does not match the first draft of gotamusique's
  `/state` field names; the two agree when task 3-07 is written. Where they
  differ, the manifest follows the bot, since the bot owns both.
- The `/library` answer needs a `tracks` array with `title` and `path`.
- A bot without an HTTP API can use `chat` actions and a manifest without
  `api`: the panel then has no state and no live part (just buttons and forms).

## Versioning

- Adding a component type, a field or an action kind: still `1`. Older
  clients draw unknown components as "not supported" and ignore unknown fields.
- Changing what an existing field means, or removing one: `2`. The plugin may
  serve both: `GET /manifest?v=2`, falling back to `/manifest`. A client asks
  for the highest version it knows.
- Kallisti never runs code from a manifest, so a manifest cannot break
  another feature; the worst it can do is show a useless panel.
