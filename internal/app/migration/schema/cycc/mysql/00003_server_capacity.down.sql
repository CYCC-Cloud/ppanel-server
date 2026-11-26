-- Drops the display capacity, conditionally: the column may not be there.
SET @column_exists = (SELECT COUNT(*)
                      FROM information_schema.COLUMNS
                      WHERE TABLE_SCHEMA = DATABASE()
                        AND TABLE_NAME = 'servers'
                        AND COLUMN_NAME = 'capacity');
SET @sql = IF(@column_exists = 0,
              'SELECT 1',
              'ALTER TABLE `servers` DROP COLUMN `capacity`');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
