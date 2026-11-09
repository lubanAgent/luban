-- Offline test/example schema only; production uses sql/trace/*.mysql.sql.
CREATE TABLE tblLubanTraceRun (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    service TEXT NOT NULL,
    agent_key TEXT NOT NULL,
    business_type TEXT NOT NULL DEFAULT '',
    business_id TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    error_msg TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_run_run_id ON tblLubanTraceRun(run_id);
CREATE INDEX idx_trace_run_agent_time ON tblLubanTraceRun(service, agent_key, started_at, run_id);
CREATE INDEX idx_trace_run_business ON tblLubanTraceRun(service, business_type, business_id, started_at);
CREATE INDEX idx_trace_run_time ON tblLubanTraceRun(started_at, run_id);

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan0 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity0 ON tblLubanTraceSpan0(run_id, span_id);
CREATE INDEX idx_trace_span_time0 ON tblLubanTraceSpan0(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan1 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity1 ON tblLubanTraceSpan1(run_id, span_id);
CREATE INDEX idx_trace_span_time1 ON tblLubanTraceSpan1(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan2 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity2 ON tblLubanTraceSpan2(run_id, span_id);
CREATE INDEX idx_trace_span_time2 ON tblLubanTraceSpan2(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan3 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity3 ON tblLubanTraceSpan3(run_id, span_id);
CREATE INDEX idx_trace_span_time3 ON tblLubanTraceSpan3(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan4 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity4 ON tblLubanTraceSpan4(run_id, span_id);
CREATE INDEX idx_trace_span_time4 ON tblLubanTraceSpan4(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan5 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity5 ON tblLubanTraceSpan5(run_id, span_id);
CREATE INDEX idx_trace_span_time5 ON tblLubanTraceSpan5(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan6 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity6 ON tblLubanTraceSpan6(run_id, span_id);
CREATE INDEX idx_trace_span_time6 ON tblLubanTraceSpan6(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan7 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity7 ON tblLubanTraceSpan7(run_id, span_id);
CREATE INDEX idx_trace_span_time7 ON tblLubanTraceSpan7(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan8 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity8 ON tblLubanTraceSpan8(run_id, span_id);
CREATE INDEX idx_trace_span_time8 ON tblLubanTraceSpan8(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan9 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity9 ON tblLubanTraceSpan9(run_id, span_id);
CREATE INDEX idx_trace_span_time9 ON tblLubanTraceSpan9(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan10 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity10 ON tblLubanTraceSpan10(run_id, span_id);
CREATE INDEX idx_trace_span_time10 ON tblLubanTraceSpan10(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan11 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity11 ON tblLubanTraceSpan11(run_id, span_id);
CREATE INDEX idx_trace_span_time11 ON tblLubanTraceSpan11(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan12 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity12 ON tblLubanTraceSpan12(run_id, span_id);
CREATE INDEX idx_trace_span_time12 ON tblLubanTraceSpan12(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan13 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity13 ON tblLubanTraceSpan13(run_id, span_id);
CREATE INDEX idx_trace_span_time13 ON tblLubanTraceSpan13(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan14 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity14 ON tblLubanTraceSpan14(run_id, span_id);
CREATE INDEX idx_trace_span_time14 ON tblLubanTraceSpan14(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan15 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity15 ON tblLubanTraceSpan15(run_id, span_id);
CREATE INDEX idx_trace_span_time15 ON tblLubanTraceSpan15(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan16 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity16 ON tblLubanTraceSpan16(run_id, span_id);
CREATE INDEX idx_trace_span_time16 ON tblLubanTraceSpan16(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan17 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity17 ON tblLubanTraceSpan17(run_id, span_id);
CREATE INDEX idx_trace_span_time17 ON tblLubanTraceSpan17(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan18 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity18 ON tblLubanTraceSpan18(run_id, span_id);
CREATE INDEX idx_trace_span_time18 ON tblLubanTraceSpan18(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan19 (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT,
    create_time INTEGER NOT NULL DEFAULT 0,
    update_time INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uk_trace_span_identity19 ON tblLubanTraceSpan19(run_id, span_id);
CREATE INDEX idx_trace_span_time19 ON tblLubanTraceSpan19(started_at, run_id, span_id);
