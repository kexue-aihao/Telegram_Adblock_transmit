"use strict";

// All API values are inserted as text, including patterns and Telegram messages.
function el(tag, props, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props || {})) {
    if (key === "class") node.className = value;
    else if (key === "value") continue;
    else if (["checked", "disabled", "hidden", "required"].includes(key)) node[key] = value;
    else if (key === "dataset") Object.assign(node.dataset, value);
    else if (key.startsWith("on") && typeof value === "function") node.addEventListener(key.slice(2), value);
    else if (value !== null && value !== undefined) node.setAttribute(key, value);
  }
  for (const child of children.flat(Infinity)) {
    if (child !== null && child !== undefined) node.append(child instanceof Node ? child : String(child));
  }
  if (props && props.value !== undefined) node.value = props.value;
  return node;
}
function empty(node) { node.replaceChildren(); return node; }
function icon(name) {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("class", "icon");
  svg.setAttribute("aria-hidden", "true");
  const use = document.createElementNS(svg.namespaceURI, "use");
  use.setAttribute("href", "/assets/icons.svg#" + name);
  svg.append(use);
  return svg;
}
function button(label, name, handler, props = {}) {
  return el("button", { type: "button", class: "btn", onclick: (event) => {
    // WebKit does not focus pointer-clicked buttons by default. Keep a reliable
    // invoker for dialog focus restoration across all supported browsers.
    event.currentTarget.focus({ preventScroll: true });
    handler?.(event);
  }, ...props },
    name ? icon(name) : null, el("span", null, label));
}
function iconButton(label, name, handler, props = {}) {
  const control = button(label, null, handler, { class: "icon-btn", "aria-label": label, title: label, ...props });
  control.replaceChildren(icon(name));
  return control;
}
function field(label, control, hint) {
  return el("div", { class: "field" }, el("label", { for: control.id }, label), control,
    hint ? el("p", { class: "hint" }, hint) : null);
}
function notice(text, kind = "err") {
  return el("div", { class: "notice " + kind, role: kind === "err" ? "alert" : "status" }, icon("circle-alert"), el("span", null, text));
}
function toast(message, kind = "ok") {
  const node = el("div", { class: "toast " + kind }, icon(kind === "ok" ? "check" : "circle-alert"), el("span", null, message));
  (activeModal?.dialog.open ? activeModal.feedback : document.getElementById("toast-region")).append(node);
  panelMotion.reveal(node, 180);
  setTimeout(() => panelMotion.play(node, [{ opacity: 1 }, { opacity: 0, transform: "translateY(6px)" }], 180, {}, () => node.remove()), kind === "ok" ? 4500 : 9000);
}
function feedback(data, message) {
  const warning = data?.warning === "cache_refresh_failed"
    ? "规则已保存，但机器人规则缓存未刷新，可能尚未生效。请检查服务日志后重试保存。"
    : data?.warning;
  toast(warning || message, warning ? "warn" : "ok");
}
function loading(text = "正在加载…", shape = "rows") {
  return el("div", { class: "loading skeleton-" + shape, role: "status" },
    el("div", { class: "skeleton-shapes", "aria-hidden": "true" }, Array.from({ length: shape === "stats" ? 4 : 3 }, () => el("span", { class: "skeleton-block" }))),
    el("span", { class: "loading-caption" }, text));
}
async function busy(btn, label, work) {
  if (btn.disabled) return;
  const children = [...btn.childNodes];
  const previousWidth = btn.style.width;
  const previousLabel = btn.getAttribute("aria-label");
  const width = btn.offsetWidth;
  btn.style.width = width + "px";
  btn.disabled = true;
  btn.setAttribute("aria-busy", "true");
  btn.setAttribute("aria-label", label);
  btn.replaceChildren(el("span", { class: "spinner", "aria-hidden": "true" }),
    btn.classList.contains("icon-btn") ? "" : el("span", { class: "busy-label" }, label));
  try { return await work(); }
  finally {
    btn.disabled = false;
    btn.removeAttribute("aria-busy");
    btn.replaceChildren(...children);
    btn.style.width = previousWidth;
    if (previousLabel === null) btn.removeAttribute("aria-label");
    else btn.setAttribute("aria-label", previousLabel);
  }
}
const numberFormat = new Intl.NumberFormat("zh-CN");
const axisNumberFormat = new Intl.NumberFormat("zh-CN", { notation: "compact", maximumFractionDigits: 1 });
const dateFormat = new Intl.DateTimeFormat("zh-CN", {
  timeZone: "UTC", year: "numeric", month: "2-digit", day: "2-digit",
  hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23",
});
function num(value) { return numberFormat.format(value || 0); }
function date(value) {
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? "未知时间" : dateFormat.format(parsed);
}
function utcDay(offset = 0) {
  const now = new Date();
  now.setUTCDate(now.getUTCDate() + offset);
  return now.toISOString().slice(0, 10);
}
function positiveInt(raw, fallback = 1) {
  return /^[1-9]\d*$/.test(raw || "") && Number.isSafeInteger(Number(raw)) ? Number(raw) : fallback;
}
function hashURL(route, values = {}) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) {
    if (value !== "" && value !== null && value !== undefined) params.set(key, String(value));
  }
  return "#/" + route + (params.size ? "?" + params.toString() : "");
}
function routeParams() { return new URLSearchParams((location.hash.split("?")[1]) || ""); }
function syncParams(values) {
  const hash = hashURL(state.route, values);
  history.replaceState(null, "", hash);
  state.hash = hash;
  rememberRoute();
}
function rememberRoute() {
  const link = document.querySelector('.nav-item[data-route="' + state.route + '"]');
  if (link) link.href = location.hash;
}

const state = { authenticated: false, username: "", route: "dashboard", hash: "", chats: [], builtinCatalog: new Map(), builtinCatalogLoaded: false };
let activeModal = null;
let pageRequests;

