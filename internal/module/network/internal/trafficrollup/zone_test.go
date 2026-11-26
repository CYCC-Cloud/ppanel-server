package trafficrollup

import (
	"context"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// fakeRollup records what a run asks of the rollup repository and answers
// with one byte per elapsed second, so a day's total is its elapsed seconds.
type fakeRollup struct {
	logStart time.Time
	hours    []traffic.TrafficPoint
	windows  [][2]time.Time
	days     map[string][]traffic.TrafficSum
	dayCalls []string
}

var _ interface {
	OldestTrafficLog(context.Context) (time.Time, bool, error)
	LatestHourlyStart(context.Context) (time.Time, bool, error)
	OldestHourlyStart(context.Context) (time.Time, bool, error)
	TrafficLogSums(context.Context, time.Time, time.Time) ([]traffic.TrafficSum, error)
	ReplaceHourly(context.Context, time.Time, []traffic.TrafficSum) error
	HourlySums(context.Context, time.Time, time.Time) ([]traffic.TrafficSum, error)
	ReplaceDaily(context.Context, string, []traffic.TrafficSum) error
	DeleteBeforeBatch(context.Context, time.Time, int) (int64, error)
	DeleteDailyBeforeBatch(context.Context, string, int) (int64, error)
	TrafficLogPoints(context.Context, int64, int64, time.Time, time.Time) ([]traffic.TrafficPoint, error)
	HourPoints(context.Context, int64, int64, time.Time, time.Time) ([]traffic.TrafficPoint, error)
	DayPoints(context.Context, int64, int64, string, string) ([]traffic.DayPoint, error)
	ServerHourPoints(context.Context, int64, int64, time.Time, time.Time) ([]traffic.ServerTraffic, error)
	ServerDayPoints(context.Context, int64, int64, string, string) ([]traffic.ServerTraffic, error)
} = (*fakeRollup)(nil)

func (f *fakeRollup) OldestTrafficLog(context.Context) (time.Time, bool, error) {
	return f.logStart, !f.logStart.IsZero(), nil
}

func (f *fakeRollup) LatestHourlyStart(context.Context) (time.Time, bool, error) {
	if len(f.hours) == 0 {
		return time.Time{}, false, nil
	}
	return f.hours[len(f.hours)-1].Start, true, nil
}

func (f *fakeRollup) OldestHourlyStart(context.Context) (time.Time, bool, error) {
	if len(f.hours) == 0 {
		return time.Time{}, false, nil
	}
	return f.hours[0].Start, true, nil
}

func (f *fakeRollup) TrafficLogSums(_ context.Context, start, end time.Time) ([]traffic.TrafficSum, error) {
	f.windows = append(f.windows, [2]time.Time{start, end})
	return []traffic.TrafficSum{{
		UserSubscribeId: 1,
		UserId:          7,
		ServerId:        1,
		Upload:          int64(end.Sub(start) / time.Second),
	}}, nil
}

func (f *fakeRollup) ReplaceHourly(_ context.Context, hourStart time.Time, rows []traffic.TrafficSum) error {
	var upload int64
	for _, row := range rows {
		upload += row.Upload
	}
	f.hours = append(f.hours, traffic.TrafficPoint{Start: hourStart, Upload: upload})
	return nil
}

func (f *fakeRollup) HourlySums(_ context.Context, start, end time.Time) ([]traffic.TrafficSum, error) {
	var rows []traffic.TrafficSum
	for _, hour := range f.hours {
		if !hour.Start.Before(start) && hour.Start.Before(end) {
			rows = append(rows, traffic.TrafficSum{UserSubscribeId: 1, UserId: 7, ServerId: 1, Upload: hour.Upload})
		}
	}
	// The repository groups by subscription, user and server.
	grouped := make([]traffic.TrafficSum, 0, 1)
	for _, row := range rows {
		if len(grouped) == 0 {
			grouped = append(grouped, row)
			continue
		}
		grouped[0].Upload += row.Upload
		grouped[0].Download += row.Download
	}
	return grouped, nil
}

func (f *fakeRollup) ReplaceDaily(_ context.Context, day string, rows []traffic.TrafficSum) error {
	if f.days == nil {
		f.days = make(map[string][]traffic.TrafficSum)
	}
	f.days[day] = rows
	f.dayCalls = append(f.dayCalls, day)
	return nil
}

func (f *fakeRollup) DeleteBeforeBatch(context.Context, time.Time, int) (int64, error) { return 0, nil }

func (f *fakeRollup) DeleteDailyBeforeBatch(context.Context, string, int) (int64, error) {
	return 0, nil
}

func (f *fakeRollup) TrafficLogPoints(context.Context, int64, int64, time.Time, time.Time) ([]traffic.TrafficPoint, error) {
	return nil, nil
}

func (f *fakeRollup) HourPoints(_ context.Context, _, _ int64, start, end time.Time) ([]traffic.TrafficPoint, error) {
	var points []traffic.TrafficPoint
	for _, hour := range f.hours {
		if !hour.Start.Before(start) && hour.Start.Before(end) {
			points = append(points, hour)
		}
	}
	return points, nil
}

func (f *fakeRollup) DayPoints(context.Context, int64, int64, string, string) ([]traffic.DayPoint, error) {
	return nil, nil
}

func (f *fakeRollup) ServerHourPoints(context.Context, int64, int64, time.Time, time.Time) ([]traffic.ServerTraffic, error) {
	return nil, nil
}

func (f *fakeRollup) ServerDayPoints(context.Context, int64, int64, string, string) ([]traffic.ServerTraffic, error) {
	return nil, nil
}

func inZone(t *testing.T, zone string, month time.Month, day, hour, minute int) time.Time {
	t.Helper()
	location, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(2026, month, day, hour, minute, 0, 0, location)
}

// runInZone runs the rollup with the application zone set to zone, from the
// oldest raw log row to now, and returns the fake it drove.
func runInZone(t *testing.T, zone string, logStart, now time.Time) *fakeRollup {
	t.Helper()
	previous := timeutil.Location().String()
	if err := timeutil.LoadLocation(zone); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = timeutil.LoadLocation(previous) })

	rollup := &fakeRollup{logStart: logStart}
	service := NewService(Deps{Rollup: rollup, Limits: func() Limits { return Limits{HourlyDays: 90} }})
	if err := service.RunAt(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	return rollup
}

