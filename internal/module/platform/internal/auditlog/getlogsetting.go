package auditlog

import (
	"context"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
)

// GetLogSetting returns the stored log retention settings, with the traffic
// history's defaults applied to an unset value: it reports what the rollup
// actually applies.
func (s *Service) GetLogSetting(ctx context.Context) (*dto.LogSetting, error) {
	configs, err := s.deps.System.GetLogConfig(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetLogSetting] Database query error", logger.Field("error", err.Error()))
		return nil, err
	}
	resp := &dto.LogSetting{}
	config.SystemConfigSliceReflectToStruct(configs, resp)
	stored := config.Log{}
	if resp.TrafficHourlyRetentionDays != nil {
		stored.TrafficHourlyRetentionDays = *resp.TrafficHourlyRetentionDays
	}
	if resp.TrafficDailyRetentionDays != nil {
		stored.TrafficDailyRetentionDays = *resp.TrafficDailyRetentionDays
	}
	hourly := stored.HourlyTrafficRetentionDays()
	daily := stored.DailyTrafficRetentionDays()
	resp.TrafficHourlyRetentionDays = &hourly
	resp.TrafficDailyRetentionDays = &daily
	return resp, nil
}
