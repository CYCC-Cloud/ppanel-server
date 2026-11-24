package serverapi

import (
	"context"
	"errors"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/internal/trafficagg"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// ServerPushStatus records a server's status report and heartbeat. A changed
// certificate pin the node reports for its protocol is stored too, and the
// server's caches that carry the old pin are dropped.
func (s *Service) ServerPushStatus(ctx context.Context, req *dto.ServerPushStatusRequest) error {
	log := logger.WithContext(ctx)
	serverInfo, err := s.deps.Servers.FindOneServer(ctx, req.ServerId)
	if err != nil || serverInfo.Id <= 0 {
		log.Errorw("[ServerPushStatus] FindOne error", logger.Field("error", err))
		return errors.New("server not found")
	}
	err = s.deps.Status.UpdateStatusCache(ctx, req.ServerId, &node.Status{
		Cpu:       req.Cpu,
		Mem:       req.Mem,
		Disk:      req.Disk,
		UpdatedAt: req.UpdatedAt,
	})
	if err != nil {
		log.Errorw("[ServerPushStatus] UpdateNodeStatus error", logger.Field("error", err))
		return errors.New("update node status failed")
	}
	now := timeutil.Now()
	if err := trafficagg.New(trafficagg.Deps{Redis: s.deps.Redis}).RecordServerReport(ctx, req.ServerId, now); err != nil {
		log.Errorw("[ServerPushStatus] RecordServerReport error", logger.Field("error", err))
		return errors.New("update node report time failed")
	}

	// Certificate metadata is exceptional: most heartbeats have nothing to
	// persist after their status and last-seen values reach Redis.
	certPinChanged := false
	currentProtocols := serverInfo.Protocols
	if req.CertPinSHA256 != "" {
		protocols, unmarshalErr := serverInfo.UnmarshalProtocols()
		if unmarshalErr != nil {
			log.Errorw("[ServerPushStatus] UnmarshalProtocols error", logger.Field("error", unmarshalErr))
			return nil
		}
		// The fingerprint belongs to the reporting listener alone; resolving
		// by protocol type would stamp it onto a sibling listener too.
		listener, resolveErr := resolveRequestListener(protocols, req.ServerCommon)
		if resolveErr != nil {
			log.Errorw("[ServerPushStatus] resolve listener error",
				logger.Field("listener_key", req.ListenerKey),
				logger.Field("protocol", req.Protocol),
				logger.Field("error", resolveErr.Error()),
			)
			return nil
		}
		certPinChanged, err = serverInfo.ApplyReportedCertPin(listener.ListenerKey, req.CertPinSHA256)
		if err != nil {
			log.Errorw("[ServerPushStatus] ApplyReportedCertPin error", logger.Field("error", err))
			certPinChanged = false
		}
	}

	if certPinChanged {
		updated, updateErr := s.deps.Status.UpdateServerProtocolsIfCurrent(ctx, serverInfo.Id, currentProtocols, serverInfo.Protocols)
		if updateErr != nil {
			log.Errorw("[ServerPushStatus] UpdateServerProtocols error", logger.Field("error", updateErr))
			return errors.New("update node certificate metadata failed")
		}
		if !updated {
			// An administrator changed the protocol configuration after this
			// heartbeat read it. The next heartbeat will apply the fingerprint to
			// the fresh value instead of overwriting that change.
			return nil
		}
		if err := s.deps.Status.ClearServerCache(ctx, req.ServerId); err != nil {
			log.Errorw("[ServerPushStatus] ClearServerCache error", logger.Field("error", err))
		}
	}

	return nil
}
