-- Adds the display capacity of a server, in users. Fork migrations run
-- after upstream's and are recorded in their own table, so this may meet a
-- column created by a retired fork release: the statement stays conditional.
ALTER TABLE "servers"
    ADD COLUMN IF NOT EXISTS "capacity" bigint NOT NULL DEFAULT 0;