async function api(path, options = {}) {
  const pageSignal = (!options.method || options.method === "GET") ? pageRequests?.signal : undefined;
  const signal = options.signal && pageSignal && typeof AbortSignal.any === "function"
    ? AbortSignal.any([options.signal, pageSignal]) : options.signal || pageSignal;
  const init = { method: options.method || "GET", credentials: "same-origin",
    headers: { "X-Requested-With": "fetch" }, signal };
  if (options.body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(options.body);
  }
  let response;
  try { response = await fetch(path, init); }
  catch (err) {
    if (err.name === "AbortError") throw err;
    throw new Error("网络连接失败，请检查连接后重试。");
  }
  const data = response.status === 204 ? null : await response.json().catch(() => null);
  if (!response.ok) {
    const err = new Error(data && data.error || "请求失败（" + response.status + "），请稍后重试。");
    err.status = response.status;
    err.code = data && data.code;
    if (response.status === 401 && (!err.code || err.code === "unauthorized") && path !== "/api/login" && state.authenticated) {
      state.authenticated = false;
      activeModal?.close(true);
      renderShell();
      toast("登录已过期，请重新登录。", "warn");
    }
    throw err;
  }
  return data;
}
function renderError(region, err, retry) {
  if (!region.isConnected || err.name === "AbortError") return;
  region.querySelectorAll(":scope > .request-error").forEach((node) => node.remove());
  if (region.querySelector(".loading")) empty(region);
  region.append(el("div", { class: "request-error" }, notice(err.message), retry ? button("重试", "refresh-cw", retry) : null));
}
function pageHeader(title, meta, actions = []) {
  return el("header", { class: "page-header" },
    el("div", null, el("h1", { tabindex: "-1" }, title), meta ? el("p", { class: "page-meta" }, meta) : null),
    el("div", { class: "actions" }, actions));
}
function table(headers, className = "") {
  const body = el("tbody");
  const element = el("table", { class: "data " + className },
    el("thead", null, el("tr", null, headers.map((h) => el("th", { scope: "col" }, h)))), body);
  return { body, wrap: el("div", { class: "table-wrap" }, element) };
}
function cell(label, content, className = "") { return el("td", { class: className, "data-label": label }, content); }
function emptyState(title, action) {
  return el("div", { class: "empty-state" }, icon("search"), el("p", null, title), action);
}
function pagination(page, totalPages, total, onPage) {
  return el("nav", { class: "pagination", "aria-label": "分页" },
    el("span", { class: "pagination-info", role: "status" }, "共 " + num(total) + " 条，第 " + page + " / " + Math.max(1, totalPages) + " 页"),
    iconButton("上一页", "chevron-left", () => onPage(page - 1), { disabled: page <= 1 }),
    iconButton("下一页", "chevron-right", () => onPage(page + 1), { disabled: page >= totalPages }));
}
function switchTheme() {
  const theme = document.documentElement.dataset.theme === "dark" ? "light" : "dark";
  document.documentElement.dataset.theme = theme;
  document.querySelector('meta[name="theme-color"]').content = theme === "dark" ? "#111619" : "#f3f6f5";
  try { localStorage.setItem("panel-theme", theme); } catch { /* Keep the current session theme. */ }
  themeLabel();
}
function themeLabel() {
  const label = document.documentElement.dataset.theme === "dark" ? "切换浅色主题" : "切换深色主题";
  const control = document.getElementById("theme-toggle");
  control.setAttribute("aria-label", label);
  control.title = label;
}
function moveNavIndicator(animate = true) {
  const nav = document.querySelector(".nav");
  const selected = nav.querySelector('[aria-current="page"]');
  const marker = nav.querySelector(".nav-indicator");
  if (!selected || document.getElementById("sidebar").hidden) return;
  // Read all geometry before writing. Only transform changes during the animation.
  const old = marker.getBoundingClientRect();
  const target = selected.getBoundingClientRect();
  const parent = nav.getBoundingClientRect();
  const ready = marker.dataset.ready === "true";
  panelMotion.clear(marker);
  Object.assign(marker.style, { width: target.width + "px", height: target.height + "px", transform: `translate(${target.left - parent.left}px, ${target.top - parent.top}px)` });
  marker.dataset.ready = "true";
  if (animate && ready) panelMotion.play(marker, [
    { transform: `translate(${old.left - parent.left}px, ${old.top - parent.top}px)` },
    { transform: marker.style.transform },
  ], 220);
}
function navigate() {
  if (activeModal && !activeModal.close(false, true)) {
    history.replaceState(null, "", state.hash || "#/dashboard");
    return;
  }
  if (!state.authenticated) return;
  const previousRoute = state.route;
  state.route = (location.hash || "#/dashboard").replace(/^#\/?/, "").split("?")[0];
  state.hash = location.hash;
  document.querySelectorAll(".nav-item[data-route]").forEach((item) => {
    if (item.dataset.route === (state.route === "builtin" ? "rules" : state.route)) item.setAttribute("aria-current", "page");
    else item.removeAttribute("aria-current");
  });
  rememberRoute();
  moveNavIndicator();
  if (state.route !== previousRoute) window.scrollTo({ top: 0, behavior: "instant" });
  renderView(true);
}
function renderShell() {
  document.getElementById("sidebar").hidden = !state.authenticated;
  document.getElementById("logout-btn").hidden = !state.authenticated;
  document.getElementById("account-name").textContent = state.authenticated ? state.username : "";
  document.body.classList.toggle("signed-in", state.authenticated);
  if (state.authenticated) navigate();
  else {
    pageRequests?.abort();
    const root = document.getElementById("view");
    panelMotion.clear(root);
    empty(root).append(renderLogin());
    panelMotion.reveal(root.firstElementChild, 240);
  }
}
function renderView(transition = false) {
  const root = document.getElementById("view");
  const previous = root.querySelector(".page:not(.page-exit)");
  let snapshot;
  if (transition && previous && !panelMotion.reduced()) {
    snapshot = previous.cloneNode(true);
    snapshot.classList.add("page-exit");
    snapshot.inert = true;
    snapshot.setAttribute("aria-hidden", "true");
    snapshot.removeAttribute("id");
    snapshot.querySelectorAll("[id]").forEach((node) => node.removeAttribute("id"));
  }
  // Detach actual controls immediately: their existing isConnected guards remain valid.
  pageRequests?.abort();
  pageRequests = new AbortController();
  panelMotion.clear(root);
  const view = el("section", { class: "page", tabindex: "-1" }, loading());
  empty(root).append(view);
  if (snapshot) {
    root.append(snapshot);
    panelMotion.play(snapshot, [{ opacity: .65 }, { opacity: 0 }], 70, {}, () => snapshot.remove());
  }
  const renderers = { dashboard: renderDashboard, rules: renderRules, builtin: renderBuiltin, audit: renderAudit, settings: renderSettings };
  const titles = { dashboard: "仪表盘", rules: "规则管理", builtin: "内置广告库", audit: "审计日志", settings: "设置" };
  document.title = (titles[state.route] || "页面不存在") + " · 广告拦截管理面板";
  if (renderers[state.route]) {
    renderers[state.route](view).then(() => {
      if (view.isConnected && document.activeElement === view) view.querySelector("h1")?.focus({ preventScroll: true });
    });
  }
  else empty(view).append(emptyState("页面不存在", el("a", { href: "#/dashboard", class: "btn" }, "返回仪表盘")));
  if (transition) {
    panelMotion.reveal(view, 240);
    (view.querySelector("h1") || view).focus({ preventScroll: true });
  }
}
function renderLogin() {
  const username = el("input", { id: "login-user", name: "username", autocomplete: "username", required: true, spellcheck: "false" });
  const password = el("input", { id: "login-pass", name: "password", type: "password", autocomplete: "current-password", required: true });
  const errors = el("div", { id: "login-error" });
  const submit = button("登录", null, null, { type: "submit", class: "btn primary block" });
  const form = el("form", { class: "login-form" },
    el("div", { class: "login-mark" }, icon("shield-check")),
    el("p", { class: "eyebrow" }, "TELEGRAM MODERATION"), el("h1", null, "登录管理面板"),
    el("p", { class: "login-description" }, "让社区交流，回归内容本身。"),
    field("用户名", username), field("密码", password), errors, submit);
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    busy(submit, "登录中…", async () => {
      empty(errors);
      try {
        await api("/api/login", { method: "POST", body: { username: username.value.trim(), password: password.value } });
        state.authenticated = true;
        state.username = username.value.trim();
        renderShell();
      } catch (err) { errors.append(notice(err.message)); password.focus(); }
    });
  });
  return el("div", { class: "login-wrap" }, form);
}
async function doLogout() {
  const btn = document.getElementById("logout-btn");
  await busy(btn, "退出中…", async () => {
    try {
      await api("/api/logout", { method: "POST" });
      state.authenticated = false;
      state.chats = [];
      state.builtinCatalog.clear();
      state.builtinCatalogLoaded = false;
      activeModal?.close(true);
      renderShell();
    } catch (err) { toast(err.message, "err"); }
  });
}

function openModal(title, subtitle = "") {
  if (activeModal && !activeModal.close(false, true)) return null;
  const previousFocus = document.activeElement;
  const content = el("div", { class: "modal-content" });
  const actions = el("div", { class: "modal-actions" });
  const feedbackRegion = el("div", { class: "modal-feedback", role: "status", "aria-live": "polite" });
  const dialog = el("dialog", { class: "modal", "aria-labelledby": "modal-title", tabindex: "-1" });
  const modal = {
    content, actions, dialog, feedback: feedbackRegion, dirty: () => false, saving: false, closing: false,
    close(force = false, immediate = false) {
      if (modal.closing && !force && !immediate) return true;
      if (!modal.closing && !force && (modal.saving || (modal.dirty() && !window.confirm("有未保存的修改，确定放弃吗？")))) return false;
      modal.closing = true;
      dialog.inert = true;
      const finish = () => {
        if (!dialog.isConnected) return;
        document.getElementById("toast-region").append(...feedbackRegion.childNodes);
        dialog.close();
        dialog.remove();
        if (activeModal === modal) activeModal = null;
        if (previousFocus && previousFocus.isConnected) previousFocus.focus({ preventScroll: true });
        else document.querySelector(".page:not(.page-exit) h1")?.focus({ preventScroll: true });
      };
      panelMotion.clear(dialog);
      if (force || immediate) finish();
      else panelMotion.play(dialog, [{ opacity: 1, transform: "scale(1)" }, { opacity: 0, transform: "translateY(8px) scale(.98)" }], 160, {}, finish);
      return true;
    },
  };
  dialog.append(el("header", { class: "modal-header" },
    el("div", null, el("h2", { id: "modal-title" }, title), subtitle ? el("p", { class: "hint" }, subtitle) : null),
    iconButton("关闭弹窗", "x", () => modal.close())), content, feedbackRegion, actions);
  dialog.addEventListener("cancel", (event) => { event.preventDefault(); modal.close(); });
  dialog.addEventListener("keydown", (event) => {
    if (event.key !== "Tab") return;
    const controls = [...dialog.querySelectorAll("button, input, textarea, select, a[href], [tabindex]")]
      .filter((node) => !node.disabled && node.tabIndex >= 0 && node.getClientRects().length);
    const first = controls[0];
    const last = controls[controls.length - 1];
    if (!first || document.activeElement === dialog ||
        (event.shiftKey ? document.activeElement === first : document.activeElement === last)) {
      event.preventDefault();
      (event.shiftKey ? last : first)?.focus();
    }
  });
  dialog.addEventListener("click", (event) => {
    if (event.target !== dialog) return;
    const bounds = dialog.getBoundingClientRect();
    if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) modal.close();
  });
  document.getElementById("modal-root").append(dialog);
  activeModal = modal;
  queueMicrotask(() => {
    if (!dialog.isConnected) return;
    dialog.showModal();
    panelMotion.play(dialog, [{ opacity: 0, transform: "translateY(12px) scale(.97)" }, { opacity: 1, transform: "translateY(0) scale(1)" }], 240, { easing: "cubic-bezier(0.16, 1.12, 0.3, 1)" });
    const first = matchMedia("(min-width: 720px)").matches ? content.querySelector("input, textarea, button") : null;
    (first || dialog).focus();
  });
  return modal;
}
window.addEventListener("beforeunload", (event) => {
  if (activeModal && (activeModal.dirty() || activeModal.saving)) { event.preventDefault(); event.returnValue = ""; }
});

