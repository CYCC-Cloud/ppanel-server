ALTER TABLE `nodes`
    DROP INDEX `idx_nodes_server_listener`,
    DROP COLUMN `listener_key`;
