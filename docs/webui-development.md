# WebUI Development and Verification

The panel uses Go handlers and embedded HTML/CSS/JavaScript. It requires no
frontend build or CDN. Production assets are embedded into the Go binary, so
deploying UI changes requires rebuilding the binary or container image.

## Local Preview

Run the following from the repository root in PowerShell:

~~~powershell
$env:WEBUI_PREVIEW_ADDR = '127.0.0.1:8765'
go test -tags webuipreview -run '^TestWebUIPreview$' -v ./internal/webui -timeout 0
~~~

On Debian/Linux:

~~~bash
WEBUI_PREVIEW_ADDR=127.0.0.1:8765 go test -tags webuipreview -run '^TestWebUIPreview$' -v ./internal/webui -timeout 0
~~~

Open <http://127.0.0.1:8765>. Login: `admin` / `preview-only`.
Use another free loopback port if 8765 is occupied.

The preview uses real panel handlers with disposable in-memory stores. It does
not connect to Telegram or PostgreSQL. The fixture includes enabled/disabled
rules, long patterns, multiple groups, deletion failures and multiline summaries.
Builtin fixtures include categorized detections with stored explanation snapshots,
legacy IDs without details and an unknown historical ID. The fixture uses the
real builtin catalog and detector; rule counts are not fixed in the browser suite.
Changes disappear when the process stops. Assets are read from disk, so reloading
the browser picks up frontend edits. Stop with Ctrl+C. The `webuipreview` build tag
and `_test.go` file keep this fixture out of production builds.

## Browser Regression

Keep the preview running and use a second terminal. Install Python Playwright
in a local virtual environment. PowerShell:

~~~powershell
py -3 -m venv .venv
.\.venv\Scripts\python.exe -m pip install playwright==1.63.0
.\.venv\Scripts\python.exe -m playwright install chromium
.\.venv\Scripts\python.exe scripts/test_webui.py
~~~

Debian/Linux:

~~~bash
python3 -m venv .venv
.venv/bin/python -m pip install playwright==1.63.0
.venv/bin/python -m playwright install --with-deps chromium
.venv/bin/python scripts/test_webui.py
~~~

The script only accepts localhost and checks the `X-WebUI-Preview` response
header before changing data. Run against a fresh fixture; restart the preview
if a failed test left its sample rules or credentials changed. Pass
`--base-url http://127.0.0.1:8766` to use another preview port.

Screenshots and `report.json` are written to `.gocache/webui-screenshots/`.
Checks cover:

- Trend periods, keyboard day selection, chart/table switching, axis scaling
  and zero-hit data.
- Rule search, status filters, URL persistence, pagination, toggle rollback,
  cache warnings, regex validation, editing, creation, deletion and export.
- Unsaved-change confirmation and forward/backward modal focus cycling.
- Builtin categories, library version, combined detection conditions,
  master/per-rule switches, saved state, effective counts and failed-save rollback.
- Builtin text tests for money laundering, illicit drug promotion and personal
  data sales, benign discussion, Unicode limits, disabled detectors, API failures
  and stale responses after editing. Test requests must not create audit records.
- Audit filters, pagination, long summaries rendered as text, failure details
  and clipboard feedback; stored builtin reasons and versions, legacy catalog
  names, unknown-ID fallback and safe rendering of explanation labels.
- Failed requests, retries and late responses after navigation.
- 1440, 768, 390 and 320 pixel layouts, light/dark themes and mobile dialogs.
- Account validation, credential changes, expired sessions and logout.

For optional WCAG A/AA checks, install axe-core into an ignored directory:

~~~powershell
npm install --prefix .gocache/webui-tools axe-core@4.13.0
.\.venv\Scripts\python.exe scripts/test_webui.py --axe .gocache/webui-tools/node_modules/axe-core/axe.min.js
~~~

On Linux use `.venv/bin/python` for the last command. Automated accessibility
checks supplement manual screenshot and keyboard review; they do not establish
full WCAG compliance. This business regression suite targets Chromium. The
additional motion suite also checks Firefox and WebKit.

## Motion and Visual Regression

The design tokens, surface hierarchy, motion timings and request ownership rules
are documented in [webui-design.md](webui-design.md).

With the disposable preview running, use PowerShell:

~~~powershell
.\.venv\Scripts\python.exe -m playwright install chromium firefox webkit
.\.venv\Scripts\python.exe scripts/test_webui_motion.py
~~~

Pass `--axe <path-to-axe.min.js>` to include accessibility checks in both themes.
Use `--browsers chromium` for a targeted run. Screenshots, `<browser>-motion.webm`
recordings and `report.json` are written to `.gocache/webui-motion/` by default.
This suite uses normal animation, checks interruption and cleanup, and covers
1920/1440/768/390/320px viewports. It also checks reduced motion, forced colors,
theme persistence, 200% CSS zoom, modal focus restoration and expired sessions.
The suite does not relax the production Content Security Policy.
Run it after the business regression suite, or use a separate preview port with
`--base-url`. Account changes in the business suite invalidate other sessions on
the same preview server.

## Go and Static Checks

Run outside the terminal with `WEBUI_PREVIEW_ADDR` set:

~~~text
go test -race ./...
go test -race -tags webuipreview ./internal/webui
go test -race ./internal/store -run '^(TestBuiltinSettingsRepositoryIntegration|TestPanelAuditBuiltinHitsIntegration|TestAuditDistinctMessagesIntegration)$' -count=1 -v
node --check internal/webui/assets/app.js
node --check internal/webui/assets/theme.js
node --check internal/webui/assets/motion.js
git diff --check
~~~

Go race tests require CGO and an available C compiler. The preview test skips
when `WEBUI_PREVIEW_ADDR` is unset.

The builtin settings and audit integration tests require `TEST_DATABASE_URL`
pointing at a disposable PostgreSQL test database with schema-creation permission.
Each applies the embedded migrations in its own temporary schema and removes that
schema on completion. They verify settings survive checker recreation, legacy and
new JSONB audit details round-trip, original content summaries/hashes are retained,
and duplicate message events count as one strike within the window. Without the
variable they skip; the preview and browser tests use in-memory storage.

## Data and Assets

Audit summaries are limited to 120 characters by the existing store. Expanding
or copying a summary does not recover the original full message. All API text,
including patterns and Telegram content, is rendered as text nodes.

`POST /api/builtin-rules/test` accepts `{ "text": "sample message" }` through
the existing authenticated, CSRF-protected API. It accepts 1–4096 Unicode code
points (including astral characters as one code point), rejects blank text and
uses the current builtin settings. It returns `enabled`, `matched`,
`library_version` and detailed `hits`. The operation only analyzes the supplied
text; it does not store samples, create audits or call Telegram. Hidden links,
buttons and forwarding metadata are available only on actual Telegram messages.

Audit `builtin_details` is optional and reflects the version and evidence labels
saved when the message was processed. The UI never re-analyzes historical
summaries. Older `builtin_hits` resolve names through the builtin catalog; IDs
absent from that catalog remain visible unchanged.

`internal/webui/assets/icons.svg` contains selected icons from
`lucide-static@1.47.0`. Its ISC license is included as
`internal/webui/assets/lucide-LICENSE.txt`.
