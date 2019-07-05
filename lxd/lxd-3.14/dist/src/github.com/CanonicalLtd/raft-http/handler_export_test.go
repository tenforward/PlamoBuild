package rafthttp

import "net"

// Connections returns a channel of net.Conn objects that a Layer can
// receive from in order to establish new raft TCP connections.
func (h *Handler) Connections() <-chan net.Conn {
	return h.connections
}
