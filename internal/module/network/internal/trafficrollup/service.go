// Package trafficrollup rebuilds the fork's traffic rollup tables from the
// raw traffic log and answers the user traffic history. The raw log is kept
// for days, the hourly rollup for months and the daily rollup indefinitely,
// so a user can see their own traffic long after the log rows are gone. Only
// the network module's facade may reach this package.
//
// The buckets are the application's local hours and days. Like every stored
// time in this application, they rely on the database session zone being
// AppLocation (see orm and app.warnDatabaseTimeZone): the drivers convert a
// written time into the session zone, so a bucket read back is the same
// instant only while the two zones agree.
package trafficrollup

import (
	"context"
	"sort"
	"time"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/orm"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

const (
	// hourlyOverlap re-reads the newest rolled hour and the one before it, so
	// a report that reached the raw log after a run's hour was written is
	// still counted, and two replicas rolling at once converge.
	hourlyOverlap = time.Hour
	// retentionBatchSize bounds each delete of the retention cleanup.
	retentionBatchSize = 5000
	// maxRollupHours bounds one run's rebuild. The first run backfills what
	// the raw log still holds; a run further behind than this leaves the rest
	// to the next tick instead of holding the task for minutes.
	maxRollupHours = 24 * 40

	// The granularities of the user traffic history.
	GranularityMinute = "minute"
	GranularityHour   = "hour"
	GranularityDay    = "day"
	GranularityMonth  = "month"

	maxMinuteSpan = 24 * time.Hour
	maxHourSpan   = 31 * 24 * time.Hour
	maxDaySpan    = 366 * 24 * time.Hour
	maxMonthSpan  = 120
	maxServerSpan = 366 * 24 * time.Hour
)

// Limits is the retention the rollup applies, in days. A zero daily limit
// keeps the daily rows forever.
type Limits struct {
	HourlyDays int64
	DailyDays  int64
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Rollup repository.TrafficRollupRepo
	// Limits reads the runtime retention settings; nil keeps the defaults.
	Limits func() Limits
}

// Service is the rollup entry point used by the network facade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

func (s *Service) limits() Limits {
	if s.deps.Limits == nil {
		return Limits{HourlyDays: config.DefaultTrafficHourlyRetentionDays}
	}
	return s.deps.Limits()
}

// Run rebuilds every rollup bucket the watermark does not cover yet and
// applies the retention. Overwriting a bucket makes a rerun, a second
// replica and a late report harmless: the newest sums replace the stored
// ones instead of adding to them.
func (s *Service) Run(ctx context.Context) error {
	return s.RunAt(ctx, timeutil.Now())
}

// RunAt is Run at the given instant, so the range a run covers is testable.
func (s *Service) RunAt(ctx context.Context, now time.Time) error {
	loc := timeutil.Location()
	now = now.In(loc)
	// The hour in progress is rebuilt too: it lags by at most one tick and
	// the next run overwrites it with the complete hour.
	end := nextLocalHour(localHourStart(now, loc), loc)

	start, ok, err := s.rollupStart(ctx, loc)
	if err != nil {
		return err
	}
	if ok && start.Before(end) {
		days, err := s.rollupHours(ctx, start, end, loc)
		if err != nil {
			return err
		}
		if err := s.rollupDays(ctx, days, loc); err != nil {
			return err
		}
	}
	return s.applyRetention(ctx, now, loc)
}

// rollupStart returns the first hour a run rebuilds: the newest rolled hour,
// one hour back, or the oldest raw log hour when nothing was rolled yet.
func (s *Service) rollupStart(ctx context.Context, loc *time.Location) (time.Time, bool, error) {
	latest, ok, err := s.deps.Rollup.LatestHourlyStart(ctx)
	if err != nil {
		return time.Time{}, false, xerr.Wrapf(err, xerr.DatabaseQueryError, "read the newest traffic rollup hour")
	}
	if ok {
		return localHourStart(latest.In(loc), loc).Add(-hourlyOverlap), true, nil
	}
	oldest, ok, err := s.deps.Rollup.OldestTrafficLog(ctx)
	if err != nil {
		return time.Time{}, false, xerr.Wrapf(err, xerr.DatabaseQueryError, "read the oldest traffic log row")
	}
	if !ok {
		return time.Time{}, false, nil
	}
	return localHourStart(oldest.In(loc), loc), true, nil
}

// rollupHours rebuilds each hour in [start, end) and returns the local dates
// it touched, in order, so only those days are recomputed.
func (s *Service) rollupHours(ctx context.Context, start, end time.Time, loc *time.Location) ([]string, error) {
	hour := localHourStart(start.In(loc), loc)
	days := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)
	hours := 0
	for hour.Before(end) {
		if hours >= maxRollupHours {
			logger.WithContext(ctx).Infow("[Traffic Rollup] covered its hour limit, the next tick continues",
				logger.Field("next_hour", hour.Format(time.RFC3339)))
			break
		}
		next := nextLocalHour(hour, loc)
		rows, err := s.deps.Rollup.TrafficLogSums(ctx, hour, next)
		if err != nil {
			return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "sum the traffic log of %s", hour.Format(time.RFC3339))
		}
		if err := s.deps.Rollup.ReplaceHourly(ctx, hour, rows); err != nil {
			return nil, xerr.Wrapf(err, xerr.DatabaseUpdateError, "store the traffic rollup hour %s", hour.Format(time.RFC3339))
		}
		day := hour.Format(time.DateOnly)
		if _, touched := seen[day]; !touched {
			seen[day] = struct{}{}
			days = append(days, day)
		}
		hour = next
		hours++
	}
	return days, nil
}

