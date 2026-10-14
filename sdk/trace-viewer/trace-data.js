/* Shared by the offline page and its Node tests. No network or browser APIs. */
(function (root, create) {
  const api = create();
  if (typeof module === "object" && module.exports) module.exports = api;
  else root.LubanTraceData = api;
})(globalThis, function () {
  "use strict";

  const statuses = new Set(["success", "error", "stopped"]);

  function extractEvents(value) {
    if (Array.isArray(value)) return value.flatMap(extractEvents);
    if (!value || typeof value !== "object") return [];
    let event = Object.prototype.hasOwnProperty.call(value, "traceEvent")
      ? value.traceEvent : value;
    if (typeof event === "string") {
      try { event = JSON.parse(event); } catch { return []; }
    }
    return Array.isArray(event) ? event.flatMap(extractEvents) : [event];
  }

  function isTraceEvent(event) {
    return event && typeof event === "object" && event.version === 1 &&
      typeof event.runId === "string" && event.runId.length > 0 &&
      typeof event.spanId === "string" && event.spanId.length > 0 &&
      typeof event.type === "string" && event.type.length > 0 &&
      typeof event.name === "string" &&
      typeof event.startedAt === "string" &&
      Number.isFinite(Date.parse(event.startedAt)) &&
      typeof event.durationNs === "number" && Number.isFinite(event.durationNs) &&
      event.durationNs >= 0 &&
      typeof event.status === "string" && statuses.has(event.status) &&
      (event.parentSpanId == null || typeof event.parentSpanId === "string");
  }

  function parseLogs(text) {
    const groups = new Map();
    let skipped = 0;
    let accepted = 0;
    const lines = String(text).split(/\r?\n/).filter(line => line.trim());
    function add(value) {
      let count = 0;
      for (const event of extractEvents(value)) {
        if (!isTraceEvent(event)) continue;
        if (!groups.has(event.runId)) groups.set(event.runId, []);
        groups.get(event.runId).push(event);
        count++;
        accepted++;
      }
      return count;
    }
    for (const line of lines) {
      try {
        if (!add(JSON.parse(line))) skipped++;
      } catch {
        skipped++;
      }
    }
    // Accept a pretty-printed JSON document or array as well as NDJSON.
    if (!accepted && lines.length > 1) {
      try {
        const value = JSON.parse(String(text));
        if (add(value)) skipped = 0;
      } catch { /* The per-line failure count remains accurate. */ }
    }
    return { groups, accepted, skipped };
  }

  function readConfig(text) {
    if (!String(text).trim()) return null;
    const config = JSON.parse(String(text));
    if (!config || typeof config !== "object" || !config.agents ||
        typeof config.agents !== "object" || Array.isArray(config.agents))
      throw new Error("配置需要包含 agents 对象");
    return config.agents;
  }

  // Older logs predate TraceEvent.description. Resolve only configured node
  // identities; do not infer descriptions for arbitrary model/Tool events.
  function describeEvents(events, agents) {
    if (!agents) return events;
    const roots = events.filter(event => event.type === "run" && !event.parentSpanId);
    const keys = Object.keys(agents);
    const appKey = roots.length === 1 && agents[roots[0].name]
      ? roots[0].name : keys.length === 1 ? keys[0] : null;
    const config = appKey && agents[appKey];
    if (!config || !Array.isArray(config.flow)) return events;
    return events.map(event => {
      if (event.description) return event;
      let description = "";
      if (event.type === "run") description = config.description || "";
      else if ((event.type === "function" || event.type === "agent") &&
          Number.isInteger(event.flowIndex)) {
        const node = config.flow[event.flowIndex];
        if (node && (event.type !== "function" || node.func === event.name) &&
            (event.type !== "agent" || node.modelName === event.name))
          description = node.description || "";
      } else if (event.type === "agent" && event.agentName === event.name) {
        const match = /^luban_flow_(\d+)(?:_sub_(\d+))?$/.exec(event.name);
        if (match) {
          const node = config.flow[Number(match[1])];
          if (node) {
            const child = match[2] === undefined ? null : node.subAgents?.[Number(match[2])];
            description = child ? child.description || "" : match[2] === undefined ? node.description || "" : "";
          }
        }
      }
      return description ? { ...event, description } : event;
    });
  }

  function buildRun(events) {
    const entries = events.map((event, index) => ({
      event, key: `entry-${index}`, sourceIndex: index, parent: null,
      children: [], issue: "", transferFromSpanId: event.transferFromSpanId || "",
    }));
    const bySpanId = new Map();
    for (const entry of entries) {
      const id = entry.event.spanId;
      if (!bySpanId.has(id)) bySpanId.set(id, []);
      bySpanId.get(id).push(entry);
    }
    const warnings = [];
    const duplicateIds = [...bySpanId].filter(([, matches]) => matches.length > 1);
    if (duplicateIds.length) warnings.push(`重复 spanId：${duplicateIds.length} 个；相关父子关系无法确定`);
    const missing = new Map();
    for (const entry of entries) {
      const id = entry.event.parentSpanId;
      if (!id) continue;
      const candidates = bySpanId.get(id) || [];
      if (!candidates.length) {
        entry.issue = "父节点日志缺失";
        if (!missing.has(id)) {
          missing.set(id, {
            key: `missing-${id}`, missing: true, missingId: id, children: [], parent: null,
            event: { name: `未收到父节点 ${id}`, type: "missing" },
            sourceIndex: entry.sourceIndex,
          });
        }
        entry.parent = missing.get(id);
        missing.get(id).sourceIndex = Math.min(missing.get(id).sourceIndex, entry.sourceIndex);
      } else if (candidates.length !== 1) {
        entry.issue = "父节点 spanId 重复";
      } else if (candidates[0] === entry) {
        entry.issue = "节点指向自身";
      } else {
        entry.parent = candidates[0];
      }
    }
    // Older SDK logs put the transferred child under a selector span that had
    // already ended. Only move a child when the recorded transfer target and
    // timing both confirm the handoff. Preserve the raw parentSpanId in detail.
    let legacyTransfers = 0;
    for (const entry of entries) {
      const selector = entry.parent;
      if (entry.event.type !== "agent" || !selector || selector.missing ||
          selector.event.type !== "agent" || !selector.parent ||
          selector.event.output?.transferTo !== entry.event.name) continue;
      const selectorEnd = Date.parse(selector.event.startedAt) + selector.event.durationNs / 1e6;
      const childStart = Date.parse(entry.event.startedAt);
      if (childStart + 5 < selectorEnd) continue;
      entry.transferFromSpanId = selector.event.spanId;
      entry.displayParentSpanId = selector.parent.event.spanId || selector.parent.missingId;
      entry.parent = selector.parent;
      legacyTransfers++;
    }
    // Break only the invalid edge, keeping every original event visible.
    for (const entry of entries) {
      if (!entry.parent) continue;
      const seen = new Set([entry]);
      let cursor = entry.parent;
      while (cursor && !seen.has(cursor)) {
        seen.add(cursor);
        cursor = cursor.parent;
      }
      if (cursor) {
        entry.parent = null;
        entry.issue = "父子关系存在循环";
      }
    }
    for (const entry of entries) {
      if (entry.parent) entry.parent.children.push(entry);
    }
    for (const placeholder of missing.values()) {
      placeholder.event.startedAt = placeholder.children
        .map(child => child.event.startedAt).sort((a, b) => Date.parse(a) - Date.parse(b))[0];
    }
    const compare = (a, b) => Date.parse(a.event.startedAt) - Date.parse(b.event.startedAt) ||
      a.sourceIndex - b.sourceIndex;
    for (const entry of [...entries, ...missing.values()]) entry.children.sort(compare);
    const roots = [...entries.filter(entry => !entry.parent), ...missing.values()].sort(compare);
    const runEntries = entries.filter(entry => entry.event.type === "run" && !entry.event.parentSpanId);
    const run = runEntries.length === 1 ? runEntries[0] : null;
    if (!run) warnings.push(runEntries.length ? "存在多个 Run 根事件" : "Run 根事件缺失，日志可能被截断");
    if (roots.length !== 1) warnings.push(`可见根层级：${roots.length} 组，含 ${missing.size} 个未收到的父节点占位`);
    const detached = entries.filter(entry => entry.issue);
    if (detached.length) warnings.push(`${detached.length} 个节点无法确定父级`);
    const start = Math.min(...entries.map(entry => Date.parse(entry.event.startedAt)));
    const end = Math.max(...entries.map(entry =>
      Date.parse(entry.event.startedAt) + entry.event.durationNs / 1e6));
    // A flow Agent span is the SDK adapter around the actual ChatModelAgent(s).
    // Keep the event in entries for exact timing/I/O, but avoid rendering an
    // identical extra row when all its children are real Agent invocations.
    // Error spans remain visible so a failed setup is never hidden.
    const collapsibleFlow = entry => !entry.missing &&
      entry.event.type === "agent" && Number.isInteger(entry.event.flowIndex) &&
      !entry.event.agentName && entry.event.status === "success" &&
      entry.children.length > 0 &&
      entry.children.every(child => child.event.type === "agent" && child.event.agentName);
    const promote = children => children.flatMap(entry => {
      if (!collapsibleFlow(entry)) return [entry];
      for (const child of entry.children) child.flowWrapper = entry;
      return entry.children;
    });
    for (const entry of [...entries, ...missing.values()]) {
      entry.displayChildren = promote(entry.children);
    }
    const displayRoots = promote(roots);
    const displayEntries = entries.filter(entry => !collapsibleFlow(entry));
    return { entries, roots, displayEntries, displayRoots, run, warnings, start, end, missingCount: missing.size, legacyTransfers,
      duration: Math.max(1, end - start) };
  }

  function displayName(event) {
    if (event.type === "missing") return event.name;
    if (event.type === "skill" && event.name === "skill") {
      try {
        const args = typeof event.input?.arguments === "string"
          ? JSON.parse(event.input.arguments) : event.input?.arguments;
        if (args && typeof args.skill === "string" && args.skill)
          return `加载 Skill · ${args.skill}`;
      } catch { /* Keep the original name. */ }
    }
    if (event.description) {
      const title = event.description.split(/\r?\n/)[0]
        .replace(/^subAgentId=\d+\s*[；;]\s*/, "").trim() || event.name;
      if (event.type === "agent" && event.agentName === event.name) {
        if (/^luban_flow_\d+_sub_\d+$/.test(event.name)) return `子 Agent · ${title}`;
        if (/^luban_flow_\d+$/.test(event.name)) return `主 Agent · ${title}`;
      }
      return title;
    }
    if (event.type === "model") return "模型调用";
    return event.name;
  }

  function formatValue(value) {
    if (value === undefined) return "字段未提供";
    return JSON.stringify(value, null, 2) ?? String(value);
  }

  function demoLog() {
    const runId = "run_demo_仅供界面预览";
    const at = ms => new Date(Date.UTC(2026, 9, 14, 2, 24, 30, ms)).toISOString();
    const event = (spanId, parentSpanId, type, name, startMs, durationMs, input, output, extra = {}) => ({
      version: 1, runId, spanId, parentSpanId, type, name, startedAt: at(startMs),
      durationNs: durationMs * 1e6, status: "success", input, output, ...extra,
    });
    return [
      event("s_1", "", "run", "trustee", 0, 890, { messageId: "demo-42" }, { result: "演示结束" }),
      event("s_2", "s_1", "function", "prepareTrustee", 8, 76, { messageId: "demo-42" }, { messages: ["你好"] }, { flowIndex: 0 }),
      event("s_3", "s_1", "agent", "precheck-model", 99, 138, { messages: ["你好"] }, { isStop: false }, { flowIndex: 1 }),
      event("s_4", "s_3", "agent", "luban_flow_1", 103, 127, { instruction: "示例前置检查" }, { isStop: false }, { agentName: "luban_flow_1" }),
      event("s_5", "s_4", "model", "precheck-model", 115, 94, { Messages: ["你好"] }, { ToolCalls: [{ Name: "__runtime_output" }] }, { agentName: "luban_flow_1" }),
      event("s_6", "s_1", "agent", "route-model", 251, 515, { messages: ["你好"] }, "子 Agent 完成", { flowIndex: 3 }),
      event("s_7", "s_6", "agent", "luban_flow_3", 260, 132, { instruction: "选择子 Agent" }, { transferTo: "luban_flow_3_sub_0" }, { agentName: "luban_flow_3" }),
      event("s_8", "s_7", "model", "route-model", 270, 83, { Messages: ["你好"] }, { ToolCalls: [{ Name: "transfer_to_agent" }] }, { agentName: "luban_flow_3" }),
      event("s_9", "s_6", "agent", "luban_flow_3_sub_0", 404, 350, { instruction: "处理咨询" }, "子 Agent 完成", { agentName: "luban_flow_3_sub_0", transferFromSpanId: "s_7" }),
      event("s_10", "s_9", "skill", "skill_demo", 437, 35, { arguments: '{"skill":"skill_demo"}' }, "仅供预览的 Skill 内容", { agentName: "luban_flow_3_sub_0" }),
      event("s_11", "s_9", "tool", "send_wx_msg", 512, 28, { arguments: '{"messages":[]}' }, "候选已记录，未发送", { agentName: "luban_flow_3_sub_0" }),
      event("s_12", "s_1", "function", "finalizeTrustee", 788, 76, { candidate: "演示内容" }, { saved: true }, { flowIndex: 6 }),
    ].map(item => JSON.stringify({ msg: "luban_trace", runId, traceEvent: item })).join("\n");
  }

  return { parseLogs, readConfig, describeEvents, buildRun, displayName, formatValue, demoLog };
});
