package ws

import "go.getarcane.app/streams/logs"

var defaultLogBroadcaster = logs.New(1000)

// LogBroadcaster returns the backend-wide log broadcaster used by diagnostics.
func LogBroadcaster() *logs.Broadcaster {
	return defaultLogBroadcaster
}
