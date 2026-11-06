<script lang="ts">
  const PATHS = {
    speaker: "M4 9h4l5-4v14l-5-4H4zM16 9a4 4 0 0 1 0 6M18.5 6.5a7.5 7.5 0 0 1 0 11",
    mic: "M12 3a3 3 0 0 1 3 3v6a3 3 0 0 1-6 0V6a3 3 0 0 1 3-3zM6 11a6 6 0 0 0 12 0M12 17v4",
    headphones: "M4 15v-3a8 8 0 0 1 16 0v3M4 15h3v5H4zM17 15h3v5h-3z",
    members:
      "M16 19v-1a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v1M9 11a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7zM22 19v-1a4 4 0 0 0-3-3.9M16 4.1a3.5 3.5 0 0 1 0 6.8",
    plus: "M12 7v10M7 12h10",
    // A phone handset, to leave voice.
    hangup: "M3 15c5-5 13-5 18 0l-2.5 2.5-3-1.5v-2.5a10 10 0 0 0-7 0V16l-3 1.5z",
    hash: "M9 4 7 20M17 4l-2 16M4 9h16M3 15h16",
    pin: "M12 17v5M9 3h6l-1 6 3 3v2H7v-2l3-3z",
    thread: "M4 5h16v11H9l-5 4zM8 9h8M8 12h5",
    lock: "M6 11h12v10H6zM8.5 11V7.5a3.5 3.5 0 0 1 7 0V11",
    screen: "M3 4h18v12H3zM8 20h8M12 16v4",
    camera: "M3 7h12v10H3zM15 10.5 21 7v10l-6-3.5",
    // Stream stage: all in a grid, one large over a strip, picture-in-picture.
    grid: "M4 4h7v7H4zM13 4h7v7h-7zM4 13h7v7H4zM13 13h7v7h-7z",
    focus: "M3 3h18v12H3zM3 19h5M10 19h4M16 19h5",
    pip: "M3 5h18v14H3zM12 12h6v4h-6z",
    fullscreen: "M4 9V4h5M15 4h5v5M20 15v5h-5M9 20H4v-5",
    reply: "M9 14 4 9l5-5M4 9h10a6 6 0 0 1 6 6v5",
    send: "M4 12 20 4l-6 16-3-7zM11 13l9-9",
    // A sticker: a sheet with a peeled corner.
    sticker: "M4 4h16v9l-7 7H4zM20 13h-5a2 2 0 0 0-2 2v5",
    copy: "M9 9h11v11H9zM5 15H4V4h11v1",
    info: "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM12 11v6M12 7.5v.5",
    // A character and an "A": translate.
    translate: "M3 5h9M7.5 3v2M5 5c1 4 3.5 6.5 7 8M10 5c-1 4-3.5 6.5-7 8M13 21l4-9 4 9M14.5 18h5",
  unread: "M4 6h16v12H4zM4 7l8 6 8-6",
    search: "M10.5 4a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13zM20 20l-4.9-4.9",
    back: "M20 12H4M10 6l-6 6 6 6",
    chevron: "M9 6l6 6-6 6",
    bell: "M6 16v-5a6 6 0 0 1 12 0v5l2 2H4zM10 21h4",
    edit: "M4 20h4L19 9l-4-4L4 16zM14 6l4 4",
    trash: "M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3",
    gear: "M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z",
  } as const;

  let {
    name,
    off = false,
    size = 18,
  }: { name: keyof typeof PATHS; off?: boolean; size?: number } = $props();
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="2"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
>
  <path d={PATHS[name]} />
  {#if off}<path d="M3 3l18 18" stroke="var(--danger)" />{/if}
</svg>
