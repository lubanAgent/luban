const test = require("node:test");
const assert = require("node:assert/strict");
const { parseLogs, readConfig, describeEvents, buildRun, displayName, formatValue, tokenUsage, demoLog } = require("./trace-data.js");

function event(spanId, parentSpanId = "", overrides = {}) {
  return {
    version: 1, runId: "run-1", spanId, parentSpanId, type: "function",
    name: spanId, startedAt: "2026-10-14T02:24:30.000Z",
    durationNs: 1500000, status: "success", input: null, output: null, ...overrides,
  };
}

test("parses SDK JSON, zlog wrappers, nested JSON strings and mixed business lines", () => {
  const content = [
    JSON.stringify({ msg: "luban_trace", traceEvent: event("s_1", "", { type: "run" }) }),
    "ordinary business log",
    JSON.stringify({ traceEvent: JSON.stringify(event("s_2", "s_1", {
      input: { arguments: '{"skill":"guide"}' },
      output: "complete Skill content",
    })) }),
    JSON.stringify({ traceEvent: event("old", "", { version: 2 }) }),
    JSON.stringify(event("s_3", "s_1", { runId: "run-2" })),
  ].join("\n");
  const parsed = parseLogs(content);
  assert.equal(parsed.accepted, 3);
  assert.equal(parsed.skipped, 2);
  assert.equal(parsed.groups.size, 2);
  const run = buildRun(parsed.groups.get("run-1"));
  assert.equal(run.roots.length, 1);
  assert.equal(run.roots[0].children[0].event.spanId, "s_2");
  assert.equal(run.roots[0].children[0].event.output, "complete Skill content");
});

test("accepts a pretty JSON array and preserves null, strings, and unavailable markers", () => {
  const source = [
    event("s_1", "", { type: "run", output: null }),
    event("s_2", "s_1", { input: '{"raw":"text"}',
      output: { _traceUnavailable: { type: "chan int", reason: "unsupported type" } } }),
  ];
  const parsed = parseLogs(JSON.stringify(source, null, 2));
  assert.equal(parsed.accepted, 2);
  assert.equal(parsed.skipped, 0);
  assert.equal(formatValue(source[0].output), "null");
  assert.equal(formatValue(source[1].input), '"{\\"raw\\":\\"text\\"}"');
  assert.match(formatValue(source[1].output), /unsupported type/);
  assert.equal(formatValue(undefined), "字段未提供");
});

test("keeps all events visible when parent logs are missing or span IDs collide", () => {
  const events = [
    event("s_1", "", { type: "run" }),
    event("s_2", "lost"),
    event("duplicate", "s_1"),
    event("duplicate", "s_1"),
    event("s_5", "duplicate"),
  ];
  const view = buildRun(events);
  assert.equal(view.entries.length, 5);
  assert.equal(view.roots.length, 3);
  assert.equal(view.missingCount, 1);
  assert.match(view.warnings.join(" "), /重复 spanId/);
  assert.match(view.warnings.join(" "), /无法确定父级/);
  assert.equal(view.entries[1].issue, "父节点日志缺失");
  assert.equal(view.entries[1].parent.missingId, "lost");
  assert.equal(view.entries[4].issue, "父节点 spanId 重复");
});

test("truncated run still exposes the received model and Skill below missing agent spans", () => {
  const view = buildRun([
    event("s_2", "s_1", { name: "prepareTrustee" }),
    event("s_9", "s_8", { type: "agent", name: "luban_flow_3",
      agentName: "luban_flow_3", output: { transferTo: "luban_flow_3_sub_0" } }),
    event("s_13", "s_12", { type: "model", name: "model" }),
    event("s_14", "s_12", { type: "skill", name: "skill",
      input: { arguments: '{"skill":"skill60780"}' } }),
  ]);
  assert.equal(view.run, null);
  assert.equal(view.missingCount, 3);
  assert.equal(view.displayRoots.length, 3);
  assert.deepEqual(view.displayRoots.find(entry => entry.missingId === "s_12")
    .displayChildren.map(entry => displayName(entry.event)), ["模型调用", "加载 Skill · skill60780"]);
});

