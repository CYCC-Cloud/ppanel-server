package adminserver

import (
	"context"
	"fmt"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/internal/geolocation"
	"github.com/perfect-panel/backend/internal/module/network/internal/protocolmap"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateServer stores a server's settings and protocols and drops its
// node-facing caches. A protocol keeps the stored values of the fields the
// request left out, and its stored keys unless new ones are given.
func (s *Service) UpdateServer(ctx context.Context, req *dto.UpdateServerRequest) error {
	log := logger.WithContext(ctx)
	nodeStore := s.deps.Store.Node()
	data, err := nodeStore.FindOneServer(ctx, req.Id)
	if err != nil {
		log.Errorf("[UpdateServer] FindOneServer Error: %v", err.Error())
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find server error: %v", err.Error())
	}
	data.Name = req.Name
	data.Capacity = req.Capacity
	data.Country = req.Country
	data.City = req.City
	if req.Address != data.Address {
		// A new address is located again: the lookup's country and city
		// replace the submitted ones unless it fails.
		result, err := geolocation.GetRegionByIp(ctx, req.Address)
		if err != nil {
			log.Errorf("[UpdateServer] GetRegionByIp Error: %v", err.Error())
		} else {
			data.City = result.City
			data.Country = result.Country
		}
		data.Address = req.Address
	}
	existingProtocols, err := data.UnmarshalProtocols()
	if err != nil {
		log.Errorf("[UpdateServer] Unmarshal Protocols Error: %v", err.Error())
		return fmt.Errorf("protocols unmarshal error: %w: %w", err, xerr.NewErrCodeMsg(xerr.InvalidParams, "protocols unmarshal error"))
	}
	existingKeys := protocolKeyLookup(existingProtocols)
	existingServerKeys := serverKeyLookup(existingProtocols)
	existingRealityKeys := realityProtocolKeyLookup(existingProtocols)
	existingListeners := listenerLookup(existingProtocols)
	existingByType := make(map[string][]node.Protocol, len(existingProtocols))
	for _, existing := range existingProtocols {
		typeName := normalizedProtocolType(existing.Type)
		existingByType[typeName] = append(existingByType[typeName], existing)
	}
	requestTypeCounts := make(map[string]int, len(req.Protocols))
	requestedListenerKeys := make(map[string]bool, len(req.Protocols))
	for _, item := range req.Protocols {
		requestTypeCounts[normalizedProtocolType(item.Type)]++
		if item.ListenerKey != "" {
			requestedListenerKeys[item.ListenerKey] = true
		}
	}
	protocols := make([]node.Protocol, 0)
	for index, item := range req.Protocols {
		if item.Type == "" {
			return fmt.Errorf("protocols type is empty: %w", xerr.NewErrCodeMsg(xerr.InvalidParams, "protocols type is empty"))
		}
		protocol, err := protocolmap.FromDTO(item)
		if err != nil {
			return err
		}
		if protocol.ListenerKey == "" {
			typeName := normalizedProtocolType(item.Type)
			if err := adoptListenerKey(&protocol, existingByType[typeName], requestTypeCounts[typeName], requestedListenerKeys); err != nil {
				return err
			}
		}
		ensureListenerKey(&protocol)
		if existing, ok := existingListeners[protocol.ListenerKey]; ok {
			protocol, err = mergeMissingProtocolFields(protocol, existing, protocolFieldSetAt(req.ProtocolFieldSets, index))
			if err != nil {
				return fmt.Errorf("protocols merge error: %w: %w", err, xerr.NewErrCodeMsg(xerr.InvalidParams, "protocols merge error"))
			}
		}
		ensureGeneratedProtocolKey(&protocol, existingKeys)
		ensureShadowsocks2022ServerKey(&protocol, existingServerKeys)
		if err := ensureRealityProtocolKey(&protocol, existingRealityKeys); err != nil {
			log.Errorf("[UpdateServer] Generate Reality Key Error: %v", err.Error())
			return xerr.Wrapf(err, xerr.ERROR, "generate reality key error: %v", err)
		}
		ensureRealityProtocolDefaults(&protocol)
		protocol, err = node.NormalizeProtocolForStorage(protocol)
		if err != nil {
			return fmt.Errorf("protocols normalize error: %w: %w", err, xerr.NewErrCodeMsg(xerr.InvalidParams, err.Error()))
		}
		protocols = append(protocols, protocol)
	}
	if err := data.MarshalProtocols(protocols); err != nil {
		log.Errorf("[UpdateServer] Marshal Protocols Error: %v", err.Error())
		return fmt.Errorf("protocols marshal error: %w: %w", err, xerr.NewErrCodeMsg(xerr.InvalidParams, "protocols marshal error"))
	}

	if err := nodeStore.UpdateServer(ctx, data); err != nil {
		log.Errorf("[UpdateServer] UpdateServer Error: %v", err.Error())
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update server error: %v", err.Error())
	}

	return nodeStore.ClearServerCache(ctx, req.Id)
}
