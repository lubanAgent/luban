-- Apply through the host's database change process. The SDK never runs this
-- file automatically. Target: MySQL-compatible InnoDB / validated OceanBase.
CREATE TABLE tblLubanTraceRun (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    service       varchar(64) NOT NULL,
    agent_key     varchar(191) NOT NULL,
    business_type varchar(64) NOT NULL DEFAULT '',
    business_id   varchar(191) NOT NULL DEFAULT '',
    request_id    varchar(191) NOT NULL DEFAULT '',
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    error_msg     longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id),
    KEY idx_trace_run_agent_time (service, agent_key, started_at, run_id),
    KEY idx_trace_run_business (service, business_type, business_id, started_at),
    KEY idx_trace_run_time (started_at, run_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan00 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time00 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan01 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time01 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan02 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time02 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan03 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time03 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan04 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time04 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan05 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time05 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan06 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time06 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan07 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time07 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan08 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time08 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan09 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time09 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan10 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time10 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan11 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time11 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan12 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time12 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan13 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time13 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan14 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time14 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan15 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time15 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan16 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time16 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan17 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time17 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan18 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time18 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE tblLubanTraceSpan19 (
    run_id        varchar(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    span_id       varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_span_id varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    kind          varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status        varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at    datetime(6) NOT NULL,
    duration_ns   bigint NOT NULL,
    event_json    longtext NOT NULL,
    create_time   datetime(6) NOT NULL,
    PRIMARY KEY (run_id, span_id),
    KEY idx_trace_span_time19 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
