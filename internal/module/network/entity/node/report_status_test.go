package node

import (
	"testing"
	"time"
)

// A server is online within three minutes of its last report, degraded up to
// five, and offline after that or when it never reported.
func TestReportStatus(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name string
		last *time.Time
		want string
	}{
		{"never reported", nil, ServerStatusOffline},
		{"just now", new(now), ServerStatusOnline},
		{"under three minutes", new(now.Add(-time.Minute)), ServerStatusOnline},
		{"over three minutes", new(now.Add(-4 * time.Minute)), ServerStatusDegraded},
		{"over five minutes", new(now.Add(-6 * time.Minute)), ServerStatusOffline},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReportStatus(tc.last); got != tc.want {
				t.Fatalf("ReportStatus = %q, want %q", got, tc.want)
			}
		})
	}
}
