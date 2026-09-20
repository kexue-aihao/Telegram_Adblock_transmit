# WebUI visual and motion system

The moderation console uses graphite surfaces, a teal accent and translucent
navigation. Content sections stay unframed; tables, filters and repeated metrics
have defined boundaries. The interface uses the existing system-font stack and
local Lucide sprite. There are no remote fonts and no CDN: the panel is built
with Vite from `web/` into committed assets under `internal/webui/assets`, and it
runs under a Content-Security-Policy that forbids inline scripts and `eval`.

## Architecture

The panel is a Vue 3 single-page app (hash routing) whose runtime primitives —
request ownership, motion, dialogs, toasts, formatting — live in `web/src/core`
and have exactly one implementation each. Pages migrate from the imperative
renderers in `web/src/legacy` to Vue components one at a time behind the same
router table, so both kinds of page share one shell and one set of rules.

Invariants a page implementation must keep, whichever kind it is:

- The routed page is a `<section class="page" tabindex="-1">` inside `#view`; a
  route change cancels the previous page's GET requests and cross-fades a
  snapshot of the outgoing page that carries no ids and is inert.
- Requests: GETs inherit the page signal and die with their page; mutations never
  do. Page state lives in the hash query (`router.replace`), never in history.
- Motion goes through the shared owner, which clears every `will-change` it set;
  no animation may outlive its element.
- Accessibility and text are part of the contract: `role="switch"` with the
  existing Chinese accessible names, real `<dialog>`, `<details>` and `<a>`
  elements, and UTC timestamps formatted as before.
- Values from the API are text, never markup.

## Tokens and layout

Tokens live in `web/src/styles/tokens.css`; the structural rules that the page
renderers depend on stay in `components.css`, and the visual treatment is layered
on top of them in the same file.

| Role | Light | Dark |
| --- | --- | --- |
| Canvas | `#eceff5` | `#07080b` |
| Surface / raised | `#ffffff` / `#f2f4f8` | `#0c0e13` / `#11141b` |
| Ink / secondary | `#12151c` / `#596172` | `#e9ebf2` / `#9aa1b2` |
| Lines (three weights) | ink 8% / 13% / 18% | white 5% / 9% / 16% |
| Hover / active | ink 3.5% / 6% | white 4% / 7% |
| Brand | `oklch(.51 .18 262)`, hover darker | `oklch(.78 .16 250)`, hover brighter |
| Glass | white 68% / 88% | surface 52% / raised 66% |

Interaction states are neutral alpha washes rather than brand tints, and the
canvas sits well below white so cards and glass have somewhere to stand. The
palette and its reasoning follow the sibling panel in `Telegram_session_Adblock`
(`packages/web-vue/src/styles/theme.css`); keep the two in step.

Semantic success, warning and error colors remain independent of the brand
accent. Headings use 28/18px, body text 14px, secondary information at least 12px,
and primary metrics 30px. Mobile fields use 16px to avoid automatic input zoom.
Headings carry `-0.015em` tracking, everywhere else tracking is zero. Spacing
follows 4/8/12/16/20/24/32/40px increments (`--space-*`), and numerals in
metrics, tables and chart axes are tabular.

Controls use 8px radii, panels 12px, navigation chrome 16px and dialogs 24px. The 216px desktop sidebar and 64px topbar have 12px outer
insets. Content is capped at 1600px. Below 900px the sidebar becomes a horizontal
navigation bar; below 640px labels stack beneath icons and tables become labeled
records. Dashboard trend/failure columns collapse below 1200px.

The shell is a fixed-viewport column: the document never scrolls and `#view` is
the only scroll container, so long pages are reached with the wheel inside the
content column. `.shell` must therefore be a full-height flex column
(`height: 100dvh; display: flex; flex-direction: column`) — that is what lets
`.layout`'s `flex: 1` bind and gives `#view` a bounded height. If any wrapper in
that chain is a plain block it grows to its content, `#view`'s `overflow-y: auto`
never engages and everything below the fold is clipped by `body { overflow:
hidden }`. The topbar and the rail stay put because they are siblings of `#view`,
not sticky elements inside it. The motion suite asserts the shell fits the
viewport and that a page taller than it is reachable.

`backdrop-filter` is used only where content actually scrolls behind a surface:
the topbar, the sidebar, dialogs and toasts. Content cards are opaque with a
hairline and a one-pixel inner highlight and carry no drop shadow — blurring
dozens of cards costs frames and, over a smooth gradient, looks identical.