test("tokens come only from real model responses and mark missing usage", () => {
  const view = buildRun([
    event("s_1", "", { type: "run" }),
    event("s_2", "s_1", { type: "agent", name: "agent" }),
    event("s_3", "s_2", { type: "model", output: {
      usage: { inputTokens: 21, outputTokens: 9 },
    } }),
    event("s_4", "s_2", { type: "model", output: {} }),
    event("s_5", "s_1", { type: "tool", output: {
      usage: { inputTokens: 1000, outputTokens: 1000 },
    } }),
  ]);
  assert.deepEqual(tokenUsage(view.entries), { input: 21, output: 9, known: 1, models: 2 });
  assert.deepEqual(tokenUsage(view.entries, view.entries[1]),
    { input: 21, output: 9, known: 1, models: 2 });
  assert.deepEqual(tokenUsage(view.entries, view.entries[4]),
    { input: 0, output: 0, known: 0, models: 0 });
});

test("older logs gain configured business descriptions without changing technical identity", () => {
  const config = readConfig(JSON.stringify({ trustee: {
    description: "托管智能体",
    flow: [
      { func: "prepareTrustee", description: "准备会话资料" },
      { modelName: "model-A", description: "前置检查", subAgents: [{
        modelName: "model-B", description: "subAgentId=9；门店员工\n负责跟进"
      }] },
    ],
  } }));
  const events = [
    event("s_2", "s_1", { type: "function", name: "prepareTrustee", flowIndex: 0 }),
    event("s_3", "s_1", { type: "agent", name: "model-A", flowIndex: 1 }),
    event("s_4", "s_3", { type: "agent", name: "luban_flow_1", agentName: "luban_flow_1" }),
    event("s_5", "s_4", { type: "agent", name: "luban_flow_1_sub_0", agentName: "luban_flow_1_sub_0" }),
    event("s_6", "s_5", { type: "model", name: "model-B" }),
  ];
  const described = describeEvents(events, config);
  assert.equal(described[0].name, "prepareTrustee");
  assert.equal(displayName(described[0]), "准备会话资料");
  assert.equal(displayName(described[1]), "前置检查");
  assert.equal(displayName(described[2]), "主 Agent · 前置检查");
  assert.equal(displayName(described[3]), "子 Agent · 门店员工");
  assert.equal(displayName(described[4]), "模型调用");
  assert.equal(displayName({ type: "skill", name: "skill", input: { arguments: '{"skill":"skill60778"}' } }),
    "加载 Skill · skill60778");
  assert.throws(() => readConfig('[]'), /Agent key/);
});

test("old transfer logs display selector and child as sequential siblings", () => {
  const old = [
    event("s_1", "", { type: "run", name: "trustee", durationNs: 32000000000 }),
    event("s_8", "s_1", { type: "agent", name: "model", flowIndex: 3, durationNs: 31000000000 }),
    event("s_9", "s_8", { type: "agent", name: "luban_flow_3",
      durationNs: 3800000000, output: { transferTo: "luban_flow_3_sub_0" } }),
    event("s_12", "s_9", { type: "agent", name: "luban_flow_3_sub_0",
      startedAt: "2026-10-14T02:24:33.801Z", durationNs: 27000000000 }),
    event("s_14", "s_12", { type: "skill", name: "skill",
      startedAt: "2026-10-14T02:24:40.000Z", durationNs: 1000000 }),
  ];
  const view = buildRun(old);
  const flow = view.entries[1], selector = view.entries[2], child = view.entries[3], skill = view.entries[4];
  assert.equal(view.legacyTransfers, 1);
  assert.equal(selector.parent, flow);
  assert.equal(child.parent, flow);
  assert.equal(child.transferFromSpanId, selector.event.spanId);
  assert.equal(child.event.parentSpanId, selector.event.spanId); // original log is unchanged
  assert.equal(skill.parent, child);
});

test("new transfer logs already have sibling spans and a separate handoff link", () => {
  const view = buildRun([
    event("s_8", "", { type: "agent", name: "flow", durationNs: 31000000000 }),
    event("s_9", "s_8", { type: "agent", name: "luban_flow_3",
      output: { transferTo: "luban_flow_3_sub_0" } }),
    event("s_12", "s_8", { type: "agent", name: "luban_flow_3_sub_0",
      transferFromSpanId: "s_9", startedAt: "2026-10-14T02:24:33.801Z" }),
  ]);
  assert.equal(view.legacyTransfers, 0);
  assert.equal(view.entries[1].parent, view.entries[2].parent);
  assert.equal(view.entries[2].transferFromSpanId, "s_9");
});

