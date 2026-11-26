package taskqueue

// The task types of the scheduled jobs.
const (
	SchedulerCheckSubscription = "scheduler:check:subscription"
	// SchedulerRemindExpiringSubscriptions warns owners whose subscription
	// expires soon. Daily, at a civil hour.
	SchedulerRemindExpiringSubscriptions = "scheduler:subscription:remind-expiring"
	// SchedulerDispatchDomainEvents pumps the generic domain-event outbox
	// onto the asynq queue as events:deliver tasks.
	SchedulerDispatchDomainEvents = "scheduler:events:dispatch"
	SchedulerResetTraffic         = "scheduler:reset:traffic"
	SchedulerTrafficStat          = "scheduler:traffic:stat"
	// SchedulerTrafficRollup rebuilds the traffic history's hourly and daily
	// aggregates from the raw traffic log.
	SchedulerTrafficRollup = "scheduler:traffic:rollup"
	SchedulerLogCleanup    = "scheduler:log:cleanup"
	SchedulerFlushTraffic  = "scheduler:flush:traffic"
)
