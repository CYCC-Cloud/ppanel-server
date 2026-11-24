-- Adds the per-listener identity column a multi-listener server needs.
-- Fork migrations run after upstream's and are recorded in their own
-- table, so this may meet a column created by a retired fork release:
-- both statements stay conditional.

ALTER TABLE "nodes"
    ADD COLUMN IF NOT EXISTS "listener_key" varchar(100) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS "idx_nodes_server_listener"
    ON "nodes" ("server_id", "listener_key");