// rollupDays recomputes each touched local day from its hourly rows.
func (s *Service) rollupDays(ctx context.Context, days []string, loc *time.Location) error {
	for _, day := range days {
		dayStart, err := time.ParseInLocation(time.DateOnly, day, loc)
		if err != nil {
			return xerr.Wrapf(err, xerr.ERROR, "parse the rollup day %s", day)
		}
		rows, err := s.deps.Rollup.HourlySums(ctx, dayStart, nextLocalDay(dayStart))
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "sum the traffic rollup hours of %s", day)
		}
		if err := s.deps.Rollup.ReplaceDaily(ctx, day, rows); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "store the traffic rollup day %s", day)
		}
	}
	return nil
}

// applyRetention deletes the expired rollup rows. The daily table is only
// pruned when a limit is configured: zero keeps it forever.
func (s *Service) applyRetention(ctx context.Context, now time.Time, loc *time.Location) error {
	limits := s.limits()
	if limits.HourlyDays > 0 {
		before := localHourStart(now, loc).AddDate(0, 0, -int(limits.HourlyDays))
		deleted, err := orm.DeleteBefore(ctx, s.deps.Rollup, before, retentionBatchSize)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseDeletedError, "delete the traffic rollup hours before %s", before.Format(time.RFC3339))
		}
		if deleted > 0 {
			logger.WithContext(ctx).Infow("[Traffic Rollup] deleted expired hours",
				logger.Field("deleted", deleted), logger.Field("before", before.Format(time.RFC3339)))
		}
	}
	if limits.DailyDays > 0 {
		before := now.AddDate(0, 0, -int(limits.DailyDays)).Format(time.DateOnly)
		for {
			deleted, err := s.deps.Rollup.DeleteDailyBeforeBatch(ctx, before, retentionBatchSize)
			if err != nil {
				return xerr.Wrapf(err, xerr.DatabaseDeletedError, "delete the traffic rollup days before %s", before)
			}
			if deleted < retentionBatchSize {
				return nil
			}
		}
	}
	return nil
}

