-- The rollup tables hold derived data: every row can be rebuilt from the raw
-- traffic log while it is still retained, so dropping them loses no source
-- record.
DROP TABLE IF EXISTS `traffic_hourly`;
DROP TABLE IF EXISTS `traffic_daily`;