test("single-agent flow shows one row while retaining the adapter event in detail", () => {
  const view = buildRun([
    event("s_1", "", { type: "run", name: "task" }),
    event("s_3", "s_1", { type: "agent", name: "model", flowIndex: 1, durationNs: 2000000000 }),
    event("s_4", "s_3", { type: "agent", name: "luban_flow_1", agentName: "luban_flow_1" }),
    event("s_5", "s_4", { type: "model", name: "model" }),
  ]);
  assert.equal(view.entries.length, 4);
  assert.equal(view.displayEntries.length, 3);
  assert.equal(view.displayRoots[0].displayChildren.length, 1);
  assert.equal(view.displayRoots[0].displayChildren[0].event.spanId, "s_4");
  assert.equal(view.displayRoots[0].displayChildren[0].flowWrapper.event.spanId, "s_3");
  assert.equal(view.displayRoots[0].displayChildren[0].displayChildren[0].event.spanId, "s_5");
  assert.equal(view.entries[2].event.parentSpanId, "s_3");
});

test("selector and sub Agent render as siblings with their own model, Skill and Tool", () => {
  const view = buildRun([
    event("s_1", "", { type: "run", name: "trustee" }),
    event("s_8", "s_1", { type: "agent", name: "route-model", flowIndex: 3,
      durationNs: 32000000000 }),
    event("s_9", "s_8", { type: "agent", name: "luban_flow_3", agentName: "luban_flow_3",
      output: { transferTo: "luban_flow_3_sub_0" } }),
    event("s_10", "s_9", { type: "model", name: "route-model" }),
    event("s_12", "s_9", { type: "agent", name: "luban_flow_3_sub_0",
      agentName: "luban_flow_3_sub_0", startedAt: "2026-10-14T02:24:30.002Z" }),
    event("s_13", "s_12", { type: "model", name: "sub-model" }),
    event("s_14", "s_12", { type: "skill", name: "skill" }),
    event("s_16", "s_12", { type: "tool", name: "send_wx_msg" }),
  ]);
  const [selector, child] = view.displayRoots[0].displayChildren;
  assert.deepEqual([selector.event.spanId, child.event.spanId], ["s_9", "s_12"]);
  assert.equal(selector.flowWrapper.event.spanId, "s_8");
  assert.equal(child.flowWrapper.event.spanId, "s_8");
  assert.deepEqual(child.displayChildren.map(entry => entry.event.type), ["model", "skill", "tool"]);
  assert.equal(child.transferFromSpanId, "s_9");
  assert.equal(view.entries.length, 8);
  assert.equal(view.displayEntries.length, 7);
});

test("failed setup or mixed child kinds keep the outer flow row visible", () => {
  const failed = buildRun([event("s_1", "", { type: "run" }),
    event("s_2", "s_1", { type: "agent", flowIndex: 1, status: "error" })]);
  assert.equal(failed.displayEntries.length, 2);
  const batch = buildRun([event("s_1", "", { type: "run" }),
    event("s_2", "s_1", { type: "agent", flowIndex: 1 }),
    event("s_3", "s_2", { type: "batchItem" })]);
  assert.equal(batch.displayEntries.length, 3);
});

test("breaks cyclic links without inventing a Run root", () => {
  const view = buildRun([event("a", "b"), event("b", "a")]);
  assert.equal(view.entries.length, 2);
  assert.equal(view.roots.length, 1);
  assert.match(view.warnings.join(" "), /Run 根事件缺失/);
  assert.ok(view.entries.some(entry => entry.issue.includes("循环")));
});

test("demo is explicitly synthetic and forms a navigable parent-child tree", () => {
  const parsed = parseLogs(demoLog());
  assert.equal(parsed.accepted, 12);
  const view = buildRun([...parsed.groups.values()][0]);
  assert.deepEqual(view.warnings, []);
  const child = view.entries.find(entry => entry.event.name === "luban_flow_3_sub_0");
  assert.equal(child.parent.event.name, "route-model");
  assert.equal(child.transferFromSpanId, "s_7");
  assert.equal(view.displayRoots[0].displayChildren.find(entry => entry.event.spanId === "s_7").flowWrapper.event.spanId, "s_6");
  assert.equal(view.displayRoots[0].displayChildren.find(entry => entry.event.spanId === "s_9").flowWrapper.event.spanId, "s_6");
  assert.equal(view.displayEntries.length, 10);
});