// UserHistory returns the zero-filled traffic series of one subscription.
// The buckets are the local minutes, hours, days or months of
// [start, end), and the amounts are the ones counted against the
// subscription's quota.
func (s *Service) UserHistory(ctx context.Context, userSubscribeID, userID int64, granularity string, start, end time.Time) ([]traffic.TrafficPoint, error) {
	loc := timeutil.Location()
	start, end = start.In(loc), end.In(loc)
	if !start.Before(end) {
		return nil, xerr.Errorf(xerr.InvalidParams, "start must be before end")
	}
	switch granularity {
	case GranularityMinute:
		if end.Sub(start) > maxMinuteSpan {
			return nil, xerr.Errorf(xerr.InvalidParams, "the minute series covers at most %d hours", int(maxMinuteSpan/time.Hour))
		}
		from := start.Truncate(time.Minute)
		rows, err := s.deps.Rollup.TrafficLogPoints(ctx, userSubscribeID, userID, from, end)
		if err != nil {
			return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "read the traffic log minutes")
		}
		return minuteSeries(from, end, rows), nil
	case GranularityHour:
		if end.Sub(start) > maxHourSpan {
			return nil, xerr.Errorf(xerr.InvalidParams, "the hour series covers at most %d days", int(maxHourSpan/(24*time.Hour)))
		}
		from := localHourStart(start, loc)
		rows, err := s.deps.Rollup.HourPoints(ctx, userSubscribeID, userID, from, end)
		if err != nil {
			return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "read the traffic rollup hours")
		}
		return hourSeries(from, end, rows), nil
	case GranularityDay, GranularityMonth:
		if granularity == GranularityDay && end.Sub(start) > maxDaySpan {
			return nil, xerr.Errorf(xerr.InvalidParams, "the day series covers at most %d days", int(maxDaySpan/(24*time.Hour)))
		}
		if granularity == GranularityMonth && monthsBetween(start, end) > maxMonthSpan {
			return nil, xerr.Errorf(xerr.InvalidParams, "the month series covers at most %d months", maxMonthSpan)
		}
		fromDay := start.Format(time.DateOnly)
		toDay := localDayStart(end.Add(-time.Nanosecond)).AddDate(0, 0, 1).Format(time.DateOnly)
		rows, err := s.deps.Rollup.DayPoints(ctx, userSubscribeID, userID, fromDay, toDay)
		if err != nil {
			return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "read the traffic rollup days")
		}
		if granularity == GranularityDay {
			return daySeries(start, end, loc, rows), nil
		}
		return monthSeries(start, end, loc, rows), nil
	default:
		return nil, xerr.Errorf(xerr.InvalidParams, "granularity must be one of %s, %s, %s, %s",
			GranularityMinute, GranularityHour, GranularityDay, GranularityMonth)
	}
}

// UserServers returns one subscription's traffic per server over
// [start, end), largest first. The hourly table answers a range it still
// covers; an older range falls back to the daily table, which resolves its
// two edge days as whole days.
func (s *Service) UserServers(ctx context.Context, userSubscribeID, userID int64, start, end time.Time) ([]traffic.ServerTraffic, error) {
	loc := timeutil.Location()
	start, end = start.In(loc), end.In(loc)
	if !start.Before(end) {
		return nil, xerr.Errorf(xerr.InvalidParams, "start must be before end")
	}
	if end.Sub(start) > maxServerSpan {
		return nil, xerr.Errorf(xerr.InvalidParams, "the server series covers at most %d days", int(maxServerSpan/(24*time.Hour)))
	}

	oldest, ok, err := s.deps.Rollup.OldestHourlyStart(ctx)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "read the oldest traffic rollup hour")
	}
	var rows []traffic.ServerTraffic
	if ok && !start.Before(oldest.In(loc)) {
		rows, err = s.deps.Rollup.ServerHourPoints(ctx, userSubscribeID, userID, start, end)
	} else {
		fromDay := start.Format(time.DateOnly)
		toDay := localDayStart(end.Add(-time.Nanosecond)).AddDate(0, 0, 1).Format(time.DateOnly)
		rows, err = s.deps.Rollup.ServerDayPoints(ctx, userSubscribeID, userID, fromDay, toDay)
	}
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "read the traffic per server")
	}
	sort.Slice(rows, func(i, j int) bool {
		left, right := rows[i].Upload+rows[i].Download, rows[j].Upload+rows[j].Download
		if left != right {
			return left > right
		}
		return rows[i].ServerId < rows[j].ServerId
	})
	return rows, nil
}

// localHourStart returns the start of the local wall-clock hour holding at.
func localHourStart(at time.Time, loc *time.Location) time.Time {
	at = at.In(loc)
	return time.Date(at.Year(), at.Month(), at.Day(), at.Hour(), 0, 0, 0, loc)
}

