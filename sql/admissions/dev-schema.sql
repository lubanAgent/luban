-- Luban admissions migration development schema.
--
-- This file is only for a new, disposable local database used to exercise the
-- migrated service. It is not an upgrade script for an existing admissions
-- database. Do not apply it to a shared or production database.
--
-- It contains the tables used by the current Luban repository and the
-- ConfigItem source table. Seed/import business ConfigItems separately.

SET NAMES utf8mb4;

CREATE TABLE `tblConfigItem` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `school_id` BIGINT NOT NULL DEFAULT 0,
  `config_type` VARCHAR(32) NOT NULL DEFAULT '',
  `name` VARCHAR(128) NOT NULL DEFAULT '',
  `district` VARCHAR(64) NOT NULL DEFAULT '',
  `data_json` TEXT,
  `status` TINYINT NOT NULL DEFAULT 1,
  `create_by` BIGINT NOT NULL DEFAULT 0,
  `update_by` BIGINT NOT NULL DEFAULT 0,
  `create_time` BIGINT NOT NULL DEFAULT 0,
  `update_time` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_school_type_status` (`school_id`, `config_type`, `status`),
  KEY `idx_school_district` (`school_id`, `district`),
  KEY `idx_school_name` (`school_id`, `name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `tblStrategy` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `school_id` BIGINT NOT NULL DEFAULT 0,
  `name` VARCHAR(128) NOT NULL DEFAULT '',
  `version` INT UNSIGNED NOT NULL DEFAULT 1,
  `status` TINYINT NOT NULL DEFAULT 1,
  `conversation_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `snapshot_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `published_artifact_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `publish_time` BIGINT NOT NULL DEFAULT 0,
  `create_time` BIGINT NOT NULL DEFAULT 0,
  `update_time` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_school_name` (`school_id`, `name`),
  KEY `idx_school_status` (`school_id`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `tblStrategyConversation` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `strategy_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `current_phase` VARCHAR(32) NOT NULL DEFAULT 'collecting',
  `collected_summary` TEXT,
  `requirements_revision` BIGINT NOT NULL DEFAULT 0,
  `processed_message_seq` BIGINT NOT NULL DEFAULT 0,
  `next_message_seq` BIGINT NOT NULL DEFAULT 0,
  `preview_artifact_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `confirm_time` BIGINT NOT NULL DEFAULT 0,
  `confirm_by` BIGINT NOT NULL DEFAULT 0,
  `create_time` BIGINT NOT NULL DEFAULT 0,
  `update_time` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_strategy` (`strategy_id`),
  KEY `idx_phase` (`current_phase`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `tblStrategyMessage` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `conversation_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `seq` BIGINT NOT NULL DEFAULT 0,
  `role` VARCHAR(32) NOT NULL DEFAULT '',
  `agent_type` VARCHAR(64) NOT NULL DEFAULT '',
  `content` MEDIUMTEXT,
  `metadata` TEXT,
  `client_request_id` VARCHAR(128) DEFAULT NULL,
  `create_time` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_conversation_seq` (`conversation_id`, `seq`),
  UNIQUE KEY `uk_conversation_request` (`conversation_id`, `client_request_id`),
  KEY `idx_conv_create` (`conversation_id`, `create_time`),
  KEY `idx_conv_role` (`conversation_id`, `role`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `tblStrategyMessageTask` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `conversation_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `strategy_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `user_message_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `message_seq` BIGINT NOT NULL DEFAULT 0,
  `status` VARCHAR(32) NOT NULL DEFAULT 'pending',
  `mode` VARCHAR(32) NOT NULL DEFAULT '',
  `assistant_message_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `result_json` MEDIUMTEXT,
  `last_error` TEXT,
  `attempt_token` VARCHAR(64) NOT NULL DEFAULT '',
  `attempt_count` INT UNSIGNED NOT NULL DEFAULT 0,
  `lease_until` BIGINT NOT NULL DEFAULT 0,
  `next_retry_at` BIGINT NOT NULL DEFAULT 0,
  `engine` VARCHAR(32) NOT NULL DEFAULT '',
  `snapshot_json` MEDIUMTEXT,
  `requirements_revision` BIGINT NOT NULL DEFAULT 0,
  `result_artifact_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `start_time` BIGINT NOT NULL DEFAULT 0,
  `finish_time` BIGINT NOT NULL DEFAULT 0,
  `create_time` BIGINT NOT NULL DEFAULT 0,
  `update_time` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_user_message` (`user_message_id`),
  KEY `idx_task_claim` (`engine`, `status`, `next_retry_at`, `id`),
  KEY `idx_conversation_seq` (`conversation_id`, `message_seq`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `tblStrategyGenTask` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `strategy_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `conversation_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `trigger_reason` VARCHAR(32) NOT NULL DEFAULT '',
  `input_snapshot` MEDIUMTEXT,
  `status` VARCHAR(32) NOT NULL DEFAULT 'pending',
  `current_stage` VARCHAR(64) NOT NULL DEFAULT '',
  `progress` TINYINT NOT NULL DEFAULT 0,
  `last_error` TEXT,
  `attempt_token` VARCHAR(64) NOT NULL DEFAULT '',
  `attempt_count` INT UNSIGNED NOT NULL DEFAULT 0,
  `lease_until` BIGINT NOT NULL DEFAULT 0,
  `next_retry_at` BIGINT NOT NULL DEFAULT 0,
  `engine` VARCHAR(32) NOT NULL DEFAULT '',
  `snapshot_json` MEDIUMTEXT,
  `requirements_revision` BIGINT NOT NULL DEFAULT 0,
  `result_artifact_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `start_time` BIGINT NOT NULL DEFAULT 0,
  `finish_time` BIGINT NOT NULL DEFAULT 0,
  `create_time` BIGINT NOT NULL DEFAULT 0,
  `update_time` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_conversation_revision_engine` (`conversation_id`, `requirements_revision`, `engine`),
  KEY `idx_task_claim` (`engine`, `status`, `next_retry_at`, `id`),
  KEY `idx_conversation_status` (`conversation_id`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `tblStrategyAgentDraft` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `conversation_id` BIGINT UNSIGNED NOT NULL,
  `revision` BIGINT NOT NULL DEFAULT 0,
  `draft_json` MEDIUMTEXT NOT NULL,
  `update_time` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_conversation` (`conversation_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `tblStrategyAgentArtifact` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `task_id` BIGINT UNSIGNED NOT NULL,
  `strategy_id` BIGINT UNSIGNED NOT NULL,
  `conversation_id` BIGINT UNSIGNED NOT NULL,
  `hash` CHAR(64) NOT NULL,
  `artifact_json` MEDIUMTEXT NOT NULL,
  `snapshot_json` MEDIUMTEXT NOT NULL,
  `status` VARCHAR(32) NOT NULL DEFAULT 'ready',
  `create_time` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_task` (`task_id`),
  KEY `idx_strategy_conversation` (`strategy_id`, `conversation_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