async function renderDashboard(view) {
  const query = routeParams();
  let days = [7, 30, 90].includes(Number(query.get("days"))) ? Number(query.get("days")) : 30;
  const refresh = iconButton("刷新仪表盘", "refresh-cw", () => renderView());
  const statsRegion = el("div", null, loading("正在加载统计…", "stats"));
  const trendRegion = el("div", { class: "trend-region" }, loading("正在加载趋势…", "chart"));
  const failures = el("section", { class: "section recent-failures" }, loading("正在加载失败记录…"));
  const choices = el("div", { class: "segmented", "aria-label": "趋势时间范围" },
    [7, 30, 90].map((value) => button(value + " 天", null, () => {
      days = value;
      syncParams({ days });
      choices.querySelectorAll("button").forEach((btn) => btn.setAttribute("aria-pressed", String(Number(btn.dataset.days) === days)));
      loadTrend();
    }, { "aria-pressed": String(days === value), dataset: { days: value } })));
  empty(view).append(pageHeader("仪表盘", "社区防护概览 · 按 UTC 日期统计", [refresh]), statsRegion,
    el("div", { class: "dashboard-grid" }, el("section", { class: "section trend-panel" }, el("div", { class: "section-heading" }, el("h2", null, "广告命中趋势"), choices), trendRegion), failures));

  async function loadOverview() {
    try {
      const overview = await api("/api/dashboard/overview");
      if (!view.isConnected) return;
      const failed = overview.failed_today;
      const todayFilter = { from: utcDay(), to: utcDay() };
      const metrics = [
        ["今日命中", overview.hits_today, "", hashURL("audit", todayFilter)],
        ["删除成功", overview.deleted_today, "ok", hashURL("audit", { ...todayFilter, success: "true" })],
        ["删除失败", failed, failed ? "err" : "", hashURL("audit", { ...todayFilter, success: "false" })],
        ["近 7 天命中", overview.hits_7day, "", hashURL("audit", { from: utcDay(-6), to: utcDay() })],
      ];
      const processed = overview.deleted_today + failed;
      empty(statsRegion).append(el("div", { class: "stats-grid" }, metrics.map(([label, count, tone, href]) =>
        el("a", { class: "stat " + tone, href }, el("span", { class: "stat-label" }, label, icon("arrow-up-right")), el("strong", { class: "stat-value" }, num(count))))),
      el("div", { class: "inventory" },
        el("span", null, "群组 ", el("strong", null, num(overview.total_chats))),
        el("a", { href: "#/rules" }, "全局规则 ", el("strong", null, num(overview.total_rules))),
        el("a", { href: "#/rules?status=enabled" }, "已启用 ", el("strong", null, num(overview.enabled_rules))),
        el("span", null, "累计命中 ", el("strong", null, num(overview.total_hits))),
        el("span", null, "今日删除成功率 ", el("strong", null, processed ? num(Math.round(overview.deleted_today / processed * 1000) / 10) + "%" : "暂无数据"))));
      if (failed) statsRegion.append(el("a", { class: "failure-banner", href: hashURL("audit", { ...todayFilter, success: "false" }) },
        icon("circle-alert"), el("span", null, "今日有 " + num(failed) + " 条消息删除失败"), el("span", { class: "banner-action" }, "查看原因"), icon("chevron-right")));
      statsRegion.querySelectorAll(".stat").forEach((card, i) => panelMotion.reveal(card, 220, i * 30));
    } catch (err) { renderError(statsRegion, err, loadOverview); }
  }
  let trendRequest = 0;
  let trendAbort;
  async function loadTrend() {
    trendAbort?.abort();
    trendAbort = new AbortController();
    const request = ++trendRequest;
    trendRegion.setAttribute("aria-busy", "true");
    try {
      const data = await api("/api/dashboard/trend?days=" + days, { signal: trendAbort.signal });
      if (!view.isConnected || request !== trendRequest) return;
      empty(trendRegion).append(renderTrendChart(data || []));
      panelMotion.reveal(trendRegion);
    } catch (err) { if (request === trendRequest) renderError(trendRegion, err, loadTrend); }
    finally { if (request === trendRequest) trendRegion.removeAttribute("aria-busy"); }
  }
  async function loadFailures() {
    try {
      const data = await api("/api/audit?success=false&page_size=5");
      if (!view.isConnected) return;
      empty(failures).append(el("div", { class: "section-heading" }, el("h2", null, "最近删除失败"),
        el("a", { href: "#/audit?success=false", class: "text-link" }, "查看全部", icon("chevron-right"))));
      if (!data.items.length) { failures.append(el("p", { class: "empty-inline" }, icon("check-check"), "暂无删除失败记录")); return; }
      const list = el("ul", { class: "failure-list" });
      data.items.forEach((entry) => list.append(el("li", null,
        el("div", null, el("span", { class: "small muted" }, date(entry.occurred_at) + " UTC · 群组 " + entry.chat_id),
          el("p", { class: "failure-message" }, entry.deletion_error || "未返回错误详情")),
        button("查看记录", "arrow-up-right", () => openAuditDetail(entry.id), { class: "btn subtle" }))));
      failures.append(list);
    } catch (err) { renderError(failures, err, loadFailures); }
  }
  await Promise.all([loadOverview(), loadTrend(), loadFailures()]);
}

