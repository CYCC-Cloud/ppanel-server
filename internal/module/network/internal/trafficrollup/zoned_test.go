package trafficrollup

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// zonedRollup answers the daily reads with stored days and the rest from the
// embedded fake, so a fallback can be checked against what it stored.
type zonedRollup struct {
	fakeRollup
	days []traffic.DayPoint
}

func (z *zonedRollup) DayPoints(context.Context, int64, int64, string, string) ([]traffic.DayPoint, error) {
	return z.days, nil
}

// appZone sets the application zone for one test.
func appZone(t *testing.T, name string) {
	t.Helper()
	previous := timeutil.Location().String()
	if err := timeutil.LoadLocation(name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = timeutil.LoadLocation(previous) })
}

// hourlyTraffic returns one row per elapsed hour of [from, to), each holding
// the hour's seconds, so a bucket's total is exactly its elapsed time.
func hourlyTraffic(from, to time.Time) []traffic.TrafficPoint {
	var rows []traffic.TrafficPoint
	for at := from; at.Before(to); at = at.Add(time.Hour) {
		rows = append(rows, traffic.TrafficPoint{Start: at, Upload: int64(time.Hour / time.Second)})
	}
	return rows
}

// A day bucket in another zone is its local midnight to the next, however
// many elapsed hours that spans: 23 on a spring-forward day, 25 on a
// fall-back day, 24 otherwise.
func TestDayBucketsFollowTheRequestedZone(t *testing.T) {
	appZone(t, "UTC")
	for _, tc := range []struct {
		name  string
		zone  string
		day   time.Time
		hours int64
	}{
		{name: "london spring forward", zone: "Europe/London", day: inZone(t, "Europe/London", 3, 29, 0, 0), hours: 23},
		{name: "london fall back", zone: "Europe/London", day: inZone(t, "Europe/London", 10, 25, 0, 0), hours: 25},
		{name: "new york summer day", zone: "America/New_York", day: inZone(t, "America/New_York", 6, 1, 0, 0), hours: 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			end := tc.day.AddDate(0, 0, 1)
			service := NewService(Deps{Rollup: &fakeRollup{hours: hourlyTraffic(tc.day, end)}})
			points, zone, err := service.UserHistory(context.Background(), 1, 7, GranularityDay, tc.day, end, tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			if zone != tc.zone {
				t.Fatalf("timezone = %q, want %q", zone, tc.zone)
			}
			if len(points) != 1 {
				t.Fatalf("the day series has %d buckets, want 1", len(points))
			}
			if want := tc.hours * int64(time.Hour/time.Second); points[0].Upload != want {
				t.Fatalf("the day holds %d, want its %d elapsed hours (%d)", points[0].Upload, tc.hours, want)
			}
			if !points[0].Start.Equal(tc.day) {
				t.Fatalf("the bucket starts at %s, want the local midnight %s", points[0].Start, tc.day)
			}
		})
	}
}

// Month buckets are the requested zone's calendar months: a row on the
// zone's previous month's last evening still belongs to that month.
func TestMonthBucketsFollowTheRequestedZone(t *testing.T) {
	appZone(t, "UTC")
	start, end := inZone(t, "America/New_York", 1, 1, 0, 0), inZone(t, "America/New_York", 3, 1, 0, 0)
	service := NewService(Deps{Rollup: &fakeRollup{hours: []traffic.TrafficPoint{
		{Start: start, Upload: 0}, // 1 Jan 00:00 New York
		{Start: time.Date(2026, 1, 31, 23, 0, 0, 0, time.UTC), Upload: 10}, // 31 Jan 18:00 New York
		{Start: time.Date(2026, 2, 1, 4, 0, 0, 0, time.UTC), Upload: 20},   // 31 Jan 23:00 New York
		{Start: time.Date(2026, 2, 1, 5, 0, 0, 0, time.UTC), Upload: 40},   // 1 Feb 00:00 New York
	}}})

	points, zone, err := service.UserHistory(context.Background(), 1, 7, GranularityMonth, start, end, "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	if zone != "America/New_York" {
		t.Fatalf("timezone = %q", zone)
	}
	if len(points) != 2 || points[0].Upload != 30 || points[1].Upload != 40 {
		t.Fatalf("the month series = %+v, want January 30 and February 40", points)
	}
	if !points[1].Start.Equal(inZone(t, "America/New_York", 2, 1, 0, 0)) {
		t.Fatalf("February starts at %s", points[1].Start)
	}
}

