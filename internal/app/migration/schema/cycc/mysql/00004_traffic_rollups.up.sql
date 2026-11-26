-- The user traffic history reads these two aggregates instead of the raw
-- traffic_log, whose retention stays short. The rollup task rebuilds them,
-- overwriting each hour and day, so they are derived data: the fork's own
-- tables, created conditionally like every fork migration.
--
-- hour_start is the hour's start in the application time zone, and day is the
-- local date, written as text: both drivers store a zone-less timestamp, so a
-- DATE column would move a local midnight into the previous day whenever the
-- session zone is west of the application's.
CREATE TABLE IF NOT EXISTS `traffic_hourly`
(
    `id`                bigint  NOT NULL AUTO_INCREMENT,
    `hour_start`        datetime(3) NOT NULL COMMENT 'Local Hour Start',
    `user_subscribe_id` bigint  NOT NULL COMMENT 'User Subscription ID',
    `user_id`           bigint  NOT NULL DEFAULT '0' COMMENT 'User ID',
    `server_id`         bigint  NOT NULL DEFAULT '0' COMMENT 'Server ID',
    `upload`            bigint  NOT NULL DEFAULT '0' COMMENT 'Upload Traffic',
    `download`          bigint  NOT NULL DEFAULT '0' COMMENT 'Download Traffic',
    `updated_at`        datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT 'Update Time',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_traffic_hourly_hour_subscribe_server` (`hour_start`, `user_subscribe_id`, `server_id`),
    KEY `idx_traffic_hourly_subscribe_hour` (`user_subscribe_id`, `hour_start`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `traffic_daily`
(
    `id`                bigint       NOT NULL AUTO_INCREMENT,
    `day`               varchar(10)  NOT NULL COMMENT 'Local Date',
    `user_subscribe_id` bigint       NOT NULL COMMENT 'User Subscription ID',
    `user_id`           bigint       NOT NULL DEFAULT '0' COMMENT 'User ID',
    `server_id`         bigint       NOT NULL DEFAULT '0' COMMENT 'Server ID',
    `upload`            bigint       NOT NULL DEFAULT '0' COMMENT 'Upload Traffic',
    `download`          bigint       NOT NULL DEFAULT '0' COMMENT 'Download Traffic',
    `updated_at`        datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT 'Update Time',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_traffic_daily_day_subscribe_server` (`day`, `user_subscribe_id`, `server_id`),
    KEY `idx_traffic_daily_subscribe_day` (`user_subscribe_id`, `day`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci;
