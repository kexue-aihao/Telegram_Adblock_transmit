"""Browser regression checks against the disposable Go WebUI preview."""

import argparse
import json
import re
from pathlib import Path
from urllib.parse import urlparse

from playwright.sync_api import expect, sync_playwright


def run(base_url, output, axe_path=None):
    assert urlparse(base_url).hostname in ("127.0.0.1", "localhost"), "Use a local preview only"
    output.mkdir(parents=True, exist_ok=True)
    checks = []
    errors = []
    accessibility = []

    def passed(name):
        checks.append(name)
        print("PASS:", name, flush=True)

    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        context = browser.new_context(
            viewport={"width": 1440, "height": 1000},
            color_scheme="light",
            permissions=["clipboard-read", "clipboard-write"],
            reduced_motion="reduce",
        )
        page = context.new_page()
        page.on("pageerror", lambda error: errors.append(str(error)))
        response = page.goto(base_url)
        assert response.headers.get("x-webui-preview") == "true", "Refusing to mutate a non-preview server"
        page.wait_for_load_state("networkidle")

        def login(username="admin"):
            page.get_by_label("用户名", exact=True).fill(username)
            page.get_by_label("密码", exact=True).fill("preview-only")
            page.get_by_role("button", name="登录", exact=True).click()
            expect(page.locator("#sidebar")).to_be_visible()
            page.wait_for_load_state("networkidle")

        def visit(route, ready=None):
            page.goto(base_url + "/#/" + route)
            if ready:
                expect(page.locator(ready).first).to_be_visible()
            page.wait_for_load_state("networkidle")

        def audit_accessibility(name):
            if not axe_path:
                return
            page.evaluate(axe_path.read_text(encoding="utf-8"))
            result = page.evaluate("""async () => {
                const r = await axe.run(document, {runOnly: {
                    type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa']
                }});
                return r.violations.map(v => ({
                    id: v.id, impact: v.impact,
                    nodes: v.nodes.map(n => ({target: n.target, summary: n.failureSummary}))
                }));
            }""")
            accessibility.append({"page": name, "violations": result})
            assert not result, json.dumps(accessibility[-1], ensure_ascii=False)

        login()
        expect(page.get_by_role("heading", name="仪表盘", exact=True)).to_be_visible()
        expect(page.locator(".stats-grid .stat")).to_have_count(4)
        expect(page.locator(".chart-day")).to_have_count(30)
        page.screenshot(path=str(output / "desktop-dashboard.png"), full_page=True)
        audit_accessibility("dashboard-light")
        page.get_by_role("button", name="7 天", exact=True).click()
        expect(page.locator(".chart-day")).to_have_count(7)
        expect(page).to_have_url(re.compile(r"days=7"))
        page.locator(".chart-day[tabindex='0']").focus()
        page.keyboard.press("ArrowLeft")
        expect(page.locator(".chart-detail strong")).to_contain_text("UTC")
        page.get_by_role("button", name="数据", exact=True).click()
        expect(page.locator(".trend-table tbody tr")).to_have_count(7)
        page.get_by_role("button", name="图表", exact=True).click()
        passed("dashboard period, accessible chart/table and keyboard selection")
        page.route("**/api/dashboard/trend?*", lambda route: route.fulfill(
            status=200, content_type="application/json",
            body=json.dumps([{"date": "2026-01-01", "hits": 20, "deleted": 18, "failed": 2}])))
        page.get_by_role("button", name="30 天", exact=True).click()
        expect(page.locator(".chart-day")).to_have_count(1)
        expect(page.locator(".chart-label").nth(4)).to_have_text("20")
        page.unroute("**/api/dashboard/trend?*")
        page.route("**/api/dashboard/trend?*", lambda route: route.fulfill(
            status=200, content_type="application/json",
            body=json.dumps([{"date": "2026-01-01", "hits": 0, "deleted": 0, "failed": 0}])))
        page.get_by_role("button", name="90 天", exact=True).click()
        expect(page.get_by_text("该时段暂无广告命中", exact=True)).to_be_visible()
        expect(page.locator(".chart-label").nth(4)).to_have_text("4")
        page.unroute("**/api/dashboard/trend?*")
        passed("readable chart scaling and zero-hit trend state")

        page.get_by_role("link", name="规则管理", exact=True).click()
        expect(page.locator(".rules-table tbody tr")).to_have_count(20)
        page.screenshot(path=str(output / "desktop-rules.png"), full_page=True)
        audit_accessibility("rules-light")
        page.get_by_label("搜索规则", exact=True).fill("免费")
        expect(page.locator(".rules-table tbody tr")).to_have_count(4)
        page.get_by_label("状态", exact=True).select_option("enabled")
        expect(page.locator(".rules-table tbody tr")).to_have_count(1)
        page.reload()
        expect(page.get_by_label("搜索规则", exact=True)).to_have_value("免费")
        expect(page.get_by_label("状态", exact=True)).to_have_value("enabled")
        expect(page.locator(".rules-table tbody tr")).to_have_count(1)
        page.get_by_role("button", name="重置", exact=True).click()
        page.get_by_role("button", name="下一页", exact=True).click()
        expect(page.locator(".rules-table tbody tr")).to_have_count(7)
        page.reload()
        expect(page.locator(".rules-table tbody tr")).to_have_count(7)
        passed("rule search, status filtering and URL-preserved pagination")

        page.get_by_label("搜索规则", exact=True).fill("#3")
        expect(page.locator(".rules-table tbody tr")).to_have_count(1)
        toggle = page.get_by_role("switch", name="启用规则 #3", exact=True)
        before = toggle.is_checked()
        page.route("**/api/rules/3", lambda route: route.fulfill(
            status=503, content_type="application/json", body=json.dumps({"error": "模拟服务暂时不可用"}))
            if route.request.method == "PATCH" else route.continue_())
        toggle.click()
        expect(page.locator(".row-error")).to_contain_text("模拟服务暂时不可用")
        assert toggle.is_checked() == before
        page.unroute("**/api/rules/3")
        def cache_warning(route):
            response = route.fetch()
            payload = response.json()
            payload["warning"] = "cache_refresh_failed"
            route.fulfill(response=response, json=payload)

        page.route("**/api/rules/3", cache_warning)
        toggle.click()
        expect(toggle).to_be_checked(checked=not before)
        expect(page.locator("#toast-region")).to_contain_text("可能尚未生效")
        page.unroute("**/api/rules/3")
        toggle.click()
        expect(toggle).to_be_checked(checked=before)
        passed("rule toggle rollback on failure and successful state changes")

        page.get_by_role("button", name="编辑规则 #3", exact=True).click()
        dialog = page.get_by_role("dialog")
        expect(dialog).to_be_visible()
        original = page.get_by_label("正则表达式", exact=True).input_value()
        for key in ("Tab", "Shift+Tab"):
            for _ in range(14):
                page.keyboard.press(key)
                assert page.evaluate("!!document.activeElement.closest('dialog')")
        page.get_by_label("正则表达式", exact=True).fill("browser_test.*match")
        page.get_by_label("测试文本", exact=True).fill("BROWSER_TEST something MATCH")
        page.get_by_role("button", name="测试匹配", exact=True).click()
        expect(page.locator(".test-result")).to_contain_text("匹配，会触发拦截")
        page.get_by_label("测试文本", exact=True).fill("unrelated")
        expect(page.locator(".test-result")).to_be_empty()
        page.get_by_role("button", name="测试匹配", exact=True).click()
        expect(page.locator(".test-result")).to_contain_text("未匹配")
        page.get_by_label("正则表达式", exact=True).fill("(?=unsupported)")
        page.get_by_role("button", name="保存修改", exact=True).click()
        expect(page.locator("#rm-error [role='alert']")).to_be_visible()
        expect(dialog).to_be_visible()
        page.get_by_label("正则表达式", exact=True).fill(original + "|browser_test")
        page.get_by_role("button", name="保存修改", exact=True).click()
        expect(dialog).to_have_count(0)
        expect(page.locator(".pattern-text")).to_contain_text("|browser_test")
        page.get_by_role("button", name="编辑规则 #3", exact=True).click()
        page.get_by_label("正则表达式", exact=True).fill(original)
        page.get_by_role("button", name="保存修改", exact=True).click()
        expect(dialog).to_have_count(0)
        passed("rule editing, server regex validation, stale-result clearing and modal focus")

        page.get_by_role("button", name="新增规则", exact=True).click()
        page.get_by_label("正则表达式", exact=True).fill("browser_smoke_new_rule")
        page.once("dialog", lambda message: message.dismiss())
        page.keyboard.press("Escape")
        expect(dialog).to_be_visible()
        page.once("dialog", lambda message: message.accept())
        page.keyboard.press("Escape")
        expect(dialog).to_have_count(0)
        page.get_by_role("button", name="新增规则", exact=True).click()
        page.get_by_label("正则表达式", exact=True).fill("browser_smoke_new_rule")
        page.get_by_role("button", name="添加规则", exact=True).click()
        expect(dialog).to_have_count(0)
        page.get_by_label("搜索规则", exact=True).fill("browser_smoke_new_rule")
        expect(page.locator(".rules-table tbody tr")).to_have_count(1)
        page.locator(".rules-table .danger-quiet").click()
        expect(dialog).to_contain_text("此操作无法撤销")
        page.get_by_role("button", name="删除规则", exact=True).click()
        expect(dialog).to_have_count(0)
        expect(page.get_by_text("没有符合条件的规则", exact=True)).to_be_visible()
        with page.expect_download() as download:
            page.get_by_role("button", name="导出规则", exact=True).click()
        exported = json.loads(Path(download.value.path()).read_text(encoding="utf-8"))
        assert len(exported) == 27
        passed("unsaved-change protection, rule creation/deletion and JSON export")

        page.get_by_role("link", name="审计日志", exact=True).click()
        expect(page.locator(".audit-table tbody tr")).to_have_count(20)
        page.screenshot(path=str(output / "desktop-audit.png"), full_page=True)
        audit_accessibility("audit-light")
        for _ in range(2):
            page.get_by_role("button", name="下一页", exact=True).click()
            expect(page.locator("#audit-region .pagination")).to_have_count(1)
        expect(page.locator(".pagination-info")).to_contain_text("3 / 5")
        page.get_by_label("删除结果", exact=True).select_option("false")
        expect(page.locator(".audit-table tbody tr")).to_have_count(14)
        expect(page.locator(".pagination-info")).to_contain_text("1 / 1")
        assert all("not enough rights" in text for text in page.locator(".failure-reason").all_text_contents())
        page.reload()
        expect(page.get_by_label("删除结果", exact=True)).to_have_value("false")
        expect(page.locator(".audit-table tbody tr")).to_have_count(14)
        page.locator(".summary-expand summary").first.click()
        expect(page.locator(".summary-expand[open]")).to_have_count(1)
        expect(page.locator(".summary-expand img")).to_have_count(0)
        page.get_by_role("button", name="查看审计记录 #96", exact=True).click()
        expect(dialog.get_by_role("heading", name="删除失败原因", exact=True)).to_be_visible()
        expect(dialog).to_contain_text("not enough rights")
        expect(dialog).to_contain_text("最多 120 字")
        audit_record = context.request.get(base_url + "/api/audit/96").json()
        expect(dialog.locator(".builtin-audit-explanation")).to_contain_text(audit_record["builtin_details"]["library_version"])
        expect(dialog.locator(".builtin-evidence > li")).to_have_count(len(audit_record["builtin_details"]["hits"]))
        assert len(audit_record["builtin_details"]["hits"]) >= 2
        expect(dialog.locator("img")).to_have_count(0)
        page.get_by_role("button", name="复制摘要", exact=True).click()
        expect(dialog.locator(".modal-feedback")).to_contain_text("摘要已复制")
        audit_accessibility("audit-detail")
        page.screenshot(path=str(output / "desktop-audit-detail.png"))
        page.keyboard.press("Escape")
        expect(dialog).to_have_count(0)
        passed("audit pagination, failure filters, safe summary expansion, details and clipboard")

        page.get_by_role("button", name="重置", exact=True).click()
        catalog = context.request.get(base_url + "/api/builtin-rules").json()
        catalog_names = {rule["id"]: rule["name"] for rule in catalog["rules"]}
        expect(page.locator('.badge.builtin[title="ad_bot_mention"]').first).to_have_text(catalog_names["ad_bot_mention"])
        expect(page.locator('.badge.builtin[title="ad_legacy_unknown"]').first).to_have_text("ad_legacy_unknown")
        page.get_by_role("button", name="查看审计记录 #93", exact=True).click()
        expect(dialog.locator(".builtin-audit-legacy")).to_contain_text("未保存规则库版本和详细原因")
        expect(dialog.locator(".builtin-audit-explanation")).to_have_count(0)
        page.keyboard.press("Escape")
        # Stored explanations retain their original metadata across catalog updates.
        snapshot = json.loads(json.dumps(audit_record))
        snapshot["builtin_details"]["library_version"] = "1.9.0-preview"
        snapshot["builtin_details"]["hits"][0]["name"] = "旧版检测名称 <img src=x onerror=alert(1)>"
        snapshot["builtin_details"]["hits"][0]["evidence"] = ["旧版证据标签 <img src=x onerror=alert(1)>"]
        page.route("**/api/audit/96", lambda route: route.fulfill(status=200, content_type="application/json", body=json.dumps(snapshot)))
        page.get_by_role("button", name="查看审计记录 #96", exact=True).click()
        expect(dialog.locator(".builtin-audit-explanation")).to_contain_text("1.9.0-preview")
        expect(dialog.locator(".builtin-audit-explanation")).to_contain_text("旧版证据标签 <img")
        expect(dialog.locator("img")).to_have_count(0)
        page.keyboard.press("Escape")
        page.unroute("**/api/audit/96")
        passed("builtin audit snapshots, multi-category reasons, legacy names and unknown ID fallback")
        page.get_by_role("button", name="近 7 天", exact=True).click()
        expect(page.locator("#audit-region .result-count")).to_contain_text("28")
        page.get_by_label("结束日期 (UTC)", exact=True).fill("2000-01-01")
        page.get_by_role("button", name="查询", exact=True).click()
        expect(page.locator("#audit-filter-error")).to_contain_text("开始日期不能晚于结束日期")
        page.get_by_role("button", name="重置", exact=True).click()
        page.get_by_label("群组", exact=True).select_option("-1001234567")
        expect(page.locator("#audit-region .result-count")).to_contain_text("32")
        page.get_by_label("命中规则 ID", exact=True).fill("1")
        page.get_by_role("button", name="查询", exact=True).click()
        expect(page.locator("#audit-region .result-count")).to_contain_text("8")
        page.get_by_role("button", name="重置", exact=True).click()
        page.get_by_label("每页条数", exact=True).select_option("50")
        expect(page.locator(".audit-table tbody tr")).to_have_count(50)
        passed("audit date presets, validation, combined filters and page size")

        page.route("**/api/audit?*", lambda route: route.fulfill(status=503, content_type="application/json",
            body=json.dumps({"error": "模拟查询失败"})))
        page.get_by_role("button", name="刷新日志", exact=True).click()
        expect(page.locator("#audit-region")).to_contain_text("模拟查询失败")
        page.unroute("**/api/audit?*")
        page.get_by_role("button", name="重试", exact=True).click()
        expect(page.locator(".audit-table tbody tr")).to_have_count(50)
        pending = []
        page.route("**/api/rules", lambda route: pending.append(route))
        page.get_by_role("link", name="规则管理", exact=True).click()
        expect(page.get_by_text("正在加载规则…", exact=True)).to_be_visible()
        page.get_by_role("link", name="审计日志", exact=True).click()
        expect(page.locator(".audit-table tbody tr")).to_have_count(50)
        assert pending
        pending[0].fulfill(status=200, content_type="application/json", body="[]")
        page.unroute("**/api/rules")
        expect(page.get_by_role("heading", name="审计日志", exact=True)).to_be_visible()
        expect(page.locator(".audit-table tbody tr")).to_have_count(50)
        passed("recoverable query errors and stale navigation response isolation")

        visit("rules", ".rules-table")
        page.get_by_role("link", name="内置广告库", exact=True).click()
        catalog = context.request.get(base_url + "/api/builtin-rules").json()
        rule_count = len(catalog["rules"])
        expect(page.locator(".builtin-rule")).to_have_count(rule_count)
        expect(page.locator(".result-count")).to_contain_text(f"当前生效 {rule_count} 项")
        expect(page.locator("#builtin-version")).to_contain_text(catalog["library_version"])
        expect(page.locator(".builtin-group")).to_have_count(len({rule["category"] for rule in catalog["rules"]}))
        assert all(rule["category"] and rule["conditions"] for rule in catalog["rules"])
        audit_accessibility("builtin-light")
        master = page.get_by_role("switch", name="内置防护总开关", exact=True)
        bot_rule = page.locator("#builtin-ad_bot_mention")
        test_text = page.get_by_label("待检测文本", exact=True)
        test_button = page.get_by_role("button", name="测试文本", exact=True)
        test_result = page.locator("#builtin-test-result")
        page.route("**/api/builtin-rules", lambda route: route.fulfill(status=503, content_type="application/json",
            body=json.dumps({"error": "模拟配置保存失败"})))
        bot_rule.click()
        expect(page.locator("#builtin-error")).to_contain_text("模拟配置保存失败")
        expect(bot_rule).to_be_checked()
        page.unroute("**/api/builtin-rules")
        bot_rule.click()
        expect(page.locator(".result-count")).to_contain_text(f"当前生效 {rule_count - 1} 项")
        page.reload()
        expect(bot_rule).not_to_be_checked()
        master.click()
        expect(page.locator(".result-count")).to_contain_text("当前生效 0 项")
        expect(page.locator("#builtin-region > .notice")).to_contain_text("总开关已关闭")
        settings = context.request.get(base_url + "/api/builtin-rules").json()
        assert settings["enabled"] is False
        assert not any(rule["effective"] for rule in settings["rules"])
        test_text.fill("承接洗资业务，联系 @example_agent")
        test_button.click()
        expect(test_result).to_contain_text("本次未执行内置检测")
        master.click()
        expect(page.locator(".result-count")).to_contain_text(f"当前生效 {rule_count - 1} 项")
        expect(test_result).to_contain_text("配置已更新，请重新测试")
        bot_rule.click()
        expect(page.locator(".result-count")).to_contain_text(f"当前生效 {rule_count} 项")
        page.locator(".builtin-conditions summary").first.click()
        expect(page.locator(".builtin-conditions[open] li").first).to_be_visible()
        assert page.locator(".builtin-conditions[open] li").count() > 0
        passed("categorized builtin catalog, version, combination conditions, switches and failure rollback")

        audit_total = context.request.get(base_url + "/api/audit").json()["total"]
        for text, hit_id in [
            ("承接洗资业务，联系 @example_agent", "ad_money_laundering"),
            ("催情药现货批发，联系 @example_agent", "ad_aphrodisiac_trade"),
            ("社工库个人信息打包出售，联系 @example_agent", "ad_personal_data_trade"),
        ]:
            test_text.fill(text)
            test_button.click()
            expect(test_result).to_contain_text("当前配置会拦截这段文本")
            expect(test_result.locator(".builtin-evidence")).to_contain_text(hit_id)
            expect(test_result).not_to_contain_text("@example_agent")
        for text in ["警方提醒防范洗钱风险。", "医学文章解释催情药广告的风险。", "https://t.me/+group123"]:
            test_text.fill(text)
            test_button.click()
            expect(test_result).to_contain_text("未命中该文本")
        test_text.fill("😀" * 4096)
        expect(page.locator("#builtin-test-count")).to_have_text("4096 / 4096 个字符")
        assert test_text.evaluate("node => node.checkValidity()")
        test_button.click()
        expect(test_result).to_contain_text("未命中该文本")
        test_text.fill("😀" * 4097)
        assert test_text.evaluate("node => node.validity.customError")
        test_text.fill("承接洗资业务，联系 @example_agent")
        money_rule = page.locator("#builtin-ad_money_laundering")
        money_rule.click()
        expect(money_rule).not_to_be_checked()
        test_button.click()
        expect(test_result).to_contain_text("未命中该文本")
        money_rule.click()
        expect(money_rule).to_be_checked()
        expect(test_result).to_contain_text("请重新测试")
        test_button.click()
        expect(test_result).to_contain_text("当前配置会拦截这段文本")
        page.route("**/api/builtin-rules/test", lambda route: route.fulfill(status=503, content_type="application/json",
            body=json.dumps({"error": "模拟文本测试失败"})))
        test_button.click()
        expect(test_result).to_contain_text("模拟文本测试失败")
        page.unroute("**/api/builtin-rules/test")
        test_button.click()
        expect(test_result).to_contain_text("当前配置会拦截这段文本")
        page.screenshot(path=str(output / "desktop-builtin-test.png"), full_page=True)
        audit_accessibility("builtin-text-result")
        assert context.request.get(base_url + "/api/audit").json()["total"] == audit_total
        passed("builtin text tests, priority categories, benign contexts, Unicode limits and current settings without audit writes")

        pending_tests = []
        page.route("**/api/builtin-rules/test", lambda route: pending_tests.append(route))
        test_button.click()
        expect(test_result).to_contain_text("正在分析文本")
        page.wait_for_timeout(50)
        test_text.fill("新的正常讨论文本")
        assert pending_tests
        pending_tests[0].fulfill(status=200, content_type="application/json", body=json.dumps({
            "enabled": True, "matched": True, "library_version": catalog["library_version"],
            "hits": audit_record["builtin_details"]["hits"],
        }))
        expect(test_button).to_be_enabled()
        expect(test_result).to_contain_text("文本已修改，请重新测试")
        expect(test_result.locator(".builtin-evidence")).to_have_count(0)
        page.unroute("**/api/builtin-rules/test")
        passed("stale builtin test results discarded after text changes")
        page.get_by_role("link", name="自定义规则", exact=True).click()
        expect(page.locator(".rules-table")).to_be_visible()

        # Check the authenticated shell in both themes at desktop and narrow/mobile sizes.
        expect(page.locator("#toast-region .toast")).to_have_count(0, timeout=10000)
        page.mouse.move(0, 0)
        for width, height in [(1440, 1000), (768, 1024), (390, 844), (320, 740)]:
            page.set_viewport_size({"width": width, "height": height})
            for route, ready in [("dashboard", ".chart-day"), ("rules", ".rules-table"), ("builtin", ".builtin-rule"), ("audit", ".audit-table"), ("settings", ".settings-section")]:
                visit(route, ready)
                assert page.evaluate("document.documentElement.scrollWidth <= innerWidth"), (width, route, "page overflows")
                if width in (1440, 390):
                    page.screenshot(path=str(output / (("mobile-" if width == 390 else "desktop-") + route + ".png")), full_page=True)
                if width == 390:
                    audit_accessibility("mobile-" + route)
                if route == "dashboard":
                    expect(page.locator(".chart-label").first).to_have_css("font-size", "12px")
                    if width <= 390:
                        assert page.locator(".chart-scroll").evaluate("(el) => el.scrollLeft > 0")
            if width == 390:
                visit("rules?q=%232", ".rules-table")
                page.get_by_role("button", name="编辑规则 #2", exact=True).click()
                expect(dialog).to_be_visible()
                assert dialog.bounding_box()["width"] <= 390
                page.screenshot(path=str(output / "mobile-rule-editor.png"))
                audit_accessibility("mobile-editor")
                page.keyboard.press("Escape")
                page.get_by_role("button", name="切换深色主题", exact=True).click()
                visit("dashboard", ".chart-day")
                page.screenshot(path=str(output / "mobile-dashboard-dark.png"), full_page=True)
                audit_accessibility("dashboard-dark")
                page.get_by_role("button", name="切换浅色主题", exact=True).click()
        passed("desktop/tablet/mobile layouts, light/dark themes, accessible dialogs")

        page.set_viewport_size({"width": 1440, "height": 1000})
        visit("settings", ".settings-section")
        page.get_by_label("当前密码", exact=True).fill("preview-only")
        page.get_by_label("新密码", exact=True).fill("different-password")
        page.get_by_label("确认新密码", exact=True).fill("does-not-match")
        page.get_by_role("button", name="保存密码", exact=True).click()
        expect(page.get_by_role("alert")).to_contain_text("两次输入的新密码不一致")
        page.get_by_label("当前密码", exact=True).fill("incorrect-password")
        page.get_by_label("确认新密码", exact=True).fill("different-password")
        page.get_by_role("button", name="保存密码", exact=True).click()
        expect(page.get_by_role("alert")).to_contain_text("当前密码不正确")
        expect(page.locator("#sidebar")).to_be_visible()
        expect(page.get_by_role("heading", name="设置", exact=True)).to_be_visible()
        page.get_by_label("用户名", exact=True).fill("previewadmin")
        page.get_by_role("button", name="保存用户名", exact=True).click()
        expect(page.get_by_role("heading", name="登录管理面板", exact=True)).to_be_visible()
        login("previewadmin")
        visit("settings", ".settings-section")
        page.get_by_label("用户名", exact=True).fill("admin")
        page.get_by_role("button", name="保存用户名", exact=True).click()
        expect(page.get_by_role("heading", name="登录管理面板", exact=True)).to_be_visible()
        login()
        passed("account forms, password confirmation and credential-session invalidation")

        visit("settings", ".settings-section")
        bio = page.get_by_role("switch", name="简介辅助检测", exact=True)
        cross = page.get_by_role("switch", name="跨群管理权限", exact=True)
        owners = page.get_by_label("机器人所有者用户 ID", exact=True)
        # Start from a known state so a reused preview fixture cannot make this
        # scenario flaky; the suite restores the same state when it finishes.
        for control in (bio, cross):
            if control.is_checked():
                control.click()
                expect(control).not_to_be_checked()

        bio.click()
        expect(bio).to_be_checked()
        expect(page.locator("#toast-region")).to_contain_text("简介辅助检测已开启")
        # Runtime settings persist, so a reload must show the stored value.
        page.reload()
        visit("settings", ".settings-section")
        expect(page.get_by_role("switch", name="简介辅助检测", exact=True)).to_be_checked()

        cross.click()
        expect(cross).to_be_checked()
        expect(page.locator(".settings-controls")).to_contain_text("跨群管理已开启")

        owners.fill("not-a-number")
        page.get_by_role("button", name="保存所有者", exact=True).click()
        expect(page.get_by_role("alert")).to_contain_text("正整数")
        owners.fill("123456789, 987654321")
        page.get_by_role("button", name="保存所有者", exact=True).click()
        expect(owners).to_have_value("123456789, 987654321")
        page.reload()
        visit("settings", ".settings-section")
        expect(page.get_by_label("机器人所有者用户 ID", exact=True)).to_have_value("123456789, 987654321")

        # Restore the defaults so later scenarios start from a clean fixture.
        page.get_by_role("switch", name="简介辅助检测", exact=True).click()
        page.get_by_role("switch", name="跨群管理权限", exact=True).click()
        page.get_by_label("机器人所有者用户 ID", exact=True).fill("")
        page.get_by_role("button", name="保存所有者", exact=True).click()
        expect(page.get_by_role("switch", name="简介辅助检测", exact=True)).not_to_be_checked()
        expect(page.get_by_role("switch", name="跨群管理权限", exact=True)).not_to_be_checked()
        passed("runtime settings toggles, owner list validation and persistence")

        page.route("**/api/audit?*", lambda route: route.fulfill(status=401, content_type="application/json",
            body=json.dumps({"error": "未登录或会话已过期。", "code": "unauthorized"})))
        page.get_by_role("link", name="审计日志", exact=True).click()
        expect(page.get_by_role("heading", name="登录管理面板", exact=True)).to_be_visible()
        expect(page.locator("#sidebar")).to_be_hidden()
        page.unroute("**/api/audit?*")
        login()
        page.get_by_role("button", name="退出登录", exact=True).click()
        expect(page.get_by_role("heading", name="登录管理面板", exact=True)).to_be_visible()
        page.goto(base_url + "/#/rules")
        expect(page.get_by_role("heading", name="登录管理面板", exact=True)).to_be_visible()
        expect(page.locator("#sidebar")).to_be_hidden()
        passed("expired-session recovery and logged-out route protection")
        assert not errors, errors
        browser.close()

    report = {"checks": checks, "page_errors": errors, "accessibility": accessibility}
    (output / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    print("Completed", len(checks), "browser scenarios;", len(accessibility), "accessibility checks.", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", default="http://127.0.0.1:8765")
    parser.add_argument("--output", type=Path, default=Path(".gocache/webui-screenshots"))
    parser.add_argument("--axe", type=Path)
    args = parser.parse_args()
    run(args.base_url.rstrip("/"), args.output, args.axe)
