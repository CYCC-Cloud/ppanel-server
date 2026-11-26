package traffic

import "time"

// TrafficHourly is one hour of a subscription's traffic on one server. The
// rollup overwrites the hour's rows on every run, so the table holds derived
// data only. HourStart is the hour's start in the application time zone, as
// an instant: the drivers store a zone-less value, so two DST segments of the
// same local hour would collide, and the rollup merges them into one row.
type TrafficHourly struct {
	Id              int64     `gorm:"primaryKey"`
	HourStart       time.Time `gorm:"not null"`
	UserSubscribeId int64     `gorm:"not null"`
	UserId          int64     `gorm:"not null"`
	ServerId        int64     `gorm:"not null"`
	Upload          int64     `gorm:"not null;default:0"`
	Download        int64     `gorm:"not null;default:0"`
	UpdatedAt       time.Time `gorm:"not null"`
}

func (TrafficHourly) TableName() string {
	return "traffic_hourly"
}

// TrafficDaily is one local day of a subscription's traffic on one server,
// recomputed from TrafficHourly. Day is the local date as "YYYY-MM-DD": a
// DATE column would shift a local midnight into the previous day whenever
// the session zone differs from the application's.
type TrafficDaily struct {
	Id              int64     `gorm:"primaryKey"`
	Day             string    `gorm:"type:varchar(10);not null"`
	UserSubscribeId int64     `gorm:"not null"`
	UserId          int64     `gorm:"not null"`
	ServerId        int64     `gorm:"not null"`
	Upload          int64     `gorm:"not null;default:0"`
	Download        int64     `gorm:"not null;default:0"`
	UpdatedAt       time.Time `gorm:"not null"`
}

func (TrafficDaily) TableName() string {
	return "traffic_daily"
}

// TrafficSum is a subscription's traffic on one server over one rollup
// bucket.
type TrafficSum struct {
	UserSubscribeId int64
	UserId          int64
	ServerId        int64
	Upload          int64
	Download        int64
}

// TrafficPoint is one bucket of a traffic history series: the bucket's start
// and the traffic in it.
type TrafficPoint struct {
	Start    time.Time
	Upload   int64
	Download int64
}

// DayPoint is one local day's traffic.
type DayPoint struct {
	Day      string
	Upload   int64
	Download int64
}

// ServerTraffic is one server's share of a subscription's traffic.
type ServerTraffic struct {
	ServerId int64
	Upload   int64
	Download int64
}