// nextLocalHour returns the start of the next local wall-clock hour. Go
// resolves a wall clock the spring-forward gap skips onto an adjacent
// instant, so that hour holds one elapsed hour, while the fall-back hour
// holds two: the day keeps 23 or 25 real hours either way.
func nextLocalHour(hour time.Time, loc *time.Location) time.Time {
	return time.Date(hour.Year(), hour.Month(), hour.Day(), hour.Hour()+1, 0, 0, 0, loc)
}

// localDayStart returns the start of the local day holding at.
func localDayStart(at time.Time) time.Time {
	return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
}

func nextLocalDay(day time.Time) time.Time {
	return day.AddDate(0, 0, 1)
}

// monthsBetween counts the calendar months from start's to end's, exclusive
// of the month holding end.
func monthsBetween(start, end time.Time) int {
	first := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, start.Location())
	last := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, end.Location())
	return (last.Year()-first.Year())*12 + int(last.Month()-first.Month())
}

// minuteSeries zero-fills the minutes of [from, end) with the raw log's rows.
func minuteSeries(from, end time.Time, rows []traffic.TrafficPoint) []traffic.TrafficPoint {
	sums := make(map[int64][2]int64, len(rows))
	for _, row := range rows {
		key := row.Start.Truncate(time.Minute).Unix()
		sum := sums[key]
		sums[key] = [2]int64{sum[0] + row.Upload, sum[1] + row.Download}
	}
	points := make([]traffic.TrafficPoint, 0, int(end.Sub(from)/time.Minute)+1)
	for at := from; at.Before(end); at = at.Add(time.Minute) {
		sum := sums[at.Unix()]
		points = append(points, traffic.TrafficPoint{Start: at, Upload: sum[0], Download: sum[1]})
	}
	return points
}

// hourSeries zero-fills the local hours of [from, end) with the rollup rows.
func hourSeries(from, end time.Time, rows []traffic.TrafficPoint) []traffic.TrafficPoint {
	loc := from.Location()
	sums := make(map[int64][2]int64, len(rows))
	for _, row := range rows {
		key := row.Start.Unix()
		sum := sums[key]
		sums[key] = [2]int64{sum[0] + row.Upload, sum[1] + row.Download}
	}
	points := make([]traffic.TrafficPoint, 0, 32)
	for hour := from; hour.Before(end); hour = nextLocalHour(hour, loc) {
		sum := sums[hour.Unix()]
		points = append(points, traffic.TrafficPoint{Start: hour, Upload: sum[0], Download: sum[1]})
	}
	return points
}

// daySeries zero-fills the local days of [from, end) with the rollup rows.
func daySeries(from, end time.Time, loc *time.Location, rows []traffic.DayPoint) []traffic.TrafficPoint {
	sums := make(map[string][2]int64, len(rows))
	for _, row := range rows {
		sum := sums[row.Day]
		sums[row.Day] = [2]int64{sum[0] + row.Upload, sum[1] + row.Download}
	}
	points := make([]traffic.TrafficPoint, 0, 32)
	for day := localDayStart(from.In(loc)); day.Before(end); day = nextLocalDay(day) {
		sum := sums[day.Format(time.DateOnly)]
		points = append(points, traffic.TrafficPoint{Start: day, Upload: sum[0], Download: sum[1]})
	}
	return points
}

// monthSeries sums the daily rows into the local months of [from, end).
func monthSeries(from, end time.Time, loc *time.Location, rows []traffic.DayPoint) []traffic.TrafficPoint {
	sums := make(map[string][2]int64, len(rows))
	for _, row := range rows {
		if len(row.Day) < 7 {
			continue
		}
		key := row.Day[:7]
		sum := sums[key]
		sums[key] = [2]int64{sum[0] + row.Upload, sum[1] + row.Download}
	}
	points := make([]traffic.TrafficPoint, 0, 12)
	month := time.Date(from.In(loc).Year(), from.In(loc).Month(), 1, 0, 0, 0, 0, loc)
	for month.Before(end) {
		sum := sums[month.Format("2006-01")]
		points = append(points, traffic.TrafficPoint{Start: month, Upload: sum[0], Download: sum[1]})
		month = month.AddDate(0, 1, 0)
	}
	return points
}
