"""Visual and interruptible-motion regression checks on the disposable preview."""

import argparse
import json
import time
from pathlib import Path
from urllib.parse import urlparse

from playwright.sync_api import expect, sync_playwright


def run(base_url, output, engines, axe_path=None):
    assert urlparse(base_url).hostname in ("127.0.0.1", "localhost")
    output.mkdir(parents=True, exist_ok=True)
    results = []
    with sync_playwright() as playwright:
        for engine in engines:
            browser = getattr(playwright, engine).launch()
            context = browser.new_context(
                viewport={"width": 1440, "height": 1000}, color_scheme="dark",
                reduced_motion="no-preference",
                record_video_dir=str(output / "video" / engine),
                record_video_size={"width": 1440, "height": 1000},
            )
            page = context.new_page()
            video = page.video
            errors = []
            page.on("pageerror", lambda error: errors.append(str(error)))
            response = page.goto(base_url)
            assert response.headers.get("x-webui-preview") == "true", "Use the disposable preview only"
            page.get_by_label("用户名", exact=True).fill("admin")
            page.get_by_label("密码", exact=True).fill("preview-only")
            page.get_by_role("button", name="登录", exact=True).click()

            def settle():
                # Playwright's wait_for_function evaluates a string inside the page and
                # conflicts with the production CSP. Poll without relaxing that policy.
                deadline = time.monotonic() + 5
                while not page.evaluate("document.getAnimations().filter(a => !a.effect.target.closest?.('.ambient')).every(a => a.playState === 'finished') && [...document.querySelectorAll('[style]')].every(n => !n.style.willChange)"):
                    assert time.monotonic() < deadline, "Animation did not settle"
                    page.wait_for_timeout(25)
                expect(page.locator(".page-exit")).to_have_count(0)
                assert page.evaluate("[...document.querySelectorAll('[style]')].every(n => !n.style.willChange)")

            def visit(route, ready):
                page.evaluate("route => location.hash = '#/' + route", route)
                expect(page.locator(ready).first).to_be_visible()
                page.wait_for_load_state("networkidle")
                settle()

            expect(page.locator(".stats-grid .stat")).to_have_count(4)
            settle()

            # Several actual route events within one transition, including a suspended request.
            held = []
            page.route("**/api/settings/account", lambda route: held.append(route))
            page.get_by_role("link", name="设置", exact=True).click()
            expect(page.locator(".loading")).to_be_visible()
            page.evaluate("""async () => {
                for (const route of ['audit', 'dashboard', 'rules', 'builtin', 'rules']) {
                    location.hash = '#/' + route;
                    await new Promise(resolve => setTimeout(resolve, 35));
                }
            }""")
            expect(page.locator(".rules-table")).to_be_visible()
            for request in held:
                request.fulfill(status=200, content_type="application/json", body='{"username":"obsolete"}')
            page.unroute("**/api/settings/account")
            settle()
            expect(page.get_by_role("heading", name="规则管理", exact=True)).to_be_visible()
            expect(page.locator('.nav-item[aria-current="page"]')).to_have_attribute("data-route", "rules")

            # Ordinary close restores focus; navigation during close clears the top layer.
            for _ in range(3):
                opener = page.get_by_role("button", name="新增规则", exact=True)
                opener.click()
                expect(page.get_by_role("dialog")).to_be_visible()
                page.keyboard.press("Escape")
                expect(page.locator("dialog")).to_have_count(0)
                expect(opener).to_be_focused()
            opener.click()
            page.keyboard.press("Escape")
            visit("audit", ".audit-table")
            expect(page.locator("dialog")).to_have_count(0)

            # Repeated theme changes persist the final preference across reloads.
            page.evaluate("""() => {
                for (let i = 0; i < 7; i++) document.getElementById('theme-toggle').click();
            }""")
            theme = page.locator("html").get_attribute("data-theme")
            page.reload()
            expect(page.locator("html")).to_have_attribute("data-theme", theme)
            expect(page.locator(".audit-table")).to_be_visible()
            settle()

            # The palette is the second appearance axis. It is independent of
            # light/dark, it repaints the brand colour, and it also persists.
            brand = "() => getComputedStyle(document.querySelector('.brand-mark')).backgroundImage"
            brand_before = page.evaluate(brand)
            visit("settings", ".palette-grid")
            page.get_by_role("radio", name="琥珀", exact=True).check()
            expect(page.locator("html")).to_have_attribute("data-accent", "amber")
            assert page.evaluate(brand) != brand_before, (engine, "palette did not repaint the brand mark")
            # A swatch paints the palette it offers, not the one that is active.
            swatch = page.evaluate("""() => {
                const chip = document.querySelector('.palette-chip[data-palette="teal"]');
                return {
                    chip: getComputedStyle(chip).getPropertyValue('--color-accent').trim(),
                    active: getComputedStyle(document.documentElement).getPropertyValue('--color-accent').trim(),
                };
            }""")
            assert swatch["chip"] and swatch["chip"] != swatch["active"], (engine, swatch)
            settle()
            page.reload()
            expect(page.locator("html")).to_have_attribute("data-accent", "amber")
            expect(page.locator("html")).to_have_attribute("data-theme", theme)
            visit("settings", ".palette-grid")
            page.get_by_role("radio", name="湛蓝", exact=True).check()
            expect(page.locator("html")).to_have_attribute("data-accent", "azure")
            settle()
            visit("audit", ".audit-table")
            settle()

            # Collect a bounded frame sample; results describe the host, not a universal FPS claim.
            perf = page.evaluate("""async () => {
                const frames = [], tasks = [];
                let observer;
                if (PerformanceObserver.supportedEntryTypes?.includes('longtask')) {
                    observer = new PerformanceObserver(list => tasks.push(...list.getEntries().map(e => e.duration)));
                    observer.observe({type: 'longtask'});
                }
                const start = performance.now(); let previous = start;
                location.hash = '#/dashboard';
                await new Promise(resolve => {
                    function sample(now) {
                        frames.push(now - previous); previous = now;
                        if (now - start < 900) requestAnimationFrame(sample); else resolve();
                    }
                    requestAnimationFrame(sample);
                });
                observer?.disconnect();
                frames.sort((a,b) => a-b);
                return {frames: frames.length, p95_frame_ms: frames[Math.floor(frames.length*.95)], long_tasks_ms: tasks};
            }""")
            settle()

            routes = [("dashboard", ".chart-day"), ("rules", ".rules-table"),
                      ("builtin", ".builtin-rule"), ("audit", ".audit-table"), ("settings", ".settings-section")]
            accessibility_count = 0
            for width, height in [(1920, 1080), (1440, 1000), (768, 1024), (390, 844), (320, 740)]:
                page.set_viewport_size({"width": width, "height": height})
                for appearance in ("dark", "light"):
                    if page.locator("html").get_attribute("data-theme") != appearance:
                        page.locator("#theme-toggle").click()
                    for route, ready in routes:
                        visit(route, ready)
                        assert page.evaluate("document.documentElement.scrollWidth <= innerWidth"), (engine, width, route)
                        # The panel is a fixed-viewport shell: the document never
                        # scrolls and #view is the only scroller. If .shell stops
                        # stretching to the viewport (it must be a full-height flex
                        # column for that), every wrapper grows to its content, #view
                        # gets no scrollable overflow and body{overflow:hidden} clips
                        # the rest — a long page with a dead wheel. Assert the shell
                        # fits the window and that a taller page is reachable.
                        scroll = page.evaluate("""() => {
                            const view = document.querySelector('#view');
                            const shell = document.querySelector('.shell').getBoundingClientRect();
                            const doc = document.scrollingElement;
                            const overflow = view.scrollHeight - view.clientHeight;
                            let reached = true;
                            if (overflow > 1) {
                                view.scrollTop = overflow;
                                reached = view.scrollTop >= overflow - 1;
                                view.scrollTop = 0;
                            }
                            return {
                                shellFits: shell.height <= innerHeight + 1 && shell.bottom <= innerHeight + 1,
                                documentFixed: !(doc.scrollHeight > doc.clientHeight + 1),
                                // #view is the scroller, never a descendant.
                                viewIsScroller: getComputedStyle(view).overflowY === 'auto',
                                reached: reached,
                            };
                        }""")
                        assert all(scroll.values()), (engine, width, route, appearance, scroll)
                        # The selection marker is a thin bar, so what has to hold is
                        # that its centre stays on the active item's centre after both
                        # navigation and resize.
                        delta = page.evaluate("""() => {
                            const a = document.querySelector('.nav-indicator').getBoundingClientRect();
                            const b = document.querySelector('.nav-item[aria-current]').getBoundingClientRect();
                            // Desktop: a vertical rail bar, so vertical tracking is
                            // what matters. Mobile: the bar lies flat, so horizontal.
                            const nav = document.querySelector('.nav').getBoundingClientRect();
                            const horizontal = nav.width > nav.height * 2;
                            return horizontal
                              ? Math.abs((a.x + a.width / 2) - (b.x + b.width / 2))
                              : Math.abs((a.y + a.height / 2) - (b.y + b.height / 2));
                        }""")
                        assert delta < 2, (engine, width, route, delta)
                        if engine == "chromium" and width in (1440, 390):
                            page.screenshot(path=str(output / f"{width}-{route}-{appearance}.png"), full_page=True)
                        if axe_path and engine == "chromium" and width in (1440, 390):
                            page.evaluate(axe_path.read_text(encoding="utf-8"))
                            violations = page.evaluate("""async () => (await axe.run(document, {runOnly: {
                                type:'tag', values:['wcag2a','wcag2aa','wcag21aa']}})).violations""")
                            assert not violations, (width, route, appearance, violations)
                            accessibility_count += 1

            page.set_viewport_size({"width": 1440, "height": 1000})
            visit("rules", ".rules-table")
            page.get_by_role("button", name="新增规则", exact=True).click()
            settle()
            if engine == "chromium":
                page.screenshot(path=str(output / "rule-editor.png"))
            # Changing the OS motion preference while a dialog exits must still remove it.
            page.keyboard.press("Escape")
            page.emulate_media(reduced_motion="reduce")
            expect(page.locator("dialog")).to_have_count(0)
            visit("dashboard", ".chart-day")
            assert page.evaluate("document.getAnimations().length === 0")
            page.emulate_media(reduced_motion="no-preference", forced_colors="active")
            assert page.locator(".topbar").evaluate("n => getComputedStyle(n).backdropFilter") in ("blur(0px)", "none", None)
            page.emulate_media(forced_colors="none")
            page.add_style_tag(content="html { zoom: 2; }")
            assert page.evaluate("document.documentElement.scrollWidth <= innerWidth"), "200% CSS zoom overflow"

            # Session expiry during an open dialog must bypass exit animation and clear controls.
            page.route("**/api/audit/*", lambda route: route.fulfill(status=401, content_type="application/json",
                       body=json.dumps({"error": "会话已过期", "code": "unauthorized"})))
            visit("audit", ".audit-table")
            page.get_by_role("button", name="查看审计记录 #96", exact=True).click()
            expect(page.get_by_role("heading", name="登录管理面板", exact=True)).to_be_visible()
            expect(page.locator("dialog")).to_have_count(0)
            settle()
            assert not errors, errors
            results.append({"browser": engine, "performance": perf, "accessibility_checks": accessibility_count,
                            "page_errors": errors, "checks": "navigation interruption, late response, modal focus/cleanup, theme persistence, palette persistence, responsive layout, reduced motion, forced colors, CSS zoom, session expiry"})
            context.close()
            video.save_as(str(output / f"{engine}-motion.webm"))
            video.delete()
            browser.close()
            print("PASS:", engine, json.dumps(perf), flush=True)
            (output / "report.json").write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding="utf-8")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", default="http://127.0.0.1:8765")
    parser.add_argument("--output", type=Path, default=Path(".gocache/webui-motion"))
    parser.add_argument("--browsers", nargs="+", choices=["chromium", "firefox", "webkit"], default=["chromium", "firefox", "webkit"])
    parser.add_argument("--axe", type=Path)
    args = parser.parse_args()
    run(args.base_url.rstrip("/"), args.output, args.browsers, args.axe)
