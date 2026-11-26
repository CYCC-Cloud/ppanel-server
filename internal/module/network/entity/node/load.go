package node

// ServerLoad is the graded state of one server as users' node lists show it:
// the report status and, unless the server is offline, its load level. The
// figures behind the level are never returned.
type ServerLoad struct {
	Status string `json:"status"`
	Load   string `json:"load,omitempty"`
}

// ServerLoadCacheKey holds a server's graded load and when it was computed,
// as JSON. Example: server:load:1
const ServerLoadCacheKey = "server:load:%d"