function renderTrendChart(data) {
  if (!data.length) return emptyState("该时段暂无统计数据");
  const sums = data.reduce((acc, day) => ({ hits: acc.hits + day.hits, deleted: acc.deleted + day.deleted, failed: acc.failed + day.failed }),
    { hits: 0, deleted: 0, failed: 0 });
  const container = el("div", { class: "trend" });
  const chartView = el("div", { class: "chart-scroll", tabindex: "0", "aria-label": "趋势图" });
  const dataView = table(["日期 (UTC)", "命中", "删除成功", "删除失败"], "trend-table" + (data.length > 50 ? " large-list" : ""));
  dataView.wrap.hidden = true;
  data.forEach((day) => dataView.body.append(el("tr", null,
    cell("日期 (UTC)", day.date), cell("命中", num(day.hits)), cell("删除成功", num(day.deleted)),
    cell("删除失败", num(day.failed), day.failed ? "err-text" : ""))));
  const modes = el("div", { class: "segmented", "aria-label": "趋势展示方式" });
  ["图表", "数据"].forEach((label, index) => modes.append(button(label, null, () => {
    chartView.hidden = index !== 0;
    dataView.wrap.hidden = index !== 1;
    modes.querySelectorAll("button").forEach((btn, i) => btn.setAttribute("aria-pressed", String(i === index)));
  }, { "aria-pressed": String(index === 0) })));
  container.append(el("div", { class: "trend-summary" },
    el("div", { class: "legend" },
      el("span", null, "命中 ", el("strong", null, num(sums.hits))),
      el("span", { class: "legend-item ok" }, "删除成功 ", el("strong", null, num(sums.deleted))),
      el("span", { class: "legend-item err" }, "删除失败 ", el("strong", null, num(sums.failed)))), modes));
  if (!sums.hits) container.append(el("p", { class: "empty-inline" }, "该时段暂无广告命中"));
  const width = 880, height = 270, left = 80, top = 20, bottom = 38;
  const innerHeight = height - top - bottom, step = (width - left - 20) / data.length;
  const max = Math.max(1, ...data.map((day) => Math.max(day.hits, day.deleted + day.failed)));
  const unit = Math.pow(10, Math.floor(Math.log10(max / 4)));
  const tick = Math.max(1, Math.ceil(max / (4 * unit)) * unit);
  const ceiling = tick * 4;
  function svgEl(tag, props, text) {
    const node = document.createElementNS("http://www.w3.org/2000/svg", tag);
    Object.entries(props).forEach(([key, value]) => node.setAttribute(key, value));
    if (text !== undefined) node.textContent = text;
    return node;
  }
  // Percentage x-coordinates preserve label sizes when the chart viewport changes.
  const xPercent = (value) => value / width * 100 + "%";
  const svg = svgEl("svg", { width: "100%", height, role: "group", "aria-label": "每日广告命中与删除结果" });
  if (data.length > 50) svg.classList.add("long-trend");
  for (let i = 0; i <= 4; i++) {
    const y = top + innerHeight * (1 - i / 4);
    svg.append(svgEl("line", { x1: xPercent(left), x2: xPercent(width - 20), y1: y, y2: y, class: "chart-grid" }),
      svgEl("text", { x: xPercent(left - 12), y: y + 4, "text-anchor": "end", class: "chart-label" }, axisNumberFormat.format(tick * i)));
  }
  const detail = el("div", { class: "chart-detail", role: "status" });
  const groups = [];
  function select(index, focus = false) {
    const day = data[index];
    groups.forEach((group, i) => { group.setAttribute("tabindex", i === index ? "0" : "-1"); group.classList.toggle("selected", i === index); });
    empty(detail).append(el("strong", null, day.date + " UTC"), el("span", null, "命中 " + num(day.hits)),
      el("span", null, "成功 " + num(day.deleted)), el("span", { class: day.failed ? "err-text" : "" }, "失败 " + num(day.failed)),
      el("a", { href: hashURL("audit", { from: day.date, to: day.date }), class: "text-link" }, "查看当日日志", icon("arrow-up-right")));
    if (focus) groups[index].focus();
  }
  data.forEach((day, i) => {
    const x = left + step * i, barWidth = Math.max(2, step * 0.6);
    const group = svgEl("g", { role: "button", tabindex: "-1", class: "chart-day",
      "aria-label": day.date + "，命中 " + day.hits + "，删除成功 " + day.deleted + "，删除失败 " + day.failed });
    group.append(svgEl("rect", { x: xPercent(x), y: top, width: xPercent(step), height: innerHeight, class: "chart-hit" }));
    let y = top + innerHeight;
    [[day.deleted, "chart-success"], [day.failed, "chart-failed"]].forEach(([value, className]) => {
      const h = value / ceiling * innerHeight;
      y -= h;
      group.append(svgEl("rect", { x: xPercent(x + (step - barWidth) / 2), y, width: xPercent(barWidth), height: h, rx: "1", class: className }));
    });
    group.addEventListener("click", () => select(i));
    group.addEventListener("focus", () => select(i));
    group.addEventListener("keydown", (event) => {
      if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) {
        event.preventDefault();
        const next = event.key === "Home" ? 0 : event.key === "End" ? data.length - 1 : Math.max(0, Math.min(data.length - 1, i + (event.key === "ArrowLeft" ? -1 : 1)));
        select(next, true);
      } else if (event.key === "Enter" || event.key === " ") { event.preventDefault(); select(i); }
    });
    groups.push(group);
    svg.append(group);
    if (i === 0 || i === data.length - 1 || (i % Math.max(1, Math.ceil(data.length / 7)) === 0 && i < data.length - 2)) {
      svg.append(svgEl("text", { x: xPercent(x + step / 2), y: height - 12, "text-anchor": "middle", class: "chart-label" }, day.date.slice(5)));
    }
  });
  chartView.append(svg);
  select(data.length - 1);
  container.append(chartView, detail, dataView.wrap);
  queueMicrotask(() => {
    if (chartView.isConnected) chartView.scrollLeft = chartView.scrollWidth;
  });
  return container;
}

function ruleTabs() {
  return el("nav", { class: "rule-tabs", "aria-label": "规则分类" },
    [["rules", "自定义规则"], ["builtin", "内置广告库"]].map(([route, label]) =>
      el("a", { href: "#/" + route, "aria-current": state.route === route ? "page" : null }, label)));
}

async function renderBuiltin(view) {
  const region = el("div", { id: "builtin-region" }, loading("正在加载内置广告库…"));
  const errors = el("div", { id: "builtin-error" });
  const progress = el("div", { class: "result-count", role: "status" });
  const version = el("p", { class: "hint", id: "builtin-version" });
  const testText = el("textarea", { id: "builtin-test-text", name: "text", rows: "4", required: true, spellcheck: "false",
    placeholder: "粘贴一条待检测消息…", "aria-describedby": "builtin-test-hint builtin-test-count" });
  const testCount = el("p", { id: "builtin-test-count", class: "hint" }, "0 / 4096 个字符");
  const testResult = el("div", { id: "builtin-test-result", role: "status", "aria-live": "polite" });
  const testConfig = el("p", { class: "hint", id: "builtin-test-config" });
  const testButton = button("测试文本", "search", null, { type: "submit", class: "btn primary" });
  const testForm = el("form", { class: "builtin-test-form" },
    field("待检测文本", testText), testCount,
    el("p", { id: "builtin-test-hint", class: "hint" }, "仅测试文本，最多 4096 个字符。转发来源、隐藏链接和消息按钮等 Telegram 元数据未包含在测试中。"),
    testConfig, el("div", { class: "test-actions" }, testButton), testResult);
  const testSection = el("section", { class: "section builtin-test", "aria-labelledby": "builtin-test-title" },
    el("h2", { id: "builtin-test-title" }, "文本测试"),
    el("p", { class: "hint" }, "按当前生效配置分析，测试内容不会保存，也不会产生删除、审计或封禁操作。"), testForm);
  let testRevision = 0;
  let settingsSaving = false;
  function invalidateTest(message) {
    testRevision++;
    if (testResult.childNodes.length) empty(testResult).append(el("p", { class: "hint" }, message));
  }
  testText.addEventListener("input", () => {
    const length = Array.from(testText.value).length;
    testCount.textContent = length + " / 4096 个字符";
    testText.setCustomValidity(length > 4096 ? "测试文本最多为 4096 个字符。" : "");
    testText.removeAttribute("aria-invalid");
    invalidateTest("文本已修改，请重新测试。");
  });
  testForm.addEventListener("submit", (event) => {
    event.preventDefault();
    if (settingsSaving || !testForm.reportValidity()) return;
    if (!testText.value.trim()) {
      testText.setAttribute("aria-invalid", "true");
      empty(testResult).append(notice("请输入待检测文本。"));
      testText.focus();
      return;
    }
    const revision = ++testRevision;
    busy(testButton, "正在测试…", async () => {
      empty(testResult).append(loading("正在分析文本…"));
      try {
        const result = await api("/api/builtin-rules/test", { method: "POST", body: { text: testText.value }, signal: pageRequests.signal });
        if (!view.isConnected || revision !== testRevision) return;
        empty(testResult).append(el("p", { class: "hint" }, "本次使用规则库版本：" + result.library_version));
        if (!result.enabled) testResult.append(notice("内置防护总开关已关闭，本次未执行内置检测。", "warn"));
        else if (!result.matched) testResult.append(el("p", null, "当前已启用检测项未命中该文本。"));
        else testResult.append(el("p", null, "当前配置会拦截这段文本。"), builtinEvidence(result.hits));
        panelMotion.reveal(testResult);
      } catch (err) {
        if (view.isConnected && revision === testRevision && err.name !== "AbortError") empty(testResult).append(notice(err.message));
      }
    }).finally(() => { testButton.disabled = settingsSaving; });
  });
  empty(view).append(pageHeader("内置广告库", "全局防护 · 对所有群组生效",
    [iconButton("刷新内置广告库", "refresh-cw", () => renderView())]), ruleTabs(), errors, version, progress, region);
  function toggle(id, label, checked, change) {
    const input = el("input", { type: "checkbox", role: "switch", id, checked, "aria-label": label, onchange: () => change(input.checked) });
    return el("label", { class: "switch", for: id }, input,
      el("span", { class: "track", "aria-hidden": "true" }),
      el("span", { class: "status-label" }, checked ? "已启用" : "已停用"));
  }
  function paint(data) {
    if (!view.isConnected) return;
    rememberBuiltinCatalog(data);
    version.textContent = "规则库版本：" + data.library_version;
    testConfig.textContent = data.enabled
      ? "本次测试使用已启用的检测项；停用项不参与判定。"
      : "当前总开关已关闭，测试会返回未执行检测。";
    empty(region).append(el("div", { class: "builtin-master" },
      el("div", null, el("h2", null, "内置防护总开关")),
      toggle("builtin-master", "内置防护总开关", data.enabled, (enabled) => save({ enabled }, "builtin-master"))));
    if (!data.enabled) region.append(notice("总开关已关闭，内置检测不生效。自定义规则仍按自身状态执行。", "warn"));
    progress.textContent = "共 " + data.rules.length + " 项检测 · 当前生效 " + data.rules.filter((rule) => rule.effective).length + " 项";
    region.append(testSection);
    const groups = new Map();
    for (const rule of data.rules) {
      const category = rule.category || "通用广告手法";
      if (!groups.has(category)) groups.set(category, []);
      groups.get(category).push(rule);
    }
    for (const [category, rules] of groups) {
      const list = el("div", { class: "builtin-list" });
      const group = el("section", { class: "builtin-group", "aria-label": category }, el("h2", null, category), list);
      for (const rule of rules) {
        const controlId = "builtin-" + rule.id;
        const description = el("div", { class: "builtin-description" },
          el("h3", null, rule.name), el("p", { class: "muted" }, rule.description),
          el("code", { class: "small muted", translate: "no" }, rule.id),
          el("div", { class: "builtin-effective" }, icon(rule.effective ? "check" : "circle-alert"), rule.effective ? "当前生效" : rule.enabled ? "总开关关闭，暂未生效" : "当前未启用"));
        const conditions = el("details", { class: "builtin-conditions" }, el("summary", null, "组合检测条件"),
          el("ul", null, (rule.conditions || []).map((condition) => el("li", null, condition))));
        if (rule.pattern) conditions.append(el("p", { class: "hint" }, "辅助匹配表达式，完整判断以上述组合条件为准。"),
          el("pre", { class: "pattern-text", translate: "no" }, rule.pattern));
        description.append(conditions);
        list.append(el("article", { class: "builtin-rule" }, description,
          toggle(controlId, "启用" + rule.name, rule.enabled, (enabled) => save({ rules: { [rule.id]: enabled } }, controlId))));
      }
      region.append(group);
    }
    async function save(patch, focusId) {
      empty(errors);
      settingsSaving = true;
      testButton.disabled = true;
      invalidateTest("防护配置正在更新，完成后请重新测试。");
      region.querySelectorAll("input").forEach((input) => { input.disabled = true; });
      region.setAttribute("aria-busy", "true");
      progress.textContent = "正在保存…";
      try {
        const result = await api("/api/builtin-rules", { method: "PATCH", body: patch });
        paint(result);
        invalidateTest("防护配置已更新，请重新测试。");
        toast("内置防护配置已保存并生效");
      } catch (err) {
        paint(data);
        invalidateTest("配置未保存，仍使用原配置，请重新测试。");
        if (view.isConnected) errors.append(notice(err.message));
      } finally {
        settingsSaving = false;
        testButton.disabled = false;
        region.removeAttribute("aria-busy");
        if (view.isConnected) document.getElementById(focusId)?.focus();
      }
    }
  }
  async function load() {
    try {
      const data = await api("/api/builtin-rules");
      paint(data);
    } catch (err) { renderError(region, err, load); }
  }
  await load();
}

