-- Drops the display capacity, conditionally: the column may not be there.
ALTER TABLE "servers" DROP COLUMN IF EXISTS "capacity";
