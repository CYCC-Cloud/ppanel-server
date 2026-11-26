-- Adds the display capacity of a server, in users. Fork migrations run
-- after upstream's and are recorded in their own table, so this may meet a
-- column created by a retired fork release: the statement stays conditional.
SET @column_exists = (SELECT COUNT(*)
                      FROM information_schema.COLUMNS
                      WHERE TABLE_SCHEMA = DATABASE()
                        AND TABLE_NAME = 'servers'
                        AND COLUMN_NAME = 'capacity');
SET @sql = IF(@column_exists = 0,
              'ALTER TABLE `servers` ADD COLUMN `capacity` bigint NOT NULL DEFAULT 0 COMMENT ''Display capacity in users''',
              'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