// midnight is the start of the local day holding at.
func midnight(at time.Time) time.Time {
	return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
}

// A fall-back day keeps both segments of its repeated hour: the buckets are
// one per local hour, one of them spans two elapsed hours, and the day's
// total holds all 25.
func TestFallBackDayKeepsEveryHour(t *testing.T) {
	now := inZone(t, "Europe/Paris", 10, 25, 23, 30)
	rollup := runInZone(t, "Europe/Paris", midnight(now), now)

	if len(rollup.windows) != 24 {
		t.Fatalf("%d hourly buckets, want 24 distinct local hours", len(rollup.windows))
	}
	var elapsed time.Duration
	longest := time.Duration(0)
	for i, window := range rollup.windows {
		elapsed += window[1].Sub(window[0])
		if window[1].Sub(window[0]) > longest {
			longest = window[1].Sub(window[0])
		}
		if i > 0 && !window[0].Equal(rollup.windows[i-1][1]) {
			t.Fatalf("windows are not contiguous at %s", window[0])
		}
		if window[0].Hour() == window[1].Hour() && window[0].Day() == window[1].Day() {
			t.Fatalf("a window does not advance the local hour: %s..%s", window[0], window[1])
		}
	}
	if elapsed != 25*time.Hour {
		t.Fatalf("a fall-back day elapsed %s, want 25h", elapsed)
	}
	if longest != 2*time.Hour {
		t.Fatalf("the longest bucket is %s, want the repeated hour's 2h", longest)
	}

	last := rollup.windows[len(rollup.windows)-1][1]
	if want := inZone(t, "Europe/Paris", 10, 26, 0, 0); !last.Equal(want) {
		t.Fatalf("the last window ends at %s, want %s", last, want)
	}
	total := int64(0)
	for _, row := range rollup.days["2026-10-25"] {
		total += row.Upload
	}
	if total != int64(25*time.Hour/time.Second) {
		t.Fatalf("the day's total is %d, want 25 hours of traffic", total)
	}
	if len(rollup.dayCalls) != 1 || rollup.dayCalls[0] != "2026-10-25" {
		t.Fatalf("recomputed days = %v, want only 2026-10-25", rollup.dayCalls)
	}

	// The history series covers the same 24 buckets and totals the same day.
	service := NewService(Deps{Rollup: rollup})
	points, err := service.UserHistory(context.Background(), 1, 7, GranularityHour,
		inZone(t, "Europe/Paris", 10, 25, 0, 0), inZone(t, "Europe/Paris", 10, 26, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 24 {
		t.Fatalf("the hour series has %d buckets, want 24", len(points))
	}
	seriesTotal := int64(0)
	for _, point := range points {
		seriesTotal += point.Upload
	}
	if seriesTotal != int64(25*time.Hour/time.Second) {
		t.Fatalf("the hour series totals %d, want the day's 25 hours", seriesTotal)
	}
}

// A spring-forward day loses its skipped hour: 23 buckets and 23 elapsed
// hours.
func TestSpringForwardDaySkipsTheMissingHour(t *testing.T) {
	now := inZone(t, "Europe/Paris", 3, 29, 23, 30)
	rollup := runInZone(t, "Europe/Paris", midnight(now), now)

	if len(rollup.windows) != 23 {
		t.Fatalf("%d hourly buckets, want 23", len(rollup.windows))
	}
	var elapsed time.Duration
	for _, window := range rollup.windows {
		elapsed += window[1].Sub(window[0])
	}
	if elapsed != 23*time.Hour {
		t.Fatalf("a spring-forward day elapsed %s, want 23h", elapsed)
	}
	total := int64(0)
	for _, row := range rollup.days["2026-03-29"] {
		total += row.Upload
	}
	if total != int64(23*time.Hour/time.Second) {
		t.Fatalf("the day's total is %d, want 23 hours of traffic", total)
	}
}

// The local date decides the day table, not UTC: a report at 23:30 in
// Shanghai belongs to the previous day in UTC but to that local day here.
func TestDayIsTheApplicationDaysNotUTCs(t *testing.T) {
	now := inZone(t, "Asia/Shanghai", 11, 26, 0, 30)
	rollup := runInZone(t, "Asia/Shanghai", inZone(t, "Asia/Shanghai", 11, 25, 22, 10), now)

	if len(rollup.dayCalls) != 2 || rollup.dayCalls[0] != "2026-11-25" || rollup.dayCalls[1] != "2026-11-26" {
		t.Fatalf("recomputed days = %v, want 2026-11-25 and 2026-11-26", rollup.dayCalls)
	}
	// 22:00, 23:00 and 00:00 local: the day starts at its own midnight, and
	// the early hour of the next day is a separate bucket.
	if len(rollup.windows) != 3 {
		t.Fatalf("%d hourly buckets, want 3", len(rollup.windows))
	}
	if want := inZone(t, "Asia/Shanghai", 11, 25, 23, 0); !rollup.windows[1][0].Equal(want) {
		t.Fatalf("the 23:00 bucket starts at %s, want %s", rollup.windows[1][0], want)
	}
	if len(rollup.days["2026-11-25"]) != 1 || rollup.days["2026-11-25"][0].Upload != int64(2*time.Hour/time.Second) {
		t.Fatalf("2026-11-25 holds %+v, want its two hours", rollup.days["2026-11-25"])
	}
}

// The month series sums the daily rows into local calendar months and
// zero-fills the months between them.
func TestMonthSeriesZeroFillsAndSumsDays(t *testing.T) {
	previous := timeutil.Location().String()
	if err := timeutil.LoadLocation("UTC"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = timeutil.LoadLocation(previous) })

	rollup := &dayRollup{days: []traffic.DayPoint{
		{Day: "2026-01-15", Upload: 10, Download: 1},
		{Day: "2026-01-31", Upload: 5, Download: 2},
		{Day: "2026-03-02", Upload: 7, Download: 3},
	}}
	service := NewService(Deps{Rollup: rollup})
	points, err := service.UserHistory(context.Background(), 1, 7, GranularityMonth,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 3 {
		t.Fatalf("the month series has %d buckets, want 3", len(points))
	}
	if points[0].Upload != 15 || points[0].Download != 3 {
		t.Fatalf("January = %+v", points[0])
	}
	if points[1].Upload != 0 || points[1].Download != 0 {
		t.Fatalf("February is not zero-filled: %+v", points[1])
	}
	if points[2].Upload != 7 || points[2].Download != 3 {
		t.Fatalf("March = %+v", points[2])
	}

	days, err := service.UserHistory(context.Background(), 1, 7, GranularityDay,
		time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 || days[0].Upload != 5 || days[1].Upload != 0 {
		t.Fatalf("the day series = %+v", days)
	}
}

// dayRollup answers the daily reads and fails the rest: the series tests only
// need the stored days.
type dayRollup struct {
	fakeRollup
	days []traffic.DayPoint
}

func (d *dayRollup) DayPoints(context.Context, int64, int64, string, string) ([]traffic.DayPoint, error) {
	return d.days, nil
}
