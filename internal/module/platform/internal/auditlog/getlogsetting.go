package auditlog

import (
	"context"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
)

// GetLogSetting returns the stored log retention settings, with the traffic
// history's and the audit trail's defaults applied to an unset value: it
// reports what the cleanup actually applies.
func (s *Service) GetLogSetting(ctx context.Context) (*dto.LogSetting, error) {
	configs, err := s.deps.System.GetLogConfig(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetLogSetting] Database query error", logger.Field("error", err.Error()))
		return nil, err
	}
	resp := &dto.LogSetting{}
	config.SystemConfigSliceReflectToStruct(configs, resp)
	stored := config.Log{AuditLogRetentionDays: config.DefaultAuditLogRetentionDays}
	if resp.TrafficHourlyRetentionDays != nil {
		stored.TrafficHourlyRetentionDays = *resp.TrafficHourlyRetentionDays
	}
	if resp.TrafficDailyRetentionDays != nil {
		stored.TrafficDailyRetentionDays = *resp.TrafficDailyRetentionDays
	}
	if resp.AuditLogRetentionDays != nil {
		stored.AuditLogRetentionDays = *resp.AuditLogRetentionDays
	}
	hourly := stored.HourlyTrafficRetentionDays()
	daily := stored.DailyTrafficRetentionDays()
	audit := stored.AuditRetentionDays()
	resp.TrafficHourlyRetentionDays = &hourly
	resp.TrafficDailyRetentionDays = &daily
	resp.AuditLogRetentionDays = &audit
	return resp, nil
}
