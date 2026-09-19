# WebUI visual and motion system

The moderation console uses graphite surfaces, a teal accent and translucent
navigation. Content sections stay unframed; tables, filters and repeated metrics
have defined boundaries. The interface uses the existing system-font stack and
local Lucide sprite. There are no remote fonts, CDN scripts or frontend build steps.

## Tokens and layout

| Role | Light | Dark |
| --- | --- | --- |
| Background | `#f3f6f5` | `#111619` |
| Content surface | `#ffffff` | `#1b2327` |
| Primary text | `#202a29` | `#edf3f2` |
| Secondary text | `#586965` | `#aab9b7` |
| Accent | `#08786f` | `#71d5be` |
| Glass fill | white / 82% | content surface / 86% |

Semantic success, warning and error colors remain independent of the brand
accent. Headings use 28/18px, body text 14px, secondary information at least 12px,
and primary metrics 36px. Mobile fields use 16px to avoid automatic input zoom.
Letter spacing is zero. Spacing follows 4/8/12/16/24/32px increments.

Controls use 10px radii, framed data tools and metrics 8px, navigation chrome
16px and dialogs 20px. The 216px desktop sidebar and 64px topbar have 12px outer
insets. Content is capped at 1600px. Below 900px the sidebar becomes a horizontal
navigation bar; below 640px labels stack beneath icons and tables become labeled
records. Dashboard trend/failure columns collapse below 1200px.

Glass is limited to navigation, login and dialogs, with static 16px blur (10px
on mobile), a fine edge highlight and diffuse shadow. The ambient background is
static. Reduced transparency, increased contrast, forced colors and unsupported
backdrop filters receive opaque surfaces. Stored theme preference takes priority
over the initial system appearance, with initialization before CSS to avoid a flash.

## Motion and request ownership

`motion.js` owns Web Animations, their cancellation and temporary layer hints.
The standard curve is `cubic-bezier(.22, 1, .36, 1)`. Navigation indicators move
for 220ms; pages enter for 240ms; metrics stagger by 30ms; dialogs enter for 240ms
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
