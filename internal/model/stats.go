package model

import "time"

type UserUsageRow struct {
	UserID    string
	BytesUp   uint64
	BytesDown uint64
	IPCount   uint32
}

type AgentStats struct {
	AgentID       string
	UptimeSeconds uint32
	WindowEnd     time.Time
	Users         []UserUsageRow
}
