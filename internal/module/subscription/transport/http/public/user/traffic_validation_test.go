package user

import (
	"strings"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
)

// The traffic endpoints reject a request the history cannot answer before it
// reaches a module: the subscription, the granularity and an increasing
// range are all required.
func TestTrafficRequestsValidateTheirQuery(t *testing.T) {
	validHistory := dto.TrafficHistoryRequest{UserSubscribeId: 9, Granularity: "hour", Start: 1764000000, End: 1764086400}
	if err := validation.Validate(&validHistory); err != nil {
		t.Fatalf("a valid history request was rejected: %v", err)
	}
	zonedHistory := validHistory
	zonedHistory.Timezone = "Europe/London"
	if err := validation.Validate(&zonedHistory); err != nil {
		t.Fatalf("a request with tz was rejected: %v", err)
	}
	for name, request := range map[string]any{
		"missing subscription": dto.TrafficHistoryRequest{Granularity: "hour", Start: 1, End: 2},
		"missing granularity":  dto.TrafficHistoryRequest{UserSubscribeId: 9, Start: 1, End: 2},
		"unknown granularity":  dto.TrafficHistoryRequest{UserSubscribeId: 9, Granularity: "week", Start: 1, End: 2},
		"missing start":        dto.TrafficHistoryRequest{UserSubscribeId: 9, Granularity: "hour", End: 2},
		"empty range":          dto.TrafficHistoryRequest{UserSubscribeId: 9, Granularity: "hour", Start: 2, End: 2},
		"reversed range":       dto.TrafficHistoryRequest{UserSubscribeId: 9, Granularity: "hour", Start: 3, End: 2},
		"overlong tz": dto.TrafficHistoryRequest{
			UserSubscribeId: 9, Granularity: "day", Start: 1, End: 2, Timezone: strings.Repeat("a", 65),
		},
		"negative subscription": dto.TrafficHistoryRequest{
			UserSubscribeId: -1, Granularity: "minute", Start: 1, End: 2,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validation.Validate(&request); err == nil {
				t.Fatal("the request was accepted")
			}
		})
	}

	validServers := dto.TrafficServersRequest{UserSubscribeId: 9, Start: 1764000000, End: 1764086400}
	if err := validation.Validate(&validServers); err != nil {
		t.Fatalf("a valid servers request was rejected: %v", err)
	}
	for name, request := range map[string]any{
		"missing subscription": dto.TrafficServersRequest{Start: 1, End: 2},
		"reversed range":       dto.TrafficServersRequest{UserSubscribeId: 9, Start: 3, End: 2},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validation.Validate(&request); err == nil {
				t.Fatal("the request was accepted")
			}
		})
	}
}
