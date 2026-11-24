package serverapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/perfect-panel/backend/pkg/timeutil"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/internal/trafficagg"
	"github.com/perfect-panel/backend/pkg/logger"
)

// ServerPushUserTraffic adds a listener's report to the aggregation pipeline,
// which bills it to the subscriptions in batches. The resolved listener names
// the protocol type the report is scoped and priced by.
func (s *Service) ServerPushUserTraffic(ctx context.Context, req *dto.ServerPushUserTrafficRequest) error {
	log := logger.WithContext(ctx)
	serverInfo, listener, err := s.resolveServerListener(ctx, req.ServerId, req.ServerCommon)
	if err != nil {
		log.Errorw("[ServerPushUserTraffic] resolve listener error",
			logger.Field("listener_key", req.ListenerKey),
			logger.Field("protocol", req.Protocol),
			logger.Field("error", err.Error()))
		return fmt.Errorf("resolve listener: %w", err)
	}
	if !listener.Enable {
		return fmt.Errorf("listener %s is disabled", listener.ListenerKey)
	}
	// Upstream prices a report by the first protocol of its type. Scoping the
	// server to the resolved listener keeps each same-type listener's ratio.
	// This is an in-memory view, not a write: a listener that predates
	// listener identity has no key yet and must still be priced.
	scoped, err := json.Marshal([]node.Protocol{listener})
	if err != nil {
		return fmt.Errorf("scope listener %s: %w", listener.ListenerKey, err)
	}
	priced := *serverInfo
	priced.Protocols = string(scoped)

	aggregator := trafficagg.New(trafficagg.Deps{
		Usage:         s.deps.TrafficUsage,
		Subscriptions: s.deps.Subscriptions,
		Reports:       s.deps.Reports,
		Redis:         s.deps.Redis,
		Multiplier:    s.deps.Multiplier,
		// A node may only bill the subscriptions its user list hands it.
		ServedSubscriptions: s.servedSubscriptionIDs,
	})
	logs := dtoTrafficToAggregator(req.Traffic)
	if req.ReportID != "" {
		err = aggregator.AdmitIdentified(ctx, &priced, listener.Type, req.ReportID, logs, timeutil.Now())
	} else {
		err = aggregator.AddReport(ctx, &priced, listener.Type, logs)
	}
	if err != nil {
		log.Errorw("[ServerPushUserTraffic] Aggregate traffic error", logger.Field("error", err.Error()))
		return fmt.Errorf("aggregate traffic: %w", err)
	}
	return nil
}

// dtoTrafficToAggregator converts the reported entries into the pipeline's
// form.
func dtoTrafficToAggregator(items []dto.UserTraffic) []trafficagg.UserTraffic {
	if len(items) == 0 {
		return nil
	}
	result := make([]trafficagg.UserTraffic, 0, len(items))
	for _, item := range items {
		result = append(result, trafficagg.UserTraffic{
			SID:      item.SID,
			Upload:   item.Upload,
			Download: item.Download,
		})
	}
	return result
}
