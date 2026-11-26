package node

import "time"

// The statuses a server's last report grades as. The admin list keeps its
// older "warning" label for a degraded server.
const (
	ServerStatusOnline   = "online"
	ServerStatusDegraded = "degraded"
	ServerStatusOffline  = "offline"
)

// ReportStatus grades a server by its last report: online within three
// minutes, degraded up to five, offline after that or without any report.
// The node settings' push interval is a minute, so the bounds tolerate a
// missed report.
func ReportStatus(last *time.Time) string {
	if last == nil {
		return ServerStatusOffline
	}
	age := time.Since(*last)
	if age > 5*time.Minute {
		return ServerStatusOffline
	}
	if age > 3*time.Minute {
		return ServerStatusDegraded
	}
	return ServerStatusOnline
}
