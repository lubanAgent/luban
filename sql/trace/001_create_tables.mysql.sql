-- Apply through the host's database change process. The SDK never runs this
-- file automatically. Target: MySQL-compatible InnoDB / validated OceanBase.
CREATE TABLE tblLubanTraceRun (
    id            bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    service       varchar(128) NOT NULL DEFAULT '' COMMENT '服务名称',
    agent_key     varchar(255) NOT NULL DEFAULT '' COMMENT 'Agent标识',
    business_type varchar(64) NOT NULL DEFAULT '' COMMENT '业务类型',
    business_id   varchar(255) NOT NULL DEFAULT '' COMMENT '业务标识',
    request_id    varchar(255) NOT NULL DEFAULT '' COMMENT '请求标识',
    status        varchar(32) NOT NULL DEFAULT '' COMMENT '运行状态',
    started_at    datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns   bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    error_msg     longtext COMMENT '错误信息',
    create_time   bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time   bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted       tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_run_run_id (run_id),
    KEY idx_trace_run_agent_time (service, agent_key, started_at, run_id),
    KEY idx_trace_run_business (service, business_type, business_id, started_at),
    KEY idx_trace_run_time (started_at, run_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace运行摘要';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan0 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity0 (run_id, span_id),
    KEY idx_trace_span_time0 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表0';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan1 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity1 (run_id, span_id),
    KEY idx_trace_span_time1 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表1';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan2 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity2 (run_id, span_id),
    KEY idx_trace_span_time2 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表2';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan3 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity3 (run_id, span_id),
    KEY idx_trace_span_time3 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表3';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan4 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity4 (run_id, span_id),
    KEY idx_trace_span_time4 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表4';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan5 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity5 (run_id, span_id),
    KEY idx_trace_span_time5 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表5';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan6 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity6 (run_id, span_id),
    KEY idx_trace_span_time6 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表6';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan7 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity7 (run_id, span_id),
    KEY idx_trace_span_time7 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表7';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan8 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity8 (run_id, span_id),
    KEY idx_trace_span_time8 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表8';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan9 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity9 (run_id, span_id),
    KEY idx_trace_span_time9 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表9';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan10 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity10 (run_id, span_id),
    KEY idx_trace_span_time10 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表10';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan11 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity11 (run_id, span_id),
    KEY idx_trace_span_time11 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表11';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan12 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity12 (run_id, span_id),
    KEY idx_trace_span_time12 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表12';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan13 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity13 (run_id, span_id),
    KEY idx_trace_span_time13 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表13';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan14 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity14 (run_id, span_id),
    KEY idx_trace_span_time14 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表14';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan15 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity15 (run_id, span_id),
    KEY idx_trace_span_time15 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表15';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan16 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity16 (run_id, span_id),
    KEY idx_trace_span_time16 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表16';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan17 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity17 (run_id, span_id),
    KEY idx_trace_span_time17 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表17';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan18 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity18 (run_id, span_id),
    KEY idx_trace_span_time18 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表18';

-- Fixed routing: CRC32 IEEE of the complete runId modulo 20.
CREATE TABLE tblLubanTraceSpan19 (
    id             bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    run_id         varchar(64) NOT NULL DEFAULT '' COMMENT 'Run唯一标识',
    span_id        varchar(64) NOT NULL DEFAULT '' COMMENT 'Span唯一标识',
    parent_span_id varchar(64) NOT NULL DEFAULT '' COMMENT '父Span唯一标识',
    kind           varchar(64) NOT NULL DEFAULT '' COMMENT 'Span类型',
    status         varchar(32) NOT NULL DEFAULT '' COMMENT 'Span状态',
    started_at     datetime(6) NOT NULL DEFAULT '1970-01-01 00:00:00.000000' COMMENT '开始时间，UTC',
    duration_ns    bigint(20) NOT NULL DEFAULT 0 COMMENT '执行耗时，纳秒',
    event_json     longtext COMMENT '完整Trace事件JSON',
    create_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '创建时间，Unix秒',
    update_time    bigint(20) NOT NULL DEFAULT 0 COMMENT '更新时间，Unix秒',
    deleted        tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '删除标记，0未删除',
    PRIMARY KEY (id),
    UNIQUE KEY uk_trace_span_identity19 (run_id, span_id),
    KEY idx_trace_span_time19 (started_at, run_id, span_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Luban Trace Span分表19';
