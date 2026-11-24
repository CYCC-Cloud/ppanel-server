package node

import "time"

const (
	// ServerCacheTTL bounds the node-facing response caches, the server
	// configs and the user lists.
	ServerCacheTTL = 5 * time.Minute

	// ServerUserListCacheKey prefixes listener-scoped cached user lists.
	ServerUserListCacheKey = "server:user:"

	// ServerProtocolUserListCacheKey prefixes the type-wide allowlist used by
	// traffic and online reports.
	ServerProtocolUserListCacheKey = "server:user:protocol:"

	// ServerConfigCacheKey prefixes the cached configs of a server.
	ServerConfigCacheKey = "server:config:"

	// ServerCacheIndexKey tracks the exact response-cache keys generated for a
	// server so invalidation does not need to scan the entire Redis keyspace.
	ServerCacheIndexKey = "server:cache:index:%d"

	// ServerCacheGenerationKey fences response-cache fills that started before
	// a server configuration mutation completed.
	ServerCacheGenerationKey = "server:cache:generation:%d"
)

// FilterParams selects a page of servers.
type FilterParams struct {
	Page   int
	Size   int
	Ids    []int64 // Server IDs
	Search string
}

// FilterNodeParams selects nodes, a page of them for the admin list.
type FilterNodeParams struct {
	Page     int      // Page Number
	Size     int      // Page Size
	NodeId   []int64  // Node IDs
	ServerId []int64  // Server IDs
	Tag      []string // Tags
	Search   string   // Search Address or Name
	// ListenerKey selects the nodes bound to one server listener. It is the
	// listener-scoped selector; Protocol only narrows by protocol type and
	// cannot separate two listeners that share one.
	ListenerKey string
	Protocol    string // Protocol
	Preload     bool   // Preload Server
	Enabled     *bool  // Enabled
}

// SortItem is a list entry's position.
type SortItem struct {
	Id   int64
	Sort int64
}
