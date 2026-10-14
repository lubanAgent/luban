(() => {
  "use strict";
  const data = globalThis.LubanTraceData;
  const byId = id => document.getElementById(id);
  const state = { groups: new Map(), selectedRun: "", selected: null, collapsed: new Set(),
    demo: false, skipped: 0, mode: "tree", detailTab: "overview" };
  const typeNames = {
    run: "Run", function: "业务函数", agent: "Agent", model: "模型调用",
    tool: "Tool", skill: "Skill", batchItem: "Batch 项",
  };
  const statusNames = { success: "成功", stopped: "已停止", error: "失败" };
  const glyphs = { run: "R", function: "ƒ", agent: "A", model: "M", tool: "T", skill: "S", batchItem: "B" };

  function element(tag, text, className) {
    const node = document.createElement(tag);
    if (text !== undefined) node.textContent = String(text);
    if (className) node.className = className;
    return node;
  }
  function duration(ms) {
    if (!Number.isFinite(ms)) return "未知";
    if (ms >= 1000) return `${(ms / 1000).toFixed(2)} s`;
    if (ms >= 1) return `${ms.toFixed(1)} ms`;
    return `${ms.toFixed(3)} ms`;
  }
  function time(value) {
    return new Date(value).toLocaleString("zh-CN", { hour12: false });
  }
  function message(text, error = false) {
    const target = byId("importMessage");
    target.textContent = text;
    target.classList.toggle("error", error);
  }
  function current() {
    return state.groups.get(state.selectedRun);
  }
  function parse(demo = false) {
    const raw = byId("logInput").value;
    if (!raw.trim()) { message("先粘贴日志，或点击“查看演示数据”。", true); return; }
    const result = data.parseLogs(raw);
    if (!result.accepted) {
      message(`没有找到 version=1 的有效 Trace 事件；检查 JSON 格式与 traceEvent 字段。已检查 ${result.skipped} 行。`, true);
      return;
    }
    let config = null;
    try {
      config = data.readConfig(byId("configInput").value);
    } catch (error) {
      message(`配置 JSON 无法解析：${error.message}；Trace 日志仍保留在输入框中。`, true);
      return;
    }
    state.groups = new Map([...result.groups].map(([id, events]) =>
      [id, data.buildRun(data.describeEvents(events, config))]));
    const ordered = [...state.groups].sort((a, b) => b[1].start - a[1].start);
    state.groups = new Map(ordered);
    state.selectedRun = ordered[0][0];
    state.selected = null;
    state.collapsed.clear();
    state.demo = demo;
    state.skipped = result.skipped;
    byId("workspace").hidden = false;
    byId("welcome").hidden = true;
    byId("importPanel").hidden = true;
    byId("toggleImport").setAttribute("aria-expanded", "false");
    message(`${result.accepted} 条事件 · ${state.groups.size} 次 Run · 跳过 ${result.skipped} 行非 Trace/无效日志`);
    render();
  }
  function clear() {
    byId("logInput").value = "";
    byId("configInput").value = "";
    byId("logFile").value = "";
    byId("configFile").value = "";
    byId("searchInput").value = "";
    state.groups.clear();
    state.selectedRun = "";
    state.selected = null;
    state.collapsed.clear();
    state.demo = false;
    byId("welcome").hidden = false;
    byId("workspace").hidden = true;
    byId("importPanel").hidden = false;
    byId("toggleImport").setAttribute("aria-expanded", "true");
    message("内容已从页面内存清空。");
  }
  function demo() {
    byId("logInput").value = data.demoLog();
    parse(true);
  }
  function renderRuns() {
    const list = byId("runsList");
    list.replaceChildren();
    byId("runCount").textContent = `${state.groups.size} 次`;
    for (const [id, view] of state.groups) {
      const button = element("button", undefined, `run-button${id === state.selectedRun ? " active" : ""}`);
      button.type = "button";
      button.title = id;
      button.setAttribute("aria-current", id === state.selectedRun ? "true" : "false");
      const row = element("div", undefined, "run-line");
      row.append(element("span", "", `dot ${view.run?.event.status || ""}`),
        element("span", view.run ? data.displayName(view.run.event) : "Run 根事件缺失"));
      button.append(row, element("div", `${view.entries.length} 节点 · ${view.run ? duration(view.run.event.durationNs / 1e6) : "耗时未知"}`, "run-meta"));
      button.addEventListener("click", () => {
        state.selectedRun = id; state.selected = null; state.collapsed.clear(); byId("searchInput").value = "";
        render();
      });
      list.append(button);
    }
  }
  function metric(label, value, className = "") {
    const card = element("div", undefined, "metric");
    card.append(element("small", label), element("strong", value, className));
    return card;
  }
  function count(value) { return value.toLocaleString("zh-CN"); }
  function tokenText(usage) {
    if (!usage.known) return "未提供";
    return `${count(usage.input + usage.output)}${usage.known < usage.models ? "（部分）" : ""}`;
  }
  function renderOverview(view) {
    const root = view.run?.event;
    byId("runSubtitle").textContent = root
      ? `开始于 ${time(root.startedAt)} · ${view.entries.length} 条已收到的执行事件`
      : `${view.entries.length} 条事件；Run 根事件缺失，无法确认整体状态与耗时`;
    const tag = byId("runTag");
    tag.textContent = `${state.demo ? "演示数据 · " : ""}${state.selectedRun}`;
    tag.classList.toggle("demo", state.demo);
    const metrics = byId("metrics");
    metrics.replaceChildren(
      metric("运行状态", root ? statusNames[root.status] : "未知", `status ${root?.status || ""}`),
      metric("总耗时", root ? duration(root.durationNs / 1e6) : "未知"),
      metric("模型调用", view.entries.filter(entry => entry.event.type === "model").length),
      metric("Token", tokenText(data.tokenUsage(view.entries))),
      metric("已收到节点", view.entries.length),
    );
    const notice = byId("notice");
    notice.replaceChildren();
    if (view.warnings.length) {
      notice.className = "notice";
      if (!root) {
        const last = view.entries[view.entries.length - 1]?.event;
        notice.append(element("strong",
          `本次导入只有 ${view.entries.length} 条已完成节点的事件，未收到 Run 结束事件。${last ? `最后一条是“${data.displayName(last)}”。` : ""}后续 Tool 或收尾节点尚无日志，无法展示完整流程。请用同一 runId 导入执行结束后的完整日志。`));
      } else {
        notice.append(element("strong", "部分父节点日志缺失；虚线层级仅标示缺失的 parentSpanId，不代表节点已经结束："));
      }
      const list = element("ul");
      for (const warning of view.warnings) list.append(element("li", warning));
      notice.append(list);
    } else {
      notice.className = "notice info";
      notice.textContent = "已收到的节点可以按父子关系重建；未写入日志的事件无法从页面推断。";
    }
    if (view.legacyTransfers) {
      notice.append(element("p",
        `${view.legacyTransfers} 处旧日志把子 Agent 记在已结束的选择 Agent 下；依据 transferTo 和时间顺序按同级展示。详情仍保留原始 parentSpanId。`,
        "legacy-note"));
    }
    byId("nodeCount").textContent = `调用链 · ${view.displayEntries.length} 个节点`;
    const ticks = byId("ticks");
    ticks.replaceChildren(element("span", "0"), element("span", duration(view.duration / 2)),
      element("span", duration(view.duration)));
  }
  function renderTree(view) {
    const target = byId("tree");
    target.replaceChildren();
    target.classList.toggle("timeline-mode", state.mode === "timeline");
    byId("treeMode").classList.toggle("active", state.mode === "tree");
    byId("timelineMode").classList.toggle("active", state.mode === "timeline");
    const query = byId("searchInput").value.trim().toLowerCase();
    const matches = entry => `${entry.event.name} ${entry.event.description || ""} ${data.displayName(entry.event)} ${entry.event.type} ${typeNames[entry.event.type] || ""} ${entry.event.agentName || ""}`
      .toLowerCase().includes(query);
    const childrenOf = entry => entry.displayChildren || entry.children;
    const containsMatch = entry => matches(entry) ||
      (entry.flowWrapper && matches(entry.flowWrapper)) ||
      childrenOf(entry).some(containsMatch);
    let drawn = 0;
    function draw(entry, depth, matchingParent = false) {
      if (query && !matchingParent && !containsMatch(entry)) return;
      drawn++;
      const event = entry.event;
      const row = element("div", undefined, `node-row kind-${event.type}${entry.missing ? " missing" : ""}${entry.transferFromSpanId ? " transferred" : ""}${state.selected === entry ? " selected" : ""}`);
      row.setAttribute("role", "treeitem");
      row.setAttribute("aria-level", String(depth + 1));
      row.setAttribute("aria-selected", String(state.selected === entry));
      row.tabIndex = 0;
      const info = element("div", undefined, "node-info");
      info.style.setProperty("--depth", String(Math.min(depth, 12)));
      for (let level = 0; level < Math.min(depth, 12); level++) {
        const guide = element("span", undefined, `branch-guide${level === depth - 1 ? " last" : ""}`);
        guide.style.left = `${10 + level * 18}px`;
        info.append(guide);
      }
      const children = childrenOf(entry);
      const expand = element("button", children.length ? (state.collapsed.has(entry.key) && !query ? "▸" : "▾") : "▾",
        `expand${children.length ? "" : " blank"}`);
      expand.type = "button";
      expand.tabIndex = children.length ? 0 : -1;
      expand.setAttribute("aria-label", `${state.collapsed.has(entry.key) ? "展开" : "收起"} ${event.name}`);
      expand.setAttribute("aria-expanded", String(!state.collapsed.has(entry.key)));
      expand.addEventListener("click", e => {
        e.stopPropagation();
        if (state.collapsed.has(entry.key)) state.collapsed.delete(entry.key); else state.collapsed.add(entry.key);
        renderTree(view);
      });
      const label = element("span", undefined, "node-label");
      const sub = entry.missing ? "尚未收到节点结束事件" :
        `${typeNames[event.type] || event.type} · ${statusNames[event.status] || event.status || "未知"}`;
      label.append(element("span", data.displayName(event) || "(未命名)", "node-name"),
        element("span", sub, "node-sub"));
      info.append(expand, element("span", glyphs[event.type] || "·", `glyph ${event.type}`), label);
      const stats = element("span", undefined, "node-stats");
      stats.append(element("span", entry.missing ? "—" : duration(event.durationNs / 1e6), "node-duration"));
      if (!entry.missing && ["run", "agent", "model"].includes(event.type)) {
        const usage = data.tokenUsage(view.entries, entry);
        if (usage.known) stats.append(element("span", `${tokenText(usage)} tk`, "node-tokens"));
      }
      const lane = element("div", undefined, "lane");
      const bar = element("span", undefined, `bar ${event.type} ${event.status}`);
      if (!entry.missing) {
        const startPercent = Math.min(100, Math.max(0, (Date.parse(event.startedAt) - view.start) / view.duration * 100));
        const widthPercent = Math.min(100 - startPercent, event.durationNs / 1e6 / view.duration * 100);
        bar.style.left = `${startPercent}%`;
        bar.style.width = `${widthPercent}%`;
        lane.append(bar);
      }
      row.append(info, stats, lane);
      row.title = entry.missing ? `日志未包含 ${entry.missingId}` :
        `${event.description ? `${event.description}\n` : ""}${event.name} · ${event.spanId} · ${time(event.startedAt)} · ${statusNames[event.status]}`;
      row.addEventListener("click", () => {
        if (entry.missing) {
          if (state.collapsed.has(entry.key)) state.collapsed.delete(entry.key);
          else state.collapsed.add(entry.key);
          renderTree(view);
          return;
        }
        state.selected = entry; state.detailTab = "overview"; renderTree(view); renderDetail(entry);
      });
      row.addEventListener("keydown", e => {
        if (e.target !== row) return;
        if (e.key === "Enter" || e.key === " ") { e.preventDefault(); row.click(); }
        if (e.key === "ArrowRight" && children.length) { state.collapsed.delete(entry.key); renderTree(view); }
        if (e.key === "ArrowLeft" && children.length) { state.collapsed.add(entry.key); renderTree(view); }
      });
      target.append(row);
      if (query || !state.collapsed.has(entry.key)) {
        for (const child of children) draw(child, depth + 1,
          matchingParent || (query && (matches(entry) || (entry.flowWrapper && matches(entry.flowWrapper)))));
      }
    }
    for (const root of (view.displayRoots || view.roots)) draw(root, 0);
    if (!drawn) target.append(element("p", "没有匹配的节点。", "tree-empty"));
  }
  function section(title, content, extraClass = "") {
    const node = element("section", undefined, "detail-section");
    node.append(element("h3", title), element("pre", content, `code-box ${extraClass}`));
    return node;
  }
  function renderDetail(entry) {
    const target = byId("detail");
    target.replaceChildren();
    if (!entry) {
      target.append(element("p", "选择左侧节点，查看执行详情与完整输入输出。", "detail-placeholder"));
      return;
    }
    const event = entry.event;
    const view = current();
    const head = element("div", undefined, "detail-head");
    head.append(element("small", typeNames[event.type] || event.type),
      element("h2", data.displayName(event) || "(未命名)"));
    if (event.description) head.append(element("p", event.description, "detail-description"));
    const pills = element("div", undefined, "pills");
    pills.append(element("span", `状态 ${statusNames[event.status] || "未知"}`, `pill ${event.status}`),
      element("span", `耗时 ${duration(event.durationNs / 1e6)}`, "pill"));
    if (event.type === "model") pills.append(element("span", `模型 ${event.name}`, "pill"));
    const usage = data.tokenUsage(view.entries, entry);
    if (event.type === "model" || event.type === "agent" || event.type === "run") {
      pills.append(element("span", `Token ${tokenText(usage)}`, "pill"));
    }
    head.append(pills);
    const tabs = element("nav", undefined, "detail-tabs");
    tabs.setAttribute("aria-label", "节点详情");
    for (const [key, title] of [["overview", "概览"], ["input", "输入"], ["output", "输出"]]) {
      const button = element("button", title, state.detailTab === key ? "active" : "");
      button.type = "button";
      button.addEventListener("click", () => { state.detailTab = key; renderDetail(entry); });
      tabs.append(button);
    }
    const scroll = element("div", undefined, "detail-scroll");
    if (state.detailTab === "overview") {
      if (event.error) scroll.append(section("执行错误", event.error, "error-box"));
      if (event.type === "model") {
        const tokenLine = element("div", undefined, "token-summary");
        tokenLine.append(metric("输入 Token", usage.known ? count(usage.input) : "未提供"),
          metric("输出 Token", usage.known ? count(usage.output) : "未提供"));
        scroll.append(tokenLine);
      }
      const summary = element("p", event.description || `${typeNames[event.type] || event.type}：${event.name}`, "detail-summary");
      scroll.append(summary);
      const preview = event.type === "model" && event.output?.ToolCalls?.length
        ? `调用工具：${event.output.ToolCalls.map(call => call.Name).join("、")}`
        : event.type === "skill" ? `加载：${data.displayName(event)}` : null;
      if (preview) scroll.append(element("p", preview, "detail-preview"));
      if (entry.transferFromSpanId) scroll.append(element("p", `由 ${entry.transferFromSpanId} 移交`, "detail-preview"));
      const details = element("details", undefined, "raw-details");
      details.append(element("summary", "技术信息"),
        element("p", `原始名称：${event.name} · Span：${event.spanId} · 父级：${event.parentSpanId || "根节点"}`));
      if (entry.flowWrapper) details.append(element("p", `外层步骤：${entry.flowWrapper.event.spanId}`));
      scroll.append(details);
    } else {
      const value = state.detailTab === "input" ? event.input : event.output;
      scroll.append(section(state.detailTab === "input" ? "完整输入" : "完整输出", data.formatValue(value)));
      if (value && typeof value === "object" && value._traceUnavailable)
        scroll.append(element("p", "该值无法序列化；上方保留 SDK 提供的类型与原因。", "data-note"));
    }
    target.append(head, tabs, scroll);
  }
  function render() {
    const view = current();
    if (!view) return;
    renderRuns(); renderOverview(view);
    if (!state.selected || !view.displayEntries.includes(state.selected))
      state.selected = view.run || view.displayEntries[0] || null;
    renderTree(view);
    renderDetail(state.selected);
  }
  byId("toggleImport").addEventListener("click", () => {
    const panel = byId("importPanel");
    panel.hidden = !panel.hidden;
    byId("toggleImport").setAttribute("aria-expanded", String(!panel.hidden));
  });
  byId("parseButton").addEventListener("click", () => parse(false));
  byId("demoButton").addEventListener("click", demo);
  byId("welcomeDemo").addEventListener("click", demo);
  byId("clearButton").addEventListener("click", clear);
  byId("searchInput").addEventListener("input", () => { if (current()) renderTree(current()); });
  byId("treeMode").addEventListener("click", () => { state.mode = "tree"; if (current()) renderTree(current()); });
  byId("timelineMode").addEventListener("click", () => { state.mode = "timeline"; if (current()) renderTree(current()); });
  byId("expandButton").addEventListener("click", () => { state.collapsed.clear(); if (current()) renderTree(current()); });
  byId("collapseButton").addEventListener("click", () => {
    const view = current();
    if (!view) return;
    state.collapsed = new Set(view.displayEntries.filter(entry => (entry.displayChildren || entry.children).length).map(entry => entry.key));
    renderTree(view);
  });
  byId("logFile").addEventListener("change", async event => {
    const file = event.target.files?.[0];
    if (!file) return;
    try {
      byId("logInput").value = await file.text();
      parse(false);
    } catch (error) {
      message(`读取本地文件失败：${error.message}`, true);
    }
  });
  byId("configFile").addEventListener("change", async event => {
    const file = event.target.files?.[0];
    if (!file) return;
    try {
      byId("configInput").value = await file.text();
      message("配置 JSON 已在本机读取，点击“解析 Trace”以显示业务描述。");
    } catch (error) {
      message(`读取配置文件失败：${error.message}`, true);
    }
  });
})();
