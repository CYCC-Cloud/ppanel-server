-- Adds the per-listener identity column a multi-listener server needs.
-- Fork migrations run after upstream's and are recorded in their own
-- table, so this may meet a column created by a retired fork release:
-- both statements stay conditional.

SET @column_exists = (SELECT COUNT(*)
                      FROM information_schema.COLUMNS
                      WHERE TABLE_SCHEMA = DATABASE()
                        AND TABLE_NAME = 'nodes'
                        AND COLUMN_NAME = 'listener_key');
SET @sql = IF(@column_exists = 0,
              'ALTER TABLE `nodes` ADD COLUMN `listener_key` varchar(100) NOT NULL DEFAULT '''' COMMENT ''Server listener key''',
              'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @index_exists = (SELECT COUNT(*)
                     FROM information_schema.STATISTICS
                     WHERE TABLE_SCHEMA = DATABASE()
                       AND TABLE_NAME = 'nodes'
                       AND INDEX_NAME = 'idx_nodes_server_listener');
SET @sql = IF(@index_exists = 0,
              'ALTER TABLE `nodes` ADD INDEX `idx_nodes_server_listener` (`server_id`, `listener_key`)',
              'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
