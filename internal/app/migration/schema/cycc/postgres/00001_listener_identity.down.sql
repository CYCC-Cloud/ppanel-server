-- Intentionally a no-op. nodes.listener_key is the identity that maps a node
-- to one of its server's listeners; dropping it discards data this migration
-- may not have created and breaks traffic accounting for multi-listener
-- servers. Roll back by restoring a database backup instead.
SELECT 1;
