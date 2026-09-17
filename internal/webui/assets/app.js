"use strict";

/* ── Small DOM helpers (XSS-safe: never use innerHTML for data) ── */

function el(tag, props, ...children) {
  const node = document.createElement(tag);
  if (props) {
    for (const [key, value] of Object.entries(props)) {
      if (key === "class") node.className = value;
      else if (key === "checked") node.checked = value;
      else if (key === "disabled") node.disabled = value;
      else if (key === "dataset") Object.assign(node.dataset, value);
      else if (key.startsWith("on") && typeof value === "function") {
        node.addEventListener(key.slice(2), value);
      } else if (value !== null && value !== undefined) {
        node.setAttribute(key, value);
      }
    }
  }
  for (const child of children.flat()) {
    if (child === null || child === undefined) continue;
    node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return node;
}

function txt(value) { return document.createTextNode(value ?? ""); }

function empty(node) { while (node.firstChild) node.removeChild(node.firstChild); return node; }

/* ── API client ──────────────────────────────────────────────── */

async function api(path, options = {}) {
  const init = {
    method: options.method || "GET",
    credentials: "same-origin",
    headers: { "X-Requested-With": "fetch" },
  };
  if (options.body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(options.body);
  }
  const res = await fetch(path, init);
  let data = null;
  const contentType = res.headers.get("content-type") || "";
  if (res.status !== 204 && contentType.includes("json")) {
    data = await res.json().catch(() => null);
  }
  if (!res.ok) {
    const err = new Error((data && data.error) || `请求失败（${res.status}）`);
    err.status = res.status;
    err.code = data && data.code;
    throw err;
  }
  return data;
}

/* ── Toast ───────────────────────────────────────────────────── */

function toast(message, kind = "") {
  const node = el("div", { class: `toast ${kind}` }, message);
  document.body.append(node);
  requestAnimationFrame(() => node.classList.add("show"));
  setTimeout(() => { node.classList.remove("show"); setTimeout(() => node.remove(), 250); }, 2600);
}

/* ── State ───────────────────────────────────────────────────── */

const state = {
  authenticated: false,
  username: "",
  route: "dashboard",
  chats: [],
  chatFilter: "",          // "" = all chats
};

/* ── Router ──────────────────────────────────────────────────── */

function parseHash() {
  const hash = location.hash || "#/dashboard";
  const route = hash.replace(/^#\//, "").split("?")[0] || "dashboard";
  return route;
}

function navigate() {
  state.route = parseHash();
  document.querySelectorAll(".nav-item[data-route]").forEach((item) => {
    item.classList.toggle("active", item.dataset.route === state.route);
  });
  renderView();
}

/* ── App shell ───────────────────────────────────────────────── */

async function boot() {
  applyTheme();
  document.getElementById("theme-toggle").addEventListener("click", toggleTheme);
  document.getElementById("logout-btn").addEventListener("click", doLogout);

  let session;
  try { session = await api("/api/session"); } catch { session = { authenticated: false }; }
  state.authenticated = !!session.authenticated;
  state.username = session.username || "";
  renderShell();
}

function renderShell() {
  const sidebar = document.getElementById("sidebar");
  const view = document.getElementById("view");

  if (!state.authenticated) {
    sidebar.hidden = true;
    empty(view).append(renderLogin());
    return;
  }
  sidebar.hidden = false;
  navigate();
}

/* ── Theme ───────────────────────────────────────────────────── */

function applyTheme() {
  const saved = localStorage.getItem("panel-theme");
  const dark = saved ? saved === "dark" : true;
  document.documentElement.dataset.theme = dark ? "dark" : "light";
}
function toggleTheme() {
  const next = document.documentElement.dataset.theme === "dark" ? "light" : "dark";
  document.documentElement.dataset.theme = next;
  localStorage.setItem("panel-theme", next);
}

/* ── Login ───────────────────────────────────────────────────── */

function renderLogin() {
  const card = el("div", { class: "login-card" },
    el("h1", null, "广告拦截管理面板"),
    el("p", { class: "sub" }, "登录后管理群组规则与审计日志"),
    el("div", { class: "field" }, el("label", { for: "login-user" }, "用户名"),
      el("input", { id: "login-user", autocomplete: "username", required: true })),
    el("div", { class: "field" }, el("label", { for: "login-pass" }, "密码"),
      el("input", { id: "login-pass", type: "password", autocomplete: "current-password", required: true })),
    el("div", { id: "login-error" }),
    el("div", { class: "actions" },
      el("button", { id: "login-btn", class: "btn primary block", type: "submit" }, "登 录")),
  );

  return el("form", { class: "login-wrap", onsubmit: (e) => { e.preventDefault(); doLogin(); } }, card);
}

async function doLogin() {
  const username = document.getElementById("login-user").value.trim();
  const password = document.getElementById("login-pass").value;
  const errorBox = document.getElementById("login-error");
  const btn = document.getElementById("login-btn");
  empty(errorBox);
  if (!username || !password) {
    errorBox.append(el("div", { class: "form-error" }, "请输入用户名和密码。"));
    return;
  }
  btn.disabled = true;
  try {
    await api("/api/login", { method: "POST", body: { username, password } });
    state.authenticated = true;
    state.username = username;
    renderShell();
    toast("登录成功");
  } catch (err) {
    errorBox.append(el("div", { class: "form-error" }, err.message));
  } finally {
    btn.disabled = false;
  }
}

async function doLogout() {
  try { await api("/api/logout", { method: "POST" }); } catch { /* ignore */ }
  location.hash = "#/dashboard";
  state.authenticated = false;
  renderShell();
}

/* ── View renderers ──────────────────────────────────────────── */

function renderView() {
  const view = document.getElementById("view");
  empty(view).append(el("div", { class: "spinner" }));
  if (state.route === "dashboard") renderDashboard(view);
  else if (state.route === "rules") renderRules(view);
  else if (state.route === "audit") renderAudit(view);
  else if (state.route === "settings") renderSettings(view);
  else {
    empty(view);
    view.append(el("div", { class: "empty" }, "页面不存在。"),
      el("a", { href: "#/dashboard", class: "btn", style: "margin-top:16px" }, "返回仪表盘"));
  }
}

/* ── Dashboard ───────────────────────────────────────────────── */

async function renderDashboard(view) {
  try {
    const [overview, trend] = await Promise.all([api("/api/dashboard/overview"), api("/api/dashboard/trend")]);
    empty(view);
    view.append(
      el("h2", null, "仪表盘"),
      el("p", { class: "page-desc" }, "群组防护与拦截情况概览"),
      renderStatCards(overview),
      el("div", { class: "chart-card card" },
        el("h3", { style: "margin:0 0 14px" }, "近 30 天拦截趋势"),
        renderTrendChart(trend, "chart-0")),
    );
  } catch (err) { renderError(view, err); }
}

function renderStatCards(overview) {
  const cards = [
    ["群组", overview.total_chats, "", "accent"],
    ["规则总数", overview.total_rules, "", ""],
    ["已启用规则", overview.enabled_rules, "", ""],
    ["今日命中", overview.hits_today, "", ""],
    ["今日删除成功", overview.deleted_today, "", "ok"],
    ["今日删除失败", overview.failed_today, "", "err"],
    ["近 7 日命中", overview.hits_7day, "", ""],
    ["累计拦截", overview.total_hits, "", ""],
  ];
  return el("div", { class: "stats-grid" },
    cards.map(([label, value, , tone]) =>
      el("div", { class: `stat ${tone}` }, el("div", { class: "label" }, label), el("div", { class: "value" }, value))));
}

function renderTrendChart(data, id) {
  const width = 860, height = 260, padL = 40, padB = 28, padT = 14, gap = 4;
  const innerW = width - padL - 12, innerH = height - padT - padB;
  const max = Math.max(1, ...data.map((d) => d.hits));
  const barW = (innerW - (data.length - 1) * gap) / data.length;

  const svgNS = "http://www.w3.org/2000/svg";
  const table = document.createElementNS(svgNS, "svg");
  table.setAttribute("viewBox", `0 0 ${width} ${height}`);
  table.setAttribute("preserveAspectRatio", "xMidYMid meet");
  table.style.width = "100%";
  table.style.height = "auto";
  table.setAttribute("role", "img");
  void id;

  // horizontal gridlines
  for (let i = 0; i <= 4; i++) {
    const y = padT + innerH - (innerH * i) / 4;
    const line = document.createElementNS(svgNS, "line");
    line.setAttribute("x1", padL); line.setAttribute("x2", width - 12);
    line.setAttribute("y1", y); line.setAttribute("y2", y);
    line.setAttribute("stroke", "var(--border)"); line.setAttribute("stroke-width", "1");
    table.append(line);
    const label = document.createElementNS(svgNS, "text");
    label.setAttribute("x", padL - 8); label.setAttribute("y", y + 4);
    label.setAttribute("text-anchor", "end"); label.setAttribute("font-size", "10");
    label.setAttribute("fill", "var(--muted)");
    label.textContent = Math.round((max * i) / 4);
    table.append(label);
  }

  data.forEach((d, i) => {
    const h = Math.max(1.5, (d.hits / max) * innerH);
    const x = padL + i * (barW + gap);
    const y = padT + innerH - h;
    const bar = document.createElementNS(svgNS, "rect");
    bar.setAttribute("x", x); bar.setAttribute("y", y);
    bar.setAttribute("width", barW); bar.setAttribute("height", h);
    bar.setAttribute("rx", 3);
    bar.setAttribute("fill", "var(--accent)");
    const tip = document.createElementNS(svgNS, "title");
    tip.textContent = `${d.date}：命中 ${d.hits}，删除成功 ${d.deleted}，失败 ${d.failed}`;
    bar.append(tip);
    table.append(bar);
    if (i % Math.max(1, Math.floor(data.length / 12)) === 0 || i === data.length - 1) {
      const label = document.createElementNS(svgNS, "text");
      label.setAttribute("x", x + barW / 2); label.setAttribute("y", height - 10);
      label.setAttribute("text-anchor", "middle"); label.setAttribute("font-size", "9");
      label.setAttribute("fill", "var(--muted)");
      label.textContent = d.date.slice(5);
      table.append(label);
    }
  });
  return table;
}

/* ── Rules ───────────────────────────────────────────────────── */

async function renderRules(view) {
  const wrap = el("div");
  empty(view).append(wrap);
  try {
    const [chats, selectedRules] = await loadRulesData();
    await paintRules(wrap, chats, selectedRules);
  } catch (err) { renderError(view, err); }
}

async function loadRulesData() {
  const chats = await api("/api/chats");
  state.chats = chats || [];
  if (state.activeChat === undefined) state.activeChat = "";
  await refreshSelectedRules();
  return [state.chats, state.selectedRules];
}

async function refreshSelectedRules() {
  state.selectedRules = [];
  if (state.activeChat !== "") {
    state.selectedRules = await api(`/api/chats/${state.activeChat}/rules`);
  }
}

async function paintRules(wrap, chats, selectedRules) {
  empty(wrap);
  wrap.append(
    el("h2", null, "规则管理"),
    el("p", { class: "page-desc" }, "按群组维护广告拦截正则，改动即时生效"),
    rulesToolbar(chats),
    el("div", { id: "rule-summary" }),
  );

  const tableWrap = el("div", { class: "table-wrap" });
  const table = el("table", { class: "data" },
    el("thead", null, el("tr", null,
      el("th", null, "ID"), el("th", null, "正则表达式"), el("th", null, "状态"),
      el("th", null, "创建时间"), el("th", null, "操作"))));
  const tbody = el("tbody");
  table.append(tbody);
  tableWrap.append(table);
  wrap.append(tableWrap);

  const summary = document.getElementById("rule-summary");
  if (!selectedRules.length) {
    empty(summary).append(el("div", { class: "empty" }, "该群组暂无规则，点击右上角「新增规则」添加。"));
    return;
  }
  empty(summary).append(el("div", { class: "hint" },
    `共 ${selectedRules.length} 条规则，启用 ${selectedRules.filter((r) => r.enabled).length} 条。`));
  for (const rule of selectedRules) {
    const statusBadge = rule.enabled
      ? el("span", { class: "badge ok" }, "启用")
      : el("span", { class: "badge muted" }, "停用");
    const toggle = el("span", { class: "switch" },
      el("input", { type: "checkbox", checked: rule.enabled,
        onchange: () => toggleRule(rule) }),
      el("span", { class: "track" }));
    tbody.append(el("tr", null,
      el("td", { class: "mono" }, rule.id),
      el("td", { class: "pattern-cell mono", title: rule.pattern }, rule.pattern),
      el("td", null, toggle, " ", statusBadge),
      el("td", { class: "mono" }, rule.created_at.slice(0, 19).replace("T", " ")),
      el("td", null, el("div", { class: "row-actions" },
        el("button", { class: "btn sm ghost", onclick: () => openRuleModal(rule) }, "编辑"),
        el("button", { class: "btn sm danger", onclick: () => confirmDeleteRule(rule) }, "删除")))));
  }
}

function rulesToolbar(chats) {
  const select = el("select", { id: "chat-select", onchange: async (e) => {
    const value = e.target.value;
    if (value === "__manual") return; // manual input is separate
    state.activeChat = value;
    try {
      await refreshSelectedRules();
      await paintRules(document.getElementById("view"), state.chats, state.selectedRules);
    } catch (err) { toast(err.message, "err"); }
  } },
    el("option", { value: "" }, "选择群组…"),
    chats.map((chat) => el("option", { value: String(chat.id) },
      (chat.title ? chat.title : `群组 ${chat.id}`) + `（${chat.id}，规则 ${chat.rule_count}）`)),
    el("option", { value: "__manual" }, "＋ 手动输入群组 ID…"),
  );
  const manualInput = el("input", { id: "manual-chat", placeholder: "手工群组 ID（冷启动群组）", hidden: true });
  select.addEventListener("change", () => {
    manualInput.hidden = select.value !== "__manual";
    if (select.value === "__manual") manualInput.focus();
  });
  const applyManual = el("button", {
    class: "btn sm", hidden: true, onclick: async () => {
      const value = manualInput.value.trim();
      if (!value || !/^-?\d+$/.test(value)) { toast("群组 ID 无效", "err"); return; }
      state.activeChat = value;
      try {
        await refreshSelectedRules();
        await paintRules(document.getElementById("view"), state.chats, state.selectedRules);
        manualInput.hidden = true; applyManual.hidden = true;
      } catch (err) { toast(err.message, "err"); }
    },
  }, "确定");
  const syncManual = () => { applyManual.hidden = manualInput.hidden = select.value !== "__manual"; };
  select.addEventListener("change", syncManual);

  const add = el("button", { class: "btn primary sm", onclick: () => openRuleModal(null) }, "＋ 新增规则");
  return el("div", { class: "toolbar" }, select, manualInput, applyManual, el("div", { class: "grow" }), add);
}

async function toggleRule(rule) {
  try {
    await api(`/api/chats/${state.activeChat}/rules/${rule.id}`, {
      method: "PATCH", body: { enabled: !rule.enabled },
    });
    toast(rule.enabled ? "规则已停用" : "规则已启用");
    await refreshSelectedRules();
    await paintRules(document.getElementById("view"), state.chats, state.selectedRules);
  } catch (err) { toast(err.message, "err"); }
}

async function confirmDeleteRule(rule) {
  const modal = openModal("删除规则", "");
  modal.content.append(
    el("p", null, "确定要删除规则 #", txt(rule.id), " 吗？"),
    el("p", { class: "regex-preview" }, rule.pattern),
  );
  modal.actions.append(
    el("button", { class: "btn", onclick: () => modal.close() }, "取消"),
    el("button", { class: "btn danger", onclick: async () => {
      try {
        await api(`/api/chats/${state.activeChat}/rules/${rule.id}`, { method: "DELETE" });
        toast("规则已删除");
        modal.close();
        await refreshSelectedRules();
        await paintRules(document.getElementById("view"), state.chats, state.selectedRules);
      } catch (err) { toast(err.message, "err"); }
    } }, "删除"));
}

function openRuleModal(rule) {
  const editing = !!rule;
  const modal = openModal(editing ? `编辑规则 #${rule.id}` : "新增规则", `群组 ${state.activeChat || "—"}`);

  const pattern = el("input", { id: "rm-pattern", class: "mono", value: rule ? rule.pattern : "", required: true,
    placeholder: "例如：免费.*领取" });
  const preview = el("div", { class: "regex-preview" }, "等待输入…");
  pattern.addEventListener("input", () => { preview.textContent = previewRegex(pattern.value); });

  const testText = el("textarea", { id: "rm-test-text", rows: 3, placeholder: "输入测试样本文本，点击「测试匹配」…" });
  const testResult = el("div", { class: "hint" });
  const errorBox = el("div", { id: "rm-error" });

  const testBtn = el("button", { class: "btn sm", onclick: async () => {
    empty(testResult);
    try {
      const result = await api("/api/rules/test", { method: "POST", body: { pattern: pattern.value, text: testText.value } });
      testResult.className = "hint " + (result.matched ? "ok" : "err");
      testResult.textContent = result.matched ? "✔ 匹配该文本（将触发拦截）" : "✘ 未匹配";
    } catch (err2) {
      testResult.className = "hint err";
      testResult.textContent = err2.message;
    }
  } }, "测试匹配");

  modal.content.append(
    el("div", { class: "field" },
      el("label", { for: "rm-pattern" }, "正则表达式（RE2，忽略大小写）"),
      pattern, preview),
    el("div", { class: "field" },
      el("label", { for: "rm-test-text" }, "测试文本"),
      testText, el("div", { style: "display:flex;gap:8px;margin-top:8px" }, testBtn, testResult)),
    errorBox,
  );

  modal.actions.append(
    el("button", { class: "btn", onclick: () => modal.close() }, "取消"),
    el("button", { class: "btn primary", onclick: async () => {
      empty(errorBox);
      const value = pattern.value.trim();
      if (!value) { errorBox.append(el("div", { class: "form-error" }, "正则表达式不能为空。")); return; }
      try {
        if (editing) {
          await api(`/api/chats/${state.activeChat}/rules/${rule.id}`, { method: "PUT", body: { pattern: value } });
          toast("规则已更新");
        } else {
          await api(`/api/chats/${state.activeChat}/rules`, { method: "POST", body: { pattern: value } });
          toast("规则已添加");
        }
        modal.close();
        await refreshSelectedRules();
        await paintRules(document.getElementById("view"), state.chats, state.selectedRules);
      } catch (err2) {
        errorBox.append(el("div", { class: "form-error" }, err2.message));
      }
    } }, editing ? "保存" : "添加"));
}

/* ── Audit ───────────────────────────────────────────────────── */

const auditState = { page: 1, pageSize: 20, total: 0 };

async function renderAudit(view) {
  try {
    const chats = state.chats.length ? state.chats : await api("/api/chats");
    state.chats = chats || [];
  } catch (err) { renderError(view, err); return; }
  // Static shell: title + filter bar are built once so pagination and filter
  // changes do not reset each other's state.
  empty(view).append(
    el("h2", null, "审计日志"),
    el("p", { class: "page-desc" }, "查询被拦截的消息与删除结果"),
    auditFilterBar(),
  );
  const region = el("div", { id: "audit-region", class: "spinner" });
  view.append(region);
  auditState.page = 1;
  await refreshAuditPage();
}

async function refreshAuditPage() {
  const region = document.getElementById("audit-region");
  if (!region) return;
  region.className = "spinner";
  const params = new URLSearchParams();
  const chat = document.getElementById("af-chat")?.value;
  const from = document.getElementById("af-from")?.value;
  const to = document.getElementById("af-to")?.value;
  const success = document.getElementById("af-success")?.value;
  const ruleId = document.getElementById("af-rule-id")?.value;
  if (chat) params.set("chat_id", chat);
  if (from) params.set("from", from);
  if (to) params.set("to", to);
  if (success === "true" || success === "false") params.set("success", success);
  if (ruleId) params.set("rule_id", ruleId);
  params.set("page", String(auditState.page));
  params.set("page_size", String(auditState.pageSize));

  try {
    const data = await api(`/api/audit?${params.toString()}`);
    auditState.total = data.total;
    region.className = "";
    renderAuditTable(region, data);
  } catch (err) {
    region.className = "";
    empty(region).append(el("div", { class: "form-error" }, err.message));
  }
}

function auditFilterBar() {
  const chat = el("select", { id: "af-chat" },
    el("option", { value: "" }, "全部群组"),
    state.chats.map((c) => el("option", { value: String(c.id) },
      (c.title ? c.title : `群组 ${c.id}`) + `（${c.id}）`)));
  const from = el("input", { id: "af-from", type: "date" });
  const to = el("input", { id: "af-to", type: "date" });
  const success = el("select", { id: "af-success" },
    el("option", { value: "" }, "全部结果"),
    el("option", { value: "true" }, "删除成功"),
    el("option", { value: "false" }, "删除失败"));
  const ruleId = el("input", { id: "af-rule-id", class: "mono", placeholder: "规则 ID", style: "width:100px" });
  const apply = el("button", {
    class: "btn sm", onclick: () => {
      auditState.page = 1;
      refreshAuditPage();
    },
  }, "查询");
  return el("div", { class: "toolbar" }, chat, from, to, success, ruleId, apply);
}

function renderAuditTable(wrap, data) {
  const existing = wrap.querySelector(".table-wrap, .pagination");
  const tableWrap = el("div", { class: "table-wrap" });
  const table = el("table", { class: "data" },
    el("thead", null, el("tr", null,
      el("th", null, "时间"), el("th", null, "群组"), el("th", null, "内容摘要"),
      el("th", null, "命中规则"), el("th", null, "结果"))));
  const tbody = el("tbody");
  table.append(tbody);
  tableWrap.append(table);
  if (existing) existing.remove();
  wrap.append(tableWrap);

  if (!data.items.length) {
    tbody.append(el("tr", null, el("td", { colspan: 5, class: "empty", style: "text-align:center" }, "没有符合条件的记录。")));
  }
  for (const item of data.items) {
    const badge = item.delete_succeeded
      ? el("span", { class: "badge ok" }, "已删除")
      : el("span", { class: "badge err", title: item.deletion_error || "" }, "删除失败");
    const ruleTags = (item.matched_rule_ids || []).map((id) => el("span", { class: "badge muted mono", style: "margin-right:4px" }, `#${id}`));
    tbody.append(el("tr", { class: "clickable", onclick: () => openAuditDetail(item.id) },
      el("td", { class: "mono" }, item.occurred_at.slice(0, 19).replace("T", " ")),
      el("td", null, txt(item.chat_id)),
      el("td", { class: "pattern-cell", title: item.content_summary }, item.content_summary || "（无摘要）"),
      el("td", null, ruleTags),
      el("td", null, badge)));
  }

  wrap.append(el("div", { class: "pagination" },
    el("span", { class: "info" }, `共 ${data.total} 条，第 ${data.page}/${data.total_pages || 1} 页`),
    el("button", { class: "btn sm", disabled: data.page <= 1, onclick: () => { auditState.page--; refreshAuditPage(); } }, "上一页"),
    el("button", { class: "btn sm", disabled: data.page >= data.total_pages, onclick: () => { auditState.page++; refreshAuditPage(); } }, "下一页")));
}

async function openAuditDetail(id) {
  let entry;
  try { entry = await api(`/api/audit/${id}`); }
  catch (err) { toast(err.message, "err"); return; }
  const modal = openModal(`审计记录 #${entry.id}`, "详情");
  modal.content.append(el("dl", { class: "detail-grid" },
    el("dt", null, "时间"), el("dd", { class: "mono" }, entry.occurred_at),
    el("dt", null, "群组 ID"), el("dd", { class: "mono" }, entry.chat_id),
    el("dt", null, "用户 ID"), el("dd", { class: "mono" }, entry.user_id ?? "—"),
    el("dt", null, "消息 ID"), el("dd", { class: "mono" }, entry.message_id),
    el("dt", null, "命中规则"), el("dd", null, (entry.matched_rule_ids || []).map((r) => txt(`#${r} `))),
    el("dt", null, "内容摘要"), el("dd", null, entry.content_summary || "（无）"),
    el("dt", null, "删除结果"), el("dd", null,
      entry.delete_succeeded ? el("span", { class: "badge ok" }, "成功") : el("span", { class: "badge err" }, "失败")),
    ...(entry.deletion_error ? [el("dt", null, "错误信息"), el("dd", null, entry.deletion_error)] : []),
  ));
  modal.actions.append(el("button", { class: "btn", onclick: () => modal.close() }, "关闭"));
}

/* ── Settings ────────────────────────────────────────────────── */

async function renderSettings(view) {
  try {
    const account = await api("/api/settings/account");
    empty(view).append(
      el("h2", null, "设置"),
      el("p", { class: "page-desc" }, "修改面板登录凭据，保存后立即生效并持久化"),
      renderSettingsAccount(account.username),
      renderSettingsPassword(),
    );
  } catch (err) { renderError(view, err); }
}

function renderSettingsAccount(username) {
  return el("div", { class: "card", style: "max-width:480px" },
    el("h3", null, "登录用户名"),
    el("p", { class: "hint" }, `当前用户名：${username}。修改后请使用新用户名重新登录。`),
    el("div", { class: "field" },
      el("label", { for: "set-user" }, "新用户名（字母、数字、_ . -，1-64 字符）"),
      el("input", { id: "set-user", value: username, autocomplete: "username", required: true })),
    el("div", { id: "set-user-err" }),
    el("div", { class: "actions" },
      el("button", { id: "set-user-btn", class: "btn primary", onclick: saveUsername }, "保存用户名")),
  );
}

function renderSettingsPassword() {
  return el("div", { class: "card", style: "max-width:480px" },
    el("h3", null, "登录密码"),
    el("p", { class: "hint" }, "修改成功后所有已登录会话都会退出，需重新登录。"),
    el("div", { class: "field" },
      el("label", { for: "set-pw-current" }, "当前密码"),
      el("input", { id: "set-pw-current", type: "password", autocomplete: "current-password", required: true })),
    el("div", { class: "field" },
      el("label", { for: "set-pw-new" }, "新密码（至少 8 个字符）"),
      el("input", { id: "set-pw-new", type: "password", autocomplete: "new-password", required: true })),
    el("div", { class: "field" },
      el("label", { for: "set-pw-confirm" }, "确认新密码"),
      el("input", { id: "set-pw-confirm", type: "password", autocomplete: "new-password", required: true })),
    el("div", { id: "set-pw-err" }),
    el("div", { class: "actions" },
      el("button", { id: "set-pw-btn", class: "btn primary", onclick: savePassword }, "保存密码")),
  );
}

async function saveUsername() {
  const input = document.getElementById("set-user");
  const errorBox = document.getElementById("set-user-err");
  const btn = document.getElementById("set-user-btn");
  empty(errorBox);
  const username = input.value.trim();
  if (!username) { errorBox.append(el("div", { class: "form-error" }, "用户名不能为空。")); return; }
  btn.disabled = true;
  try {
    await api("/api/settings/account", { method: "POST", body: { username } });
    state.username = username;
    toast("用户名已更新，请重新登录");
    await doLogout();
  } catch (err) {
    errorBox.append(el("div", { class: "form-error" }, err.message));
  } finally { btn.disabled = false; }
}

async function savePassword() {
  const errorBox = document.getElementById("set-pw-err");
  const btn = document.getElementById("set-pw-btn");
  empty(errorBox);
  const current = document.getElementById("set-pw-current").value;
  const next = document.getElementById("set-pw-new").value;
  const confirm = document.getElementById("set-pw-confirm").value;
  if (!current || !next) { errorBox.append(el("div", { class: "form-error" }, "请填写当前密码和新密码。")); return; }
  if (next !== confirm) { errorBox.append(el("div", { class: "form-error" }, "两次输入的新密码不一致。")); return; }
  if (next.length < 8) { errorBox.append(el("div", { class: "form-error" }, "新密码至少需要 8 个字符。")); return; }
  btn.disabled = true;
  try {
    await api("/api/settings/password", { method: "POST", body: { current_password: current, new_password: next } });
    toast("密码已更新，请重新登录");
    await doLogout();
  } catch (err) {
    errorBox.append(el("div", { class: "form-error" }, err.message));
  } finally { btn.disabled = false; }
}

/* ── Modal ───────────────────────────────────────────────────── */

function openModal(title, subtitle) {
  const existing = document.getElementById("modal-root");
  empty(existing);
  const modal = el("div", { class: "modal" },
    el("h3", null, title),
    el("div", { class: "sub hint" }, subtitle || ""),
    el("div", { class: "modal-content" }),
    el("div", { class: "actions" }),
  );
  const backdrop = el("div", { class: "modal-backdrop", onclick: (e) => { if (e.target === backdrop) backdrop.remove(); } }, modal);
  existing.append(backdrop);
  const content = modal.querySelector(".modal-content");
  const actions = modal.querySelector(".actions");
  return {
    content, actions,
    close() { backdrop.remove(); },
  };
}

/* ── Rule preview helpers ────────────────────────────────────── */

function previewRegex(pattern) {
  if (!pattern) return "等待输入…";
  const checks = [
    { re: /\?=/, hint: "RE2 不支持正向前瞻 (?=…)" },
    { re: /\?!/, hint: "RE2 不支持负向前瞻 (?!…)" },
    { re: /\(\?<=/, hint: "RE2 不支持正向后瞻 (?<=…)" },
    { re: /\(\?<!/, hint: "RE2 不支持负向后瞻 (?<!…)" },
    { re: /\\[1-9]/, hint: "RE2 不支持反向引用 \\N" },
    { re: /\(\?P=[a-zA-Z]/, hint: "RE2 不支持命名反向引用 (?P=…)" },
  ];
  for (const c of checks) {
    c.re.lastIndex = 0;
    if (c.re.test(pattern)) return "⚠ " + c.hint;
  }
  return "✓ 将使用 Go RE2 忽略大小写编译：" + pattern;
}

/* ── Errors ──────────────────────────────────────────────────── */

function renderError(view, err) {
  empty(view);
  view.append(el("div", { class: "form-error", style: "max-width:480px" }, err.message),
    err.code === "unauthorized" ? el("button", { class: "btn", style: "margin-top:12px", onclick: () => location.reload() }, "重新登录") : null);
}

/* ── Boot ────────────────────────────────────────────────────── */

window.addEventListener("hashchange", navigate);
boot();