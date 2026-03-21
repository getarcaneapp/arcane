package edge

import "github.com/gorilla/websocket"

func newWebSocketAgentTunnel(envID string, conn *websocket.Conn) *AgentTunnel {
	return NewAgentTunnelWithConn(envID, NewTunnelConn(conn))
}
