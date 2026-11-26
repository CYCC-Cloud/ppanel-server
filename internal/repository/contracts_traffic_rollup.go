package repository

import (
	"context"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
)

// TrafficRollupRepo owns the fork's traffic rollup tables, traffic_hourly and
// traffic_daily: the per-hour and per-day aggregates of the raw traffic log
// that the user traffic history reads. The rollup task overwrites a bucket,
// so a rerun or a second replica never double counts.
type TrafficRollupRepo interface {
	// OldestTrafficLog returns the timestamp of the oldest raw traffic log
	// row; false when the table holds none.
	OldestTrafficLog(ctx context.Context) (time.Time, bool, error)
	// LatestHourlyStart returns the newest hourly bucket start; false when
	// the hourly table holds none.
	LatestHourlyStart(ctx context.Context) (time.Time, bool, error)
	// OldestHourlyStart returns the oldest hourly bucket start; false when
	// the hourly table holds none.
	OldestHourlyStart(ctx context.Context) (time.Time, bool, error)

	// TrafficLogSums sums the raw log per subscription, user and server over
	// [start, end).
	TrafficLogSums(ctx context.Context, start, end time.Time) ([]traffic.TrafficSum, error)
	// ReplaceHourly overwrites the rows of one hour in one transaction, so
	// rows whose traffic disappeared go with them.
	ReplaceHourly(ctx context.Context, hourStart time.Time, rows []traffic.TrafficSum) error
	// HourlySums sums the hourly rows per subscription, user and server over
	// [start, end).
	HourlySums(ctx context.Context, start, end time.Time) ([]traffic.TrafficSum, error)
	// ReplaceDaily overwrites the rows of one day in one transaction.
	ReplaceDaily(ctx context.Context, day string, rows []traffic.TrafficSum) error

	// DeleteBeforeBatch deletes up to limit hourly rows older than before,
	// and reports how many it deleted; it is the rollup's retention prune.
	DeleteBeforeBatch(ctx context.Context, before time.Time, limit int) (int64, error)
	// DeleteDailyBeforeBatch deletes up to limit daily rows before day
	// ("YYYY-MM-DD"), and reports how many it deleted.
	DeleteDailyBeforeBatch(ctx context.Context, before string, limit int) (int64, error)

	// TrafficLogPoints returns the raw log rows of one subscription in
	// [start, end), oldest first, for the minute series.
	TrafficLogPoints(ctx context.Context, userSubscribeID, userID int64, start, end time.Time) ([]traffic.TrafficPoint, error)
	// HourPoints returns one subscription's hourly traffic in [start, end).
	HourPoints(ctx context.Context, userSubscribeID, userID int64, start, end time.Time) ([]traffic.TrafficPoint, error)
	// DayPoints returns one subscription's daily traffic over the days in
	// [fromDay, toDay).
	DayPoints(ctx context.Context, userSubscribeID, userID int64, fromDay, toDay string) ([]traffic.DayPoint, error)
	// ServerHourPoints returns one subscription's traffic per server over
	// [start, end).
	ServerHourPoints(ctx context.Context, userSubscribeID, userID int64, start, end time.Time) ([]traffic.ServerTraffic, error)
	// ServerDayPoints returns one subscription's traffic per server over the
	// days in [fromDay, toDay).
	ServerDayPoints(ctx context.Context, userSubscribeID, userID int64, fromDay, toDay string) ([]traffic.ServerTraffic, error)
}