The ambient layer is a fixed set of four large orbs (blue, blue-violet, violet,
cyan) drifting on 72-96 second cycles. It is what makes the frosted chrome read
as glass rather than as a grey bar: blur needs something with a colour cast
behind it, and something that moves. Two properties are non-negotiable — the orbs
are drawn with radial gradients rather than `filter: blur()` (a blurred
half-viewport element re-rasterizes every frame) and their drift animates
`translate3d` only, never `scale`. Judge them from a viewport-sized screenshot:
a full-page capture lays the fixed layer out over the whole document and makes
the orbs look far brighter than they are.

Reduced transparency, increased contrast, forced colors and unsupported backdrop
filters receive opaque surfaces and no ambient layer. Stored theme preference
takes priority over the initial system appearance, with initialization before CSS
to avoid a flash.

## Motion and request ownership

`web/src/core/motion.ts` owns Web Animations, their cancellation and temporary
layer hints; `web/src/shell/decorate.ts` adds the selection animations for markup
that page renderers produced imperatively. The standard curve is
`cubic-bezier(.22, 1, .36, 1)`, with `cubic-bezier(.34, 1.4, .64, 1)` for springs.
Selection affordances animate: the navigation marker slides between items (220ms),
the segmented-control thumb (`.segmented`, `.quick-dates`, `.rule-tabs`) slides
behind the active option (240ms), and switches move their knob with a spring.
Card entrances stagger by 28ms and are capped at a handful of elements; a chart
enters as a single surface, because animating every row or column separately costs
far more than it reads. Animations must stay compositor-only: never animate
`background-position`, `width` or `filter` on large surfaces.
Navigation indicators move for 220ms; pages enter for 240ms; dialogs enter for 240ms
and exit for 160ms; notifications enter/exit for 180ms. Only dialogs use a small
spring overshoot. Controls use 120-180ms feedback. Details expand with a short
opacity/translation reveal. There are no background animation loops.

Actual outgoing page controls are detached immediately. A noninteractive,
ARIA-hidden clone without IDs fades out for 70ms. This preserves existing
`isConnected` response guards. Each new page cancels the previous page's GET
requests; where supported, `AbortSignal.any` combines page and local request
signals. Older browsers retain local request cancellation and detached-node
guards. Mutations are not cancelled on navigation.

Rapid navigation cancels previous effects. Reduced-motion changes and hidden
documents settle active animations and release `will-change`. The next page
receives focus without unexpected scroll movement; navigation to another section
resets scroll to the top. Local queries retain their content while busy, and
errors append retry feedback. Busy buttons retain their width and accessible name.

Dialogs retain native top-layer behavior and focus trapping. Their close guard
runs before animation; closing controls become inert. Navigation, logout and
session expiration dispose of dialogs immediately. Trigger buttons explicitly
receive focus to support restoration in WebKit as well as Chromium and Firefox.

## Verification and artifacts

See [webui-development.md](webui-development.md) for the disposable preview and
test commands. The existing browser suite checks business behavior with reduced
motion. `scripts/test_webui_motion.py` exercises normal motion in Chromium,
Firefox and WebKit, including rapid navigation, stale responses, dialog cleanup,
theme persistence, five viewport widths, both themes, forced colors, reduced
motion, 200% CSS zoom and session expiration.

The motion suite saves screenshots, per-browser recordings and a JSON report
under `.gocache/webui-motion/`. The optional axe checks cover desktop/mobile
screens in both themes. Frame samples recorded alongside video are diagnostic,
not a guarantee of 60fps on other hardware. Validate final rendering and scroll
performance on the deployment's target devices as well.

UI changes require rebuilding the Go binary/container because assets are embedded.
The visual redesign alone requires no API or database migrations. Builtin library
2.0 separately adds audit migration `0007`; see [builtin-management.md](builtin-management.md).
Rollback uses the previous binary/image.

## Verification record: 2026-09-19

- `go test -race ./...` and the race-enabled preview package checks passed.
- JavaScript syntax and `git diff --check` passed. The application binary built
  successfully with `-buildvcs=false`, used only because this workspace's Windows
  ownership prevents Git VCS stamping; no global Git configuration was changed.
- The business suite passed all 13 scenarios and 12 axe checks.
- Chromium, Firefox and WebKit passed the motion suite with no page errors.
  The Chromium run passed another 20 axe checks across both themes and desktop/mobile.
- Additional checks verified reduced transparency, unavailable Web Animations,
  stable loading-button dimensions and a single discard confirmation when
  navigating during dialog dismissal.
- In Windows headless Chromium at 1440x1000, with video disabled, three one-second
  navigation samples measured 55-57 frames and no long tasks over 50ms. The 95th
  percentile frame interval was 33.3ms, so this is not a locked-60fps guarantee.
- Before screenshots: `.gocache/webui-before/`. Final screenshots, recordings and
  motion report: `.gocache/webui-motion/`. Business report: `.gocache/webui-after/`.
  Separate performance sample: `.gocache/webui-motion/performance-no-video.json`.
