package auditlog

import (
	"context"
	"fmt"
	"strconv"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/entity/system"
	"github.com/perfect-panel/backend/internal/repository/kernel"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The retention an administrator may set, in days. The floor keeps the
// login, registration and subscription logs long enough to investigate an
// incident noticed days later: a retention of a day would let the 02:30
// cleanup erase the trail before anyone looked.
const (
	MinRetentionDays = 7
	MaxRetentionDays = 3650
)

// UpdateLogSetting stores the log retention settings and, once they are
// committed, propagates them to the running configuration. The change is
// recorded in the audit trail with its values, which are not secret and are
// what an investigation needs to know: who shortened the retention, and to
// what.
func (s *Service) UpdateLogSetting(ctx context.Context, req *dto.LogSetting) error {
	if err := validateLogSetting(req); err != nil {
		return err
	}
	retention := config.Log{AutoClear: *req.AutoClear, ClearDays: req.ClearDays}
	err := s.deps.Store.InPlatformTx(ctx, func(store kernel.PlatformStore) error {
		systemStore := store.System()
		stored, err := systemStore.GetLogConfig(ctx)
		if err != nil {
			return err
		}
		// An omitted retention keeps what is stored, so an older admin UI
		// does not reset a setting it does not know.
		storedRetention := storedLogSetting(ctx, stored)
		hourly := storedRetention.HourlyTrafficRetentionDays()
		daily := storedRetention.DailyTrafficRetentionDays()
		audit := storedRetention.AuditRetentionDays()
		if req.TrafficHourlyRetentionDays != nil {
			hourly = *req.TrafficHourlyRetentionDays
		}
		if req.TrafficDailyRetentionDays != nil {
			daily = *req.TrafficDailyRetentionDays
		}
		if req.AuditLogRetentionDays != nil {
			audit = *req.AuditLogRetentionDays
		}
		retention.TrafficHourlyRetentionDays = hourly
		retention.TrafficDailyRetentionDays = daily
		retention.AuditLogRetentionDays = audit

		if err := systemStore.UpdateValueByCategoryKey(ctx, "log", "AutoClear", strconv.FormatBool(*req.AutoClear), "bool"); err != nil {
			return err
		}
		if err := systemStore.UpdateValueByCategoryKey(ctx, "log", "ClearDays", strconv.FormatInt(req.ClearDays, 10), "int64"); err != nil {
			return err
		}
		if err := systemStore.UpdateValueByCategoryKey(ctx, "log", "TrafficHourlyRetentionDays", strconv.FormatInt(hourly, 10), "int64"); err != nil {
			return err
		}
		if err := systemStore.UpdateValueByCategoryKey(ctx, "log", "TrafficDailyRetentionDays", strconv.FormatInt(daily, 10), "int64"); err != nil {
			return err
		}
		if err := systemStore.UpdateValueByCategoryKey(ctx, "log", "AuditLogRetentionDays", strconv.FormatInt(audit, 10), "int64"); err != nil {
			return err
		}
		row, err := log.NewAdminActionLog(log.AdminActionFrom(ctx, log.AdminAction{
			Action: "settings.update",
			Object: "log",
			Detail: fmt.Sprintf("keys: AutoClear, ClearDays, TrafficHourlyRetentionDays, TrafficDailyRetentionDays, AuditLogRetentionDays; auto_clear=%t clear_days=%d traffic_hourly_retention_days=%d traffic_daily_retention_days=%d audit_log_retention_days=%d",
				*req.AutoClear, req.ClearDays, hourly, daily, audit),
		}))
		if err != nil {
			return err
		}
		return store.Log().Insert(ctx, row)
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("[UpdateLogSetting] update log setting error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, " update log setting error: %v", err)
	}

	if s.deps.OnLogSettingChanged != nil {
		s.deps.OnLogSettingChanged(retention)
	}
	return nil
}

// storedLogSetting decodes the stored log settings with their defaults.
func storedLogSetting(ctx context.Context, entries []*system.System) config.Log {
	retention := config.Log{
		AutoClear:                  true,
		ClearDays:                  config.DefaultClearDays,
		TrafficHourlyRetentionDays: config.DefaultTrafficHourlyRetentionDays,
		AuditLogRetentionDays:      config.DefaultAuditLogRetentionDays,
	}
	if err := config.DecodeSystemConfig(entries, &retention); err != nil {
		logger.WithContext(ctx).Errorw("[UpdateLogSetting] stored log settings could not be applied, the defaults stay",
			logger.Field("error", err.Error()))
	}
	return retention
}

func validateLogSetting(req *dto.LogSetting) error {
	if req == nil || req.AutoClear == nil || req.ClearDays < MinRetentionDays || req.ClearDays > MaxRetentionDays {
		return xerr.Errorf(xerr.InvalidParams, "log retention requires auto_clear and clear_days between %d and %d", MinRetentionDays, MaxRetentionDays)
	}
	if hourly := req.TrafficHourlyRetentionDays; hourly != nil &&
		(*hourly < config.MinTrafficHourlyRetentionDays || *hourly > config.MaxTrafficHourlyRetentionDays) {
		return xerr.Errorf(xerr.InvalidParams, "traffic hourly retention must be between %d and %d days",
			config.MinTrafficHourlyRetentionDays, config.MaxTrafficHourlyRetentionDays)
	}
	if daily := req.TrafficDailyRetentionDays; daily != nil && *daily != 0 &&
		(*daily < config.MinTrafficDailyRetentionDays || *daily > config.MaxTrafficDailyRetentionDays) {
		return xerr.Errorf(xerr.InvalidParams, "traffic daily retention must be 0 (keep forever) or between %d and %d days",
			config.MinTrafficDailyRetentionDays, config.MaxTrafficDailyRetentionDays)
	}
	if audit := req.AuditLogRetentionDays; audit != nil && *audit != 0 &&
		(*audit < config.MinAuditLogRetentionDays || *audit > config.MaxAuditLogRetentionDays) {
		return xerr.Errorf(xerr.InvalidParams, "audit log retention must be 0 (keep forever) or between %d and %d days",
			config.MinAuditLogRetentionDays, config.MaxAuditLogRetentionDays)
	}
	return nil
}