async function renderRules(view) {
  const query = routeParams();
  const filters = {
    q: query.get("q") || "",
    status: ["enabled", "disabled"].includes(query.get("status")) ? query.get("status") : "",
    sort: query.get("sort") === "id" ? "id" : "updated",
    page: positiveInt(query.get("page")),
  };
  let rules = [];
  const refresh = iconButton("刷新规则", "refresh-cw", () => renderView());
  const add = button("新增规则", "plus", () => openRuleModal(null, reload), { class: "btn primary" });
  const exportBtn = button("导出规则", "download", () => downloadRuleExport(exportBtn));
  const search = el("input", { id: "rule-search", name: "q", type: "search", value: filters.q,
    placeholder: "搜索正则或规则 ID…", autocomplete: "off", spellcheck: "false" });
  const status = el("select", { id: "rule-status", name: "status", value: filters.status },
    el("option", { value: "" }, "全部状态"), el("option", { value: "enabled" }, "已启用"), el("option", { value: "disabled" }, "已停用"));
  const sort = el("select", { id: "rule-sort", name: "sort", value: filters.sort },
    el("option", { value: "updated" }, "最近更新"), el("option", { value: "id" }, "规则 ID"));
  const clear = button("重置", "x", () => {
    filters.q = search.value = ""; filters.status = status.value = ""; filters.sort = sort.value = "updated"; filters.page = 1; paint();
  }, { class: "btn subtle" });
  const count = el("div", { class: "result-count", role: "status" });
  const region = el("div", { id: "rules-region" }, loading("正在加载规则…"));
  empty(view).append(pageHeader("规则管理", "全局规则 · 对所有群组生效", [refresh, exportBtn, add]),
    ruleTabs(),
    el("div", { class: "filter-bar rules-filters" }, field("搜索规则", search), field("状态", status), field("排序", sort), clear), count, region);
  search.addEventListener("input", () => { filters.q = search.value; filters.page = 1; paint(); });
  status.addEventListener("change", () => { filters.status = status.value; filters.page = 1; paint(); });
  sort.addEventListener("change", () => { filters.sort = sort.value; filters.page = 1; paint(); });
  let loadRequest = 0;
  async function reload() {
    const request = ++loadRequest;
    region.setAttribute("aria-busy", "true");
    try {
      const data = await api("/api/rules");
      if (!view.isConnected || request !== loadRequest) return;
      rules = data || [];
      paint();
      panelMotion.reveal(region);
    } catch (err) { if (request === loadRequest) renderError(region, err, reload); }
    finally { if (request === loadRequest) region.removeAttribute("aria-busy"); }
  }
  function paint() {
    if (!view.isConnected) return;
    const focused = document.activeElement;
    const focusLabel = region.contains(focused) ? focused.getAttribute("aria-label") : null;
    if (focusLabel) queueMicrotask(() => {
      if (!focused.isConnected && view.isConnected) {
        (region.querySelector('[aria-label="' + CSS.escape(focusLabel) + '"]') || view.querySelector("h1"))?.focus();
      }
    });
    const needle = filters.q.trim().toLocaleLowerCase();
    const filtered = rules.filter((rule) =>
      (!needle || (needle.startsWith("#") ? String(rule.id) === needle.slice(1) :
        rule.pattern.toLocaleLowerCase().includes(needle) || String(rule.id).includes(needle))) &&
      (!filters.status || rule.enabled === (filters.status === "enabled")));
    filtered.sort((a, b) => filters.sort === "id" ? a.id - b.id : (Date.parse(b.updated_at) - Date.parse(a.updated_at) || b.id - a.id));
    const pages = Math.ceil(filtered.length / 20);
    filters.page = Math.min(filters.page, Math.max(1, pages));
    syncParams(filters);
    count.textContent = "显示 " + num(filtered.length) + " / " + num(rules.length) + " 条规则 · 已启用 " + num(rules.filter((r) => r.enabled).length) + " 条";
    clear.disabled = !filters.q && !filters.status && filters.sort === "updated";
    empty(region);
    if (!filtered.length) {
      region.append(emptyState(rules.length ? "没有符合条件的规则" : "暂无自定义规则",
        rules.length ? button("清除筛选", "x", () => clear.click()) : button("新增规则", "plus", () => add.click(), { class: "btn primary" })));
      return;
    }
    const grid = table(["ID", "正则表达式", "状态", "更新时间 (UTC)", "操作"], "rules-table");
    filtered.slice((filters.page - 1) * 20, filters.page * 20).forEach((rule) => {
      const toggle = el("input", { type: "checkbox", role: "switch", checked: rule.enabled, id: "rule-toggle-" + rule.id,
        "aria-label": "启用规则 #" + rule.id });
      const statusLabel = el("span", { class: "status-label" }, rule.enabled ? "已启用" : "已停用");
      const errorBox = el("div", { class: "row-error", role: "status" });
      const edit = iconButton("编辑规则 #" + rule.id, "pencil", () => openRuleModal(rule, reload));
      const remove = iconButton("删除规则 #" + rule.id, "trash-2", () => confirmDeleteRule(rule, reload), { class: "icon-btn danger-quiet" });
      toggle.addEventListener("change", async () => {
        toggle.disabled = edit.disabled = remove.disabled = true;
        errorBox.textContent = "";
        const enabled = toggle.checked;
        try {
          const result = await api("/api/rules/" + rule.id, { method: "PATCH", body: { enabled } });
          rule.enabled = enabled;
          rule.updated_at = result?.updated_at || new Date().toISOString();
          feedback(result, "规则 #" + rule.id + (enabled ? " 已启用" : " 已停用"));
          paint();
          document.getElementById("rule-toggle-" + rule.id)?.focus();
        } catch (err) { toggle.checked = rule.enabled; errorBox.textContent = err.message; }
        finally { toggle.disabled = edit.disabled = remove.disabled = false; }
      });
      grid.body.append(el("tr", null,
        cell("ID", "#" + rule.id, "id-cell"),
        cell("正则表达式", el("code", { class: "pattern-text", translate: "no" }, rule.pattern), "pattern-cell"),
        cell("状态", [el("label", { class: "switch", for: toggle.id }, toggle, el("span", { class: "track", "aria-hidden": "true" }), statusLabel), errorBox]),
        cell("更新时间 (UTC)", date(rule.updated_at), "time-cell"),
        cell("操作", el("div", { class: "row-actions" }, edit, remove), "action-cell")));
    });
    region.append(grid.wrap, pagination(filters.page, pages, filtered.length, (page) => { filters.page = page; paint(); }));
  }
  await reload();
}

