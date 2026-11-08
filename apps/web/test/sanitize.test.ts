// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { sanitize } from "../src/lib/sanitize";

const img = (alt: string, code: string) => `<img class="emoji" draggable="false" alt="${alt}" src="/emoji/${code}.png">`;

describe("sanitize with emoji", () => {
  it("makes emoji in text Twemoji images, after sanitizing", () => {
    expect(sanitize("hi 😀 <b>❤️</b>", true)).toBe(`hi ${img("😀", "1f600")} <b>${img("❤️", "2764")}</b>`);
  });
  it("never touches attributes, and attribute text cannot become markup", () => {
    const div = document.createElement("div");
    div.innerHTML = sanitize('<a title="😀 <img src=x onerror=alert(1)>" href="https://x.org/">hi</a>', true);
    expect(div.querySelectorAll("img").length).toBe(0); // the title stays text
    expect(div.querySelector("a")?.title).toBe("😀 <img src=x onerror=alert(1)>");
  });
  it("makes images in code too (blank there without an emoji font)", () => {
    expect(sanitize("<code>😀</code>", true)).toBe(`<code>${img("😀", "1f600")}</code>`);
  });
  it("does not let foreign images in, also with the emoji class", () => {
    expect(sanitize('<img class="emoji" src="/emoji/../api/x.png">', true)).not.toContain("src=");
  });
  it("leaves emoji alone without the option", () => {
    expect(sanitize("hi 😀")).toBe("hi 😀");
  });
});

describe("sanitize message layout", () => {
  it.each([
    "position:fixed;inset:0;z-index:2147483647;background:white",
    "position:absolute;top:0;left:0;width:100vw;height:100vh",
    "transform:scale(100);transform-origin:top left",
    "margin-top:-100vh;padding:100vh",
    "display:block;overflow:visible;contain:none",
    "font-size:100vh;line-height:0",
    "--cover:fixed;position:var(--cover)",
    "POSITION:fixed!important",
    "color:red;animation:cover 1s infinite",
    "text-decoration-thickness:100vh",
    "text-decoration:var(--missing, underline solid red 100vh)",
    "text-decoration:env(missing, underline solid red 100vh)",
    "text-decoration:attr(data-size type(*), underline solid red 100vh)",
    "text-decoration:if(style(--review: yes): underline solid red 100vh; else: underline solid red 100vh)",
  ])("removes layout styles: %s", (style) => {
    for (const emoji of [false, true]) {
      const div = document.createElement("div");
      div.innerHTML = sanitize(`<a href="https://example.org/" style="${style}">message</a>`, emoji);
      const link = div.querySelector("a")!;
      expect(link.getAttribute("style")).toBeNull();
      expect(link.href).toBe("https://example.org/");
      expect(link.textContent).toBe("message");
    }
  });

  it("does not retain raw declarations ignored by the CSS parser", () => {
    expect(sanitize('<span style="color:red;POSITION:fixed!important">x</span>')).not.toMatch(/position/i);
  });

  it.each(["auto", "from-font", "initial"])("accepts browser text-decoration longhands with thickness %s", (thickness) => {
    const div = document.createElement("div");
    div.innerHTML = sanitize(`<span style="color:red;text-decoration-line:underline;text-decoration-style:solid;text-decoration-color:blue;text-decoration-thickness:${thickness}">text</span>`);
    const kept = div.querySelector("span")!.style;
    expect(kept.color).toBe("red");
    expect(kept.getPropertyValue("text-decoration-line")).toBe("underline");
    expect(kept.getPropertyValue("text-decoration-style")).toBe("solid");
    expect(kept.getPropertyValue("text-decoration-color")).toBe("blue");
    expect(kept.getPropertyValue("text-decoration-thickness")).toBe(thickness);
  });

  it("keeps text formatting that does not control layout", () => {
    const style = "color:red;background-color:yellow;font-family:serif;font-weight:bold;font-style:italic;text-decoration:underline";
    const div = document.createElement("div");
    div.innerHTML = sanitize(`<span style="${style}">text</span>`);
    const kept = div.querySelector("span")!.style;
    expect([kept.color, kept.backgroundColor, kept.fontFamily, kept.fontWeight, kept.fontStyle, kept.textDecoration]).toEqual([
      "red", "yellow", "serif", "bold", "italic", "underline",
    ]);
  });
});

// The page may load pictures and media from other servers (several servers
// in one client): message HTML must load nothing remote (readers' IPs).
describe("sanitize loads nothing remote", () => {
  const remote = (html: string) => {
    const out = sanitize(html);
    return /tracker\.example/.test(out) ? out : null;
  };
  it("drops remote addresses from styles, srcset, posters, SVG and media", () => {
    for (const html of [
      '<span style="background-image:url(https://tracker.example/a)">x</span>',
      '<span style="background:\\75 rl(https://tracker.example/a)">x</span>',
      '<span style="background-image:image-set(\'https://tracker.example/a\' 1x)">x</span>',
      '<img srcset="https://tracker.example/a 1x" src="data:image/png;base64,AA==">',
      '<video poster="https://tracker.example/a"></video>',
      '<audio src="https://tracker.example/a"></audio>',
      '<video><source src="https://tracker.example/a"></video>',
      '<svg><image href="https://tracker.example/a"></image></svg>',
      '<svg><image xlink:href="https://tracker.example/a"></image></svg>',
      '<table background="https://tracker.example/a"><tr><td>x</td></tr></table>',
      '<p>a</p><style>p{background:url(https://tracker.example/a)}</style>',
      '<svg><style>circle{fill:url(https://tracker.example/a)}</style><circle r="5"/></svg>',
      '<div><style>body{background-image:url(https://tracker.example/a)}</style></div>',
      '<svg width="20" height="20"><rect width="20" height="20" mask="url(https://tracker.example/a)"/></svg>',
      '<svg><circle r="5" fill="url(https://tracker.example/a#g)"/></svg>',
      '<math><mi href="https://tracker.example/a">x</mi></math>',
    ]) {
      expect(remote(html), html).toBeNull();
    }
  });
  it("keeps colours, links and inline pictures", () => {
    expect(sanitize('<span style="color: red">x</span>')).toBe('<span style="color: red;">x</span>');
    expect(sanitize("<span style=\"font-family: 'Curlz MT'; color: red\">x</span>")).toContain("color: red");
    expect(sanitize('<a href="https://x.org/">x</a>')).toContain('href="https://x.org/"');
    expect(sanitize('<img src="data:image/png;base64,AA==">')).toContain("data:image/png");
    // Text and link addresses may say url( (they load nothing by it).
    const link = sanitize('<a href="https://example.org/url(example)/details" title="CSS url(...) syntax">x</a>');
    expect(link).toContain('href="https://example.org/url(example)/details"');
    expect(link).toContain('title="CSS url(...) syntax"');
  });
});
