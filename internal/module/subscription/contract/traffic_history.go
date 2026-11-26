package dto

// TrafficHistoryRequest selects one subscription's traffic history series.
// Start and End are Unix seconds; the series covers [start, end). Timezone is
// an optional IANA name: empty keeps the application's zone, and day and month
// buckets follow it otherwise.
type TrafficHistoryRequest struct {
	UserSubscribeId int64  `form:"user_subscribe_id" validate:"required,gt=0"`
	Granularity     string `form:"granularity" validate:"required,oneof=minute hour day month"`
	Start           int64  `form:"start" validate:"required,gt=0"`
	End             int64  `form:"end" validate:"required,gtfield=Start"`
	Timezone        string `form:"tz" validate:"omitempty,max=64"`
}

// TrafficHistoryPoint is one bucket of the series: its start in Unix
// seconds, and the traffic in it.
type TrafficHistoryPoint struct {
	Time     int64 `json:"time"`
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
}

// TrafficHistoryResponse is the zero-filled series plus the time zone its
// bucket starts are in.
type TrafficHistoryResponse struct {
	Timezone string                `json:"timezone"`
	List     []TrafficHistoryPoint `json:"list"`
}

// TrafficServersRequest selects one subscription's traffic per server over
// [start, end); both bounds are Unix seconds.
type TrafficServersRequest struct {
	UserSubscribeId int64 `form:"user_subscribe_id" validate:"required,gt=0"`
	Start           int64 `form:"start" validate:"required,gt=0"`
	End             int64 `form:"end" validate:"required,gtfield=Start"`
}

// TrafficServerItem is one server's share of the subscription's traffic,
// labelled with the plan's node names on that server and its location. The
// server's own name and address are never exposed.
type TrafficServerItem struct {
	ServerId int64    `json:"server_id"`
	Nodes    []string `json:"nodes"`
	Country  string   `json:"country"`
	City     string   `json:"city"`
	Upload   int64    `json:"upload"`
	Download int64    `json:"download"`
	Total    int64    `json:"total"`
}

// TrafficServersResponse lists the servers largest first.
type TrafficServersResponse struct {
	Timezone string              `json:"timezone"`
	List     []TrafficServerItem `json:"list"`
}
