-- Offline test/example schema only; production uses sql/trace/*.mysql.sql.
CREATE TABLE tblLubanTraceRun (
    run_id TEXT NOT NULL PRIMARY KEY,
    service TEXT NOT NULL,
    agent_key TEXT NOT NULL,
    business_type TEXT NOT NULL DEFAULT '',
    business_id TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    error_msg TEXT NOT NULL,
    create_time DATETIME NOT NULL
);
CREATE INDEX idx_trace_run_agent_time ON tblLubanTraceRun(service, agent_key, started_at, run_id);
CREATE INDEX idx_trace_run_business ON tblLubanTraceRun(service, business_type, business_id, started_at);
CREATE INDEX idx_trace_run_time ON tblLubanTraceRun(started_at, run_id);

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan00 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time00 ON tblLubanTraceSpan00(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan01 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time01 ON tblLubanTraceSpan01(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan02 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time02 ON tblLubanTraceSpan02(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan03 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time03 ON tblLubanTraceSpan03(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan04 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time04 ON tblLubanTraceSpan04(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan05 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time05 ON tblLubanTraceSpan05(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan06 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time06 ON tblLubanTraceSpan06(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan07 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time07 ON tblLubanTraceSpan07(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan08 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time08 ON tblLubanTraceSpan08(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan09 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time09 ON tblLubanTraceSpan09(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan10 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time10 ON tblLubanTraceSpan10(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan11 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time11 ON tblLubanTraceSpan11(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan12 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time12 ON tblLubanTraceSpan12(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan13 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time13 ON tblLubanTraceSpan13(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan14 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time14 ON tblLubanTraceSpan14(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan15 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time15 ON tblLubanTraceSpan15(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan16 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time16 ON tblLubanTraceSpan16(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan17 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time17 ON tblLubanTraceSpan17(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan18 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time18 ON tblLubanTraceSpan18(started_at, run_id, span_id);

CREATE TABLE tblLubanTraceSpan19 (
    run_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    duration_ns INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    create_time DATETIME NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
CREATE INDEX idx_trace_span_time19 ON tblLubanTraceSpan19(started_at, run_id, span_id);