function openRuleModal(rule, onSaved) {
  const modal = openModal(rule ? "编辑规则 #" + rule.id : "新增规则", "全局规则 · 对所有群组生效");
  if (!modal) return;
  const original = rule ? rule.pattern : "";
  const pattern = el("textarea", { id: "rm-pattern", name: "pattern", class: "mono", rows: "3", value: original,
    required: true, autocomplete: "off", spellcheck: "false", placeholder: "例如：免费.*领取…" });
  const counter = el("span", { class: "hint" });
  const testText = el("textarea", { id: "rm-test-text", name: "sample", rows: "4", autocomplete: "off", placeholder: "输入待检测的消息…" });
  const result = el("div", { class: "test-result", role: "status" });
  const errors = el("div", { id: "rm-error" });
  let revision = 0;
  function changed() {
    revision++;
    counter.textContent = Array.from(pattern.value).length + " / 512 字符";
    empty(result); empty(errors);
    pattern.removeAttribute("aria-invalid");
  }
  changed();
  pattern.addEventListener("input", changed);
  testText.addEventListener("input", () => { revision++; empty(result); });
  modal.dirty = () => pattern.value !== original;
  const test = button("测试匹配", "flask-conical", () => busy(test, "测试中…", async () => {
    const version = revision;
    empty(result);
    try {
      const outcome = await api("/api/rules/test", { method: "POST", body: { pattern: pattern.value, text: testText.value } });
      if (version !== revision || !modal.dialog.isConnected) return;
      result.append(el("span", { class: outcome.matched ? "badge ok" : "badge neutral" }, icon(outcome.matched ? "check" : "x"),
        outcome.matched ? "匹配，会触发拦截" : "未匹配"));
    } catch (err) { if (version === revision) result.append(notice(err.message)); }
  }));
  const form = el("form", { id: "rule-form" },
    field("正则表达式", pattern, "RE2 正则，忽略大小写"), counter,
    field("测试文本", testText), el("div", { class: "test-actions" }, test, result), errors);
  modal.content.append(form);
  const save = button(rule ? "保存修改" : "添加规则", "check", null, { type: "submit", form: "rule-form", class: "btn primary" });
  modal.actions.append(button("取消", null, () => modal.close()), save);
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    empty(errors);
    if (!pattern.value.trim() || Array.from(pattern.value).length > 512) {
      errors.append(notice("正则表达式不能为空，且不能超过 512 个字符。"));
      pattern.setAttribute("aria-invalid", "true"); pattern.focus(); return;
    }
    busy(save, "保存中…", async () => {
      modal.saving = true;
      pattern.disabled = true;
      test.disabled = true;
      try {
        const response = await api(rule ? "/api/rules/" + rule.id : "/api/rules", {
          method: rule ? "PUT" : "POST", body: { pattern: pattern.value },
        });
        feedback(response, rule ? "规则 #" + rule.id + " 已更新" : "规则已添加");
        modal.close(true);
        await onSaved();
      } catch (err) {
        if (modal.dialog.isConnected) {
          errors.append(notice(err.message)); pattern.disabled = false; pattern.setAttribute("aria-invalid", "true"); pattern.focus();
        }
      } finally { modal.saving = false; pattern.disabled = false; test.disabled = false; }
    });
  });
}
function confirmDeleteRule(rule, onSaved) {
  const modal = openModal("删除规则 #" + rule.id);
  if (!modal) return;
  modal.content.append(el("p", null, "删除后，所有群组都会停止使用这条规则。此操作无法撤销。"),
    el("pre", { class: "message-content", translate: "no" }, rule.pattern));
  const remove = button("删除规则", "trash-2", () => busy(remove, "删除中…", async () => {
    modal.saving = true;
    try {
      await api("/api/rules/" + rule.id, { method: "DELETE" });
      modal.close(true);
      toast("规则 #" + rule.id + " 已删除");
      await onSaved();
    } catch (err) { toast(err.message, "err"); }
    finally { modal.saving = false; }
  }), { class: "btn danger" });
  modal.actions.append(button("取消", null, () => modal.close()), remove);
}
async function downloadRuleExport(btn) {
  await busy(btn, "导出中…", async () => {
    try {
      const rules = await api("/api/rules/export");
      const url = URL.createObjectURL(new Blob([JSON.stringify(rules, null, 2)], { type: "application/json" }));
      const link = el("a", { href: url, download: "rules-export.json" });
      document.body.append(link); link.click(); link.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
      toast("规则已导出");
    } catch (err) { toast(err.message, "err"); }
  });
}

