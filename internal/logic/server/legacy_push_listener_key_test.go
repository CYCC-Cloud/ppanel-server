package server

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPushOnlineUsersRejectsMissingListenerKey(t *testing.T) {
	logic := NewPushOnlineUsersLogic(context.Background(), &svc.ServiceContext{NodeModel: testNodeModel{}})

	err := logic.PushOnlineUsers(&types.OnlineUsersRequest{
		ServerCommon: types.ServerCommon{ServerId: 1},
		Users:        []types.OnlineUser{{SID: 1, IP: "127.0.0.1"}},
	})

	require.EqualError(t, err, "listener_key is required")
}

func TestServerPushUserTrafficRejectsMissingListenerKey(t *testing.T) {
	logic := NewServerPushUserTrafficLogic(context.Background(), &svc.ServiceContext{NodeModel: testNodeModel{}})

	err := logic.ServerPushUserTraffic(&types.ServerPushUserTrafficRequest{
		ServerCommon: types.ServerCommon{ServerId: 1},
		Traffic:      []types.UserTraffic{{SID: 1, Upload: 10, Download: 20}},
	})

	require.EqualError(t, err, "listener_key is required")
}

func TestPushOnlineUsersRejectsUnknownListenerKey(t *testing.T) {
	logic := NewPushOnlineUsersLogic(context.Background(), &svc.ServiceContext{NodeModel: testNodeModel{
		server: testServerWithListener(t, "listener-a"),
	}})

	err := logic.PushOnlineUsers(&types.OnlineUsersRequest{
		ServerCommon: types.ServerCommon{ServerId: 1, ListenerKey: "missing"},
		Users:        []types.OnlineUser{{SID: 1, IP: "127.0.0.1"}},
	})

	require.EqualError(t, err, "listener_key is invalid")
}

func TestServerPushUserTrafficRejectsUnknownListenerKey(t *testing.T) {
	logic := NewServerPushUserTrafficLogic(context.Background(), &svc.ServiceContext{NodeModel: testNodeModel{
		server: testServerWithListener(t, "listener-a"),
	}})

	err := logic.ServerPushUserTraffic(&types.ServerPushUserTrafficRequest{
		ServerCommon: types.ServerCommon{ServerId: 1, ListenerKey: "missing"},
		Traffic:      []types.UserTraffic{{SID: 1, Upload: 10, Download: 20}},
	})

	require.EqualError(t, err, "listener_key is invalid")
}

func testServerWithListener(t *testing.T, listenerKey string) *node.Server {
	t.Helper()

	server := &node.Server{Id: 1}
	require.NoError(t, server.MarshalProtocols([]node.Protocol{{
		Type:        "vmess",
		ListenerKey: listenerKey,
		Port:        443,
	}}))

	return server
}

type testNodeModel struct {
	server *node.Server
	err    error
}

func (testNodeModel) InsertServer(context.Context, *node.Server, ...*gorm.DB) error {
	panic("unexpected call")
}
func (m testNodeModel) FindOneServer(context.Context, int64) (*node.Server, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.server == nil {
		return nil, errors.New("server not found")
	}
	return m.server, nil
}
func (testNodeModel) UpdateServer(context.Context, *node.Server, ...*gorm.DB) error {
	panic("unexpected call")
}
func (testNodeModel) DeleteServer(context.Context, int64, ...*gorm.DB) error {
	panic("unexpected call")
}
func (testNodeModel) Transaction(context.Context, func(*gorm.DB) error) error {
	panic("unexpected call")
}
func (testNodeModel) QueryServerList(context.Context, []int64) ([]*node.Server, error) {
	panic("unexpected call")
}
func (testNodeModel) InsertNode(context.Context, *node.Node, ...*gorm.DB) error {
	panic("unexpected call")
}
func (testNodeModel) FindOneNode(context.Context, int64) (*node.Node, error) {
	panic("unexpected call")
}
func (testNodeModel) UpdateNode(context.Context, *node.Node, ...*gorm.DB) error {
	panic("unexpected call")
}
func (testNodeModel) DeleteNode(context.Context, int64, ...*gorm.DB) error { panic("unexpected call") }
func (testNodeModel) StatusCache(context.Context, int64) (node.Status, error) {
	panic("unexpected call")
}
func (testNodeModel) UpdateStatusCache(context.Context, int64, *node.Status) error {
	panic("unexpected call")
}
func (testNodeModel) OnlineUserSubscribe(context.Context, int64, string) (node.OnlineUserSubscribe, error) {
	panic("unexpected call")
}
func (testNodeModel) UpdateOnlineUserSubscribe(context.Context, int64, string, node.OnlineUserSubscribe) error {
	panic("unexpected call")
}
func (testNodeModel) OnlineUserSubscribeGlobal(context.Context) (int64, error) {
	panic("unexpected call")
}
func (testNodeModel) UpdateOnlineUserSubscribeGlobal(context.Context, node.OnlineUserSubscribe) error {
	panic("unexpected call")
}
func (testNodeModel) FilterServerList(context.Context, *node.FilterParams) (int64, []*node.Server, error) {
	panic("unexpected call")
}
func (testNodeModel) FilterNodeList(context.Context, *node.FilterNodeParams) (int64, []*node.Node, error) {
	panic("unexpected call")
}
func (testNodeModel) ClearNodeCache(context.Context, *node.FilterNodeParams) error {
	panic("unexpected call")
}