// The application zone's own name is the same as no tz at all: the daily
// table answers and the echoed zone is the application's.
func TestZoneEqualToTheApplicationZoneUsesTheDailyTable(t *testing.T) {
	appZone(t, "Europe/Paris")
	rollup := &zonedRollup{days: []traffic.DayPoint{
		{Day: "2026-11-25", Upload: 10, Download: 1},
		{Day: "2026-11-26", Upload: 5, Download: 2},
	}}
	service := NewService(Deps{Rollup: rollup})
	start, end := inZone(t, "Europe/Paris", 11, 25, 0, 0), inZone(t, "Europe/Paris", 11, 27, 0, 0)

	named, zone, err := service.UserHistory(context.Background(), 1, 7, GranularityDay, start, end, "Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	plain, plainZone, err := service.UserHistory(context.Background(), 1, 7, GranularityDay, start, end, "")
	if err != nil {
		t.Fatal(err)
	}
	if zone != "Europe/Paris" || plainZone != zone {
		t.Fatalf("timezone = %q and %q", zone, plainZone)
	}
	if len(named) != len(plain) || named[0].Upload != plain[0].Upload || named[1].Upload != plain[1].Upload {
		t.Fatalf("tz %s = %+v, no tz = %+v", zone, named, plain)
	}
}

// Minute and hour buckets are absolute instants, so a tz only names the zone
// to label them with.
func TestInstantSeriesEchoTheRequestedZone(t *testing.T) {
	appZone(t, "UTC")
	start := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	service := NewService(Deps{Rollup: &fakeRollup{hours: hourlyTraffic(start, start.Add(2*time.Hour))}})

	points, zone, err := service.UserHistory(context.Background(), 1, 7, GranularityHour, start, start.Add(2*time.Hour), "Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	if zone != "Europe/London" {
		t.Fatalf("timezone = %q", zone)
	}
	if len(points) != 2 || points[0].Start.Unix() != start.Unix() || points[0].Upload != int64(time.Hour/time.Second) {
		t.Fatalf("the hour series = %+v", points)
	}
}

// A zone name that is unknown, not a name (Local), or a path is refused.
func TestHistoryRejectsAnUnusableZone(t *testing.T) {
	appZone(t, "UTC")
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 2)
	service := NewService(Deps{Rollup: &fakeRollup{hours: hourlyTraffic(start, end)}})

	for _, tz := range []string{"Mars/Olympus", "Local", "/etc/passwd", "../../etc/passwd", strings.Repeat("a", maxTimeZoneName+1)} {
		t.Run(tz, func(t *testing.T) {
			if _, _, err := service.UserHistory(context.Background(), 1, 7, GranularityDay, start, end, tz); xerr.CodeOf(err) != xerr.InvalidParams {
				t.Fatalf("tz %q = %v, want InvalidParams", tz, err)
			}
		})
	}
}

// A zone whose offset is not a whole number of hours would split an hourly
// row, so it is refused rather than approximated.
func TestHistoryRejectsAZoneOffTheWholeHour(t *testing.T) {
	appZone(t, "UTC")
	for _, tz := range []string{"Asia/Kolkata", "Asia/Kathmandu"} {
		t.Run(tz, func(t *testing.T) {
			start, end := inZone(t, tz, 11, 1, 0, 0), inZone(t, tz, 11, 3, 0, 0)
			service := NewService(Deps{Rollup: &fakeRollup{hours: hourlyTraffic(start, end)}})
			if _, _, err := service.UserHistory(context.Background(), 1, 7, GranularityDay, start, end, tz); xerr.CodeOf(err) != xerr.InvalidParams {
				t.Fatalf("tz %q = %v, want InvalidParams", tz, err)
			}
		})
	}
}

// A range older than the hourly table serves the whole request in the
// application zone from the daily rows, echoing that zone's name.
func TestOldRangeFallsBackToTheApplicationZone(t *testing.T) {
	appZone(t, "Asia/Shanghai")
	start, end := inZone(t, "Europe/London", 11, 1, 0, 0), inZone(t, "Europe/London", 11, 3, 0, 0)
	rollup := &zonedRollup{
		fakeRollup: fakeRollup{hours: hourlyTraffic(start.AddDate(0, 0, 20), end.AddDate(0, 0, 20))},
		days:       []traffic.DayPoint{{Day: "2026-11-01", Upload: 7}},
	}
	service := NewService(Deps{Rollup: rollup})

	points, zone, err := service.UserHistory(context.Background(), 1, 7, GranularityDay, start, end, "Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	if zone != timeutil.LocationName() || zone != "Asia/Shanghai" {
		t.Fatalf("timezone = %q, want the application zone", zone)
	}
	// The range spans three Shanghai days, and the stored day keeps its
	// amount at the first of them.
	if len(points) != 3 || points[0].Upload != 7 || points[1].Upload != 0 {
		t.Fatalf("the fallback series = %+v", points)
	}
}