function rememberBuiltinCatalog(data) {
  state.builtinCatalog = new Map((data.rules || []).map((rule) => [rule.id, rule]));
  state.builtinCatalogLoaded = true;
}
async function loadBuiltinCatalog() {
  if (state.builtinCatalogLoaded) return;
  try { rememberBuiltinCatalog(await api("/api/builtin-rules")); }
  catch { /* Audit history remains readable by stored names or stable IDs. */ }
}
function builtinHitName(hit) {
  return hit.name || state.builtinCatalog.get(hit.id)?.name || hit.id;
}
function builtinEvidence(hits = []) {
  return el("ul", { class: "builtin-evidence" }, hits.map((hit) => el("li", null,
    el("strong", null, builtinHitName(hit)), hit.category ? el("span", { class: "small muted" }, hit.category) : null,
    el("code", { class: "small muted", translate: "no" }, hit.id),
    el("ul", null, (hit.evidence || []).map((evidence) => el("li", null, evidence))))));
}
function hitBadges(entry) {
  return [
    ...(entry.builtin_hits || []).map((id) => {
      const hit = entry.builtin_details?.hits?.find((item) => item.id === id) || { id };
      return el("span", { class: "badge builtin", title: id }, builtinHitName(hit));
    }),
    ...(entry.matched_rule_ids || []).map((id) => el("a", { class: "badge neutral", href: hashURL("rules", { q: "#" + id }) }, "规则 #" + id)),
  ];
}
function chatName(id) { return state.chats.find((chat) => String(chat.id) === String(id))?.title || "群组 " + id; }
async function renderAudit(view) {
  const params = routeParams();
  const filters = {
    chat_id: params.get("chat_id") || "", from: params.get("from") || "", to: params.get("to") || "",
    success: ["true", "false"].includes(params.get("success")) ? params.get("success") : "",
    rule_id: params.get("rule_id") || "", page: positiveInt(params.get("page")),
    page_size: [20, 50, 100].includes(Number(params.get("page_size"))) ? Number(params.get("page_size")) : 20,
  };
  const chat = el("select", { id: "af-chat", name: "chat_id" }, el("option", { value: "" }, "全部群组"));
  if (filters.chat_id) chat.append(el("option", { value: filters.chat_id }, "群组 " + filters.chat_id));
  chat.value = filters.chat_id;
  const from = el("input", { id: "af-from", name: "from", type: "date", value: filters.from });
  const to = el("input", { id: "af-to", name: "to", type: "date", value: filters.to });
  const success = el("select", { id: "af-success", name: "success", value: filters.success },
    el("option", { value: "" }, "全部结果"), el("option", { value: "true" }, "删除成功"), el("option", { value: "false" }, "删除失败"));
  const ruleId = el("input", { id: "af-rule-id", name: "rule_id", type: "number", min: "1", step: "1", inputmode: "numeric",
    value: filters.rule_id, placeholder: "例如：12…", autocomplete: "off" });
  const pageSize = el("select", { id: "af-page-size", name: "page_size", value: filters.page_size },
    [20, 50, 100].map((size) => el("option", { value: size }, size + " 条 / 页")));
  const formError = el("div", { id: "audit-filter-error" });
  const region = el("div", { id: "audit-region" });
  const apply = button("查询", "search", null, { type: "submit", class: "btn primary" });
  const reset = button("重置", "x", () => {
    [chat, from, to, success, ruleId].forEach((input) => { input.value = ""; });
    pageSize.value = "20";
    submitFilters();
  });
  const form = el("form", { class: "audit-filters" },
    el("div", { class: "filter-bar" }, field("群组", chat), field("删除结果", success), field("命中规则 ID", ruleId), field("每页条数", pageSize)),
    el("div", { class: "filter-bar date-filters" }, field("开始日期 (UTC)", from), field("结束日期 (UTC)", to),
      el("div", { class: "quick-dates", "aria-label": "快捷日期" },
        [["今天", 1], ["近 7 天", 7], ["近 30 天", 30]].map(([label, days]) => button(label, null, () => {
          from.value = utcDay(1 - days); to.value = utcDay(); submitFilters();
        }, { class: "btn subtle", "aria-pressed": "false", dataset: { days } }))),
      el("div", { class: "actions" }, reset, apply)), formError);
  empty(view).append(pageHeader("审计日志", "消息摘要与删除结果", [iconButton("刷新日志", "refresh-cw", () => refresh())]), form, region);
  const controls = { chat_id: chat, from, to, success, rule_id: ruleId, page_size: pageSize };
  let request = 0;
  let controller;
  function updateQuickDates() {
    form.querySelectorAll("[data-days]").forEach((btn) => {
      const active = from.value === utcDay(1 - Number(btn.dataset.days)) && to.value === utcDay();
      btn.setAttribute("aria-pressed", String(active));
    });
  }
  function submitFilters() {
    if (!form.reportValidity()) return;
    empty(formError);
    if (from.value && to.value && from.value > to.value) {
      formError.append(notice("开始日期不能晚于结束日期。"));
      from.setAttribute("aria-invalid", "true"); from.focus(); return;
    }
    from.removeAttribute("aria-invalid");
    for (const [key, input] of Object.entries(controls)) filters[key] = input.value;
    filters.page = 1;
    refresh();
  }
  form.addEventListener("submit", (event) => { event.preventDefault(); submitFilters(); });
  [chat, success, pageSize].forEach((input) => input.addEventListener("change", submitFilters));
  [from, to].forEach((input) => input.addEventListener("change", updateQuickDates));
  async function refresh() {
    if (!view.isConnected) return;
    updateQuickDates();
    syncParams(filters);
    controller?.abort();
    controller = new AbortController();
    const current = ++request;
    region.setAttribute("aria-busy", "true");
    if (!region.children.length) region.append(loading("正在查询日志…"));
    try {
      const query = new URLSearchParams();
      for (const [key, value] of Object.entries(filters)) if (value !== "") query.set(key, value);
      const [page] = await Promise.all([
        api("/api/audit?" + query.toString(), { signal: controller.signal }), loadBuiltinCatalog(),
      ]);
      if (!view.isConnected || current !== request) return;
      if (page.total_pages > 0 && page.page > page.total_pages) {
        filters.page = page.total_pages; refresh(); return;
      }
      if (!page.total && filters.page !== 1) { filters.page = 1; refresh(); return; }
      filters.page = page.page;
      empty(region);
      const count = el("p", { class: "result-count", role: "status" }, "找到 " + num(page.total) + " 条记录");
      region.append(count);
      if (!page.items.length) region.append(emptyState("没有符合条件的记录", button("清除筛选", "x", () => reset.click())));
      else region.append(renderAuditTable(page.items));
      region.append(pagination(page.page, page.total_pages, page.total, (value) => { filters.page = value; refresh(); }));
      panelMotion.reveal(region);
    } catch (err) { if (current === request) renderError(region, err, refresh); }
    finally { if (current === request) region.removeAttribute("aria-busy"); }
  }
  async function loadChats() {
    try {
      state.chats = await api("/api/chats") || [];
      if (!view.isConnected) return;
      const selected = chat.value;
      empty(chat).append(el("option", { value: "" }, "全部群组"));
      state.chats.forEach((item) => chat.append(el("option", { value: item.id }, (item.title || "群组") + " (" + item.id + ")")));
      if (selected && !state.chats.some((item) => String(item.id) === selected)) chat.append(el("option", { value: selected }, "群组 " + selected));
      chat.value = selected;
      view.querySelectorAll("[data-chat-title]").forEach((node) => { node.textContent = chatName(node.dataset.chatTitle); });
    } catch (err) {
      if (view.isConnected && err.status !== 401) toast("群组列表加载失败，仍可按日期和结果筛选。", "warn");
    }
  }
  await Promise.all([loadChats(), refresh()]);
}
function renderAuditTable(entries) {
  const grid = table(["时间 (UTC)", "群组", "消息摘要", "命中规则", "删除结果", "详情"], "audit-table" + (entries.length > 50 ? " large-list" : ""));
  entries.forEach((entry) => {
    const summary = el("div", { class: "summary-cell" },
      el("p", { class: "summary-preview" }, entry.content_summary || "无消息摘要"));
    if (entry.content_summary && (Array.from(entry.content_summary).length > 45 || entry.content_summary.includes("\n"))) {
      summary.append(el("details", { class: "summary-expand" }, el("summary", null, "展开摘要"),
        el("pre", { class: "message-content" }, entry.content_summary)));
    }
    grid.body.append(el("tr", { class: entry.delete_succeeded ? "" : "failed-row" },
      cell("时间 (UTC)", date(entry.occurred_at), "time-cell"),
      cell("群组", [el("span", { class: "chat-title", dataset: { chatTitle: entry.chat_id } }, chatName(entry.chat_id)),
        el("span", { class: "small muted" }, String(entry.chat_id))], "chat-cell"),
      cell("消息摘要", summary),
      cell("命中规则", el("div", { class: "badges" }, hitBadges(entry))),
      cell("删除结果", [el("span", { class: "badge " + (entry.delete_succeeded ? "ok" : "err") },
        icon(entry.delete_succeeded ? "check" : "circle-alert"), entry.delete_succeeded ? "已删除" : "删除失败"),
        !entry.delete_succeeded ? el("p", { class: "failure-reason" }, entry.deletion_error || "未返回错误详情") : null]),
      cell("详情", iconButton("查看审计记录 #" + entry.id, "arrow-up-right", () => openAuditDetail(entry.id)), "action-cell")));
  });
  return grid.wrap;
}
async function openAuditDetail(id) {
  const modal = openModal("审计记录 #" + id);
  if (!modal) return;
  modal.content.append(loading("正在加载记录…"));
  modal.actions.append(button("关闭", null, () => modal.close()));
  async function load() {
    try {
      const [entry] = await Promise.all([api("/api/audit/" + id), loadBuiltinCatalog()]);
      if (!modal.dialog.isConnected) return;
      empty(modal.content).append(
        el("div", { class: "detail-status" }, el("span", { class: "badge " + (entry.delete_succeeded ? "ok" : "err") },
          icon(entry.delete_succeeded ? "check" : "circle-alert"), entry.delete_succeeded ? "消息已删除" : "消息删除失败")),
        el("dl", { class: "detail-grid" },
          el("dt", null, "时间 (UTC)"), el("dd", null, date(entry.occurred_at)),
          el("dt", null, "群组"), el("dd", null, chatName(entry.chat_id) + " (" + entry.chat_id + ")"),
          el("dt", null, "用户 ID"), el("dd", { class: "mono" }, entry.user_id ?? "无"),
          el("dt", null, "消息 / 话题 ID"), el("dd", { class: "mono" }, String(entry.message_id) + " / " + (entry.message_thread_id ?? "无")),
          el("dt", null, "命中规则"), el("dd", { class: "badges" }, hitBadges(entry))),
        el("h3", null, "消息摘要"),
        el("pre", { class: "message-content" }, entry.content_summary || "无消息摘要"),
        el("p", { class: "hint" }, "审计仅保存最多 120 字的摘要，较长消息的完整内容未保留。"));
      if (entry.builtin_details) {
        modal.content.append(el("section", { class: "builtin-audit-explanation" },
          el("h3", null, "内置检测原因"),
          el("p", { class: "hint" }, "命中时规则库版本：" + (entry.builtin_details.library_version || "未记录")),
          builtinEvidence(entry.builtin_details.hits || [])));
      } else if (entry.builtin_hits?.length) {
        modal.content.append(el("p", { class: "hint builtin-audit-legacy" }, "此历史记录未保存规则库版本和详细原因，仅显示原有命中项。"));
      }
      if (!entry.delete_succeeded) {
        modal.content.append(el("h3", null, "删除失败原因"),
          el("pre", { class: "message-content error-content" }, entry.deletion_error || "Telegram 未返回错误详情。"));
        if (/rights|permission|administrator|CHAT_ADMIN_REQUIRED/i.test(entry.deletion_error || "")) {
          modal.content.append(el("p", { class: "hint" }, "请检查机器人在该群组的管理员身份和删除消息权限。"));
        }
      }
      const copy = button("复制摘要", "copy", () => busy(copy, "复制中…", async () => {
        try { await navigator.clipboard.writeText(entry.content_summary || ""); toast("摘要已复制"); }
        catch { toast("复制失败，请选中摘要手动复制。", "err"); }
      }));
      modal.actions.prepend(copy);
    } catch (err) { renderError(modal.content, err, load); }
  }
  await load();
}

