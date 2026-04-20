ALTER TABLE `nodes`
    ADD COLUMN `listener_key` varchar(100) NOT NULL DEFAULT '' AFTER `protocol`,
    ADD INDEX `idx_nodes_server_listener` (`server_id`, `listener_key`);
