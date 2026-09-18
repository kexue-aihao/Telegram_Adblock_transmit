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
- Builtin detector catalog, master/per-rule switches, saved state, effective
  state counts and rollback after a failed save.
- Audit filters, pagination, long summaries rendered as text, failure details
  and clipboard feedback.
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
full WCAG compliance. The browser suite currently targets Chromium.

## Go and Static Checks

Run outside the terminal with `WEBUI_PREVIEW_ADDR` set:

~~~text
go test -race ./...
go test -race -tags webuipreview ./internal/webui
go test ./internal/store -run '^TestBuiltinSettingsRepositoryIntegration$' -v
node --check internal/webui/assets/app.js
node --check internal/webui/assets/theme.js
git diff --check
~~~

Go race tests require CGO and an available C compiler. The preview test skips
when `WEBUI_PREVIEW_ADDR` is unset.

The builtin repository integration test requires `TEST_DATABASE_URL` pointing at
a disposable PostgreSQL test database with schema-creation permission. It applies
the embedded migrations in its own temporary schema, verifies settings survive
checker recreation and removes that schema on completion. Without the variable
it skips; the preview and browser tests use in-memory storage.

## Data and Assets

Audit summaries are limited to 120 characters by the existing store. Expanding
or copying a summary does not recover the original full message. All API text,
including patterns and Telegram content, is rendered as text nodes.

`internal/webui/assets/icons.svg` contains selected icons from
`lucide-static@1.47.0`. Its ISC license is included as
`internal/webui/assets/lucide-LICENSE.txt`.