async function renderSettings(view) {
  try {
    const account = await api("/api/settings/account");
    if (!view.isConnected) return;
    let bot = null;
    try { bot = await api("/api/bot-settings"); } catch (err) { bot = null; }
    const username = el("input", { id: "set-user", name: "username", value: account.username, autocomplete: "username", spellcheck: "false",
      required: true, maxlength: "64", pattern: "[A-Za-z0-9_.\\-]{1,64}" });
    const userError = el("div");
    const saveUser = button("保存用户名", "check", null, { type: "submit", class: "btn primary" });
    const accountForm = el("form", null, field("用户名", username, "1–64 个字符，可使用字母、数字、下划线、点和短横线。"), userError, saveUser);
    accountForm.addEventListener("submit", (event) => {
      event.preventDefault();
      busy(saveUser, "保存中…", async () => {
        empty(userError);
        try {
          await api("/api/settings/account", { method: "POST", body: { username: username.value.trim() } });
          state.authenticated = false; renderShell(); toast("用户名已更新，请重新登录。");
        } catch (err) { userError.append(notice(err.message)); username.focus(); }
      });
    });
    const current = el("input", { id: "set-pw-current", name: "current_password", type: "password", autocomplete: "current-password", required: true });
    const next = el("input", { id: "set-pw-new", name: "new_password", type: "password", autocomplete: "new-password", minlength: "8", required: true });
    const confirm = el("input", { id: "set-pw-confirm", name: "confirm_password", type: "password", autocomplete: "new-password", minlength: "8", required: true });
    const passwordError = el("div");
    const savePassword = button("保存密码", "check", null, { type: "submit", class: "btn primary" });
    const passwordForm = el("form", null, field("当前密码", current), field("新密码", next, "至少 8 个字符。"), field("确认新密码", confirm), passwordError, savePassword);
    passwordForm.addEventListener("submit", (event) => {
      event.preventDefault();
      empty(passwordError);
      if (next.value !== confirm.value) { passwordError.append(notice("两次输入的新密码不一致。")); confirm.focus(); return; }
      busy(savePassword, "保存中…", async () => {
        try {
          await api("/api/settings/password", { method: "POST", body: { current_password: current.value, new_password: next.value } });
          state.authenticated = false; renderShell(); toast("密码已更新，请重新登录。");
        } catch (err) { passwordError.append(notice(err.message)); current.focus(); }
      });
    });
    // Runtime switches shared with the bot. They are global, so every control
    // here states that explicitly and rolls back when the save fails.
    const runtimeSection = (() => {
      if (!bot) {
        return el("section", { class: "section settings-section" },
          el("div", null, el("h2", null, "运行设置"), el("p", { class: "hint" }, "检测与权限开关。")),
          notice("运行设置接口不可用，请刷新页面或检查服务状态。", "warn"));
      }
      const controls = el("div", { class: "settings-controls" });
      const toggleRow = (id, label, hint, checked, save) => {
        const error = el("div");
        const status = el("span", { class: "status-label" }, checked ? "已开启" : "已关闭");
        const input = el("input", { type: "checkbox", role: "switch", id, checked, "aria-label": label });
        input.addEventListener("change", async () => {
          const next = input.checked;
          empty(error);
          input.disabled = true;
          try {
            await save(next);
            status.textContent = next ? "已开启" : "已关闭";
            toast(label + (next ? "已开启。" : "已关闭。"));
          } catch (err) {
            input.checked = !next;
            status.textContent = !next ? "已开启" : "已关闭";
            error.append(notice(err.message));
          } finally { input.disabled = false; }
        });
        return el("div", { class: "settings-toggle" },
          el("div", null, el("h3", null, label), el("p", { class: "hint" }, hint), error),
          el("label", { class: "switch", for: id }, input, el("span", { class: "track", "aria-hidden": "true" }), status));
      };
      const patch = (body) => api("/api/bot-settings", { method: "PATCH", body });
      const crossWarning = el("div");
      const paintCrossWarning = () => {
        empty(crossWarning);
        if (bot && bot.cross_group_management) {
          crossWarning.append(notice("跨群管理已开启：任何群成员都可以新增、删除或停用规则，请仅在完全信任群成员时使用。", "warn"));
        }
      };
      controls.append(
        toggleRow("set-bio-check", "简介辅助检测", "开启后，对“看我主页”等主动引流消息查询发送者简介；简介独立命中内置库才删除。", bot.bio_check_enabled,
          async (enabled) => { bot = await patch({ bio_check_enabled: enabled }); }),
        toggleRow("set-cross-group", "跨群管理权限", "开启后，非本群管理员也可以执行管理命令（命令文本仍会被广告规则审核）。默认关闭。", bot.cross_group_management,
          async (enabled) => { bot = await patch({ cross_group_management: enabled }); paintCrossWarning(); }),
        crossWarning);
      paintCrossWarning();
      const ownerInput = el("input", { id: "set-owner-ids", name: "owner_user_ids", autocomplete: "off", spellcheck: "false",
        inputmode: "numeric", placeholder: "例如：123456789, 987654321", value: (bot.owner_user_ids || []).join(", ") });
      const ownerError = el("div");
      const saveOwners = button("保存所有者", "check", null, { type: "submit", class: "btn primary" });
      const ownerForm = el("form", null,
        field("机器人所有者用户 ID", ownerInput, "逗号或空格分隔。所有者无需是本群管理员即可管理机器人，并可在群里用 /settings 修改运行开关。"),
        ownerError, saveOwners);
      ownerForm.addEventListener("submit", (event) => {
        event.preventDefault();
        const ids = ownerInput.value.split(/[\s,;]+/).filter(Boolean).map((raw) => Number(raw));
        if (ids.some((id) => !Number.isSafeInteger(id) || id <= 0)) {
          empty(ownerError).append(notice("所有者必须是正整数 Telegram 用户 ID。"));
          ownerInput.focus();
          return;
        }
        busy(saveOwners, "保存中…", async () => {
          empty(ownerError);
          try {
            bot = await patch({ owner_user_ids: ids });
            ownerInput.value = (bot.owner_user_ids || []).join(", ");
            toast("所有者名单已更新。");
          } catch (err) { ownerError.append(notice(err.message)); ownerInput.focus(); }
        });
      });
      controls.append(ownerForm);
      return el("section", { class: "section settings-section" },
        el("div", null, el("h2", null, "运行设置"),
          el("p", { class: "hint" }, "对所有群组生效。群管理员也可以在群里用 /settings 查看。")),
        controls);
    })();
    empty(view).append(pageHeader("设置", "当前登录：" + account.username),
      el("div", { class: "settings-sections" },
        el("section", { class: "section settings-section" }, el("div", null, el("h2", null, "登录账号"), el("p", { class: "hint" }, "修改后需要重新登录。")), accountForm),
        el("section", { class: "section settings-section" }, el("div", null, el("h2", null, "登录密码"), el("p", { class: "hint" }, "修改后所有现有会话将退出。")), passwordForm),
        runtimeSection));
  } catch (err) { renderError(view, err, () => renderView()); }
}
async function boot() {
  themeLabel();
  document.getElementById("theme-toggle").addEventListener("click", switchTheme);
  document.getElementById("logout-btn").addEventListener("click", doLogout);
  if (typeof ResizeObserver === "function") {
    new ResizeObserver(() => moveNavIndicator(false)).observe(document.querySelector(".nav"));
  } else window.addEventListener("resize", () => moveNavIndicator(false));
  const region = document.getElementById("view");
  empty(region).append(loading("正在连接面板…"));
  try {
    const session = await api("/api/session");
    state.authenticated = !!session.authenticated;
    state.username = session.username || "";
    renderShell();
  } catch (err) { renderError(region, err, () => location.reload()); }
}
document.querySelector(".skip-link").addEventListener("click", (event) => {
  event.preventDefault(); document.getElementById("view").focus();
});
window.addEventListener("hashchange", navigate);
boot();
