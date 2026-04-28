package model

import (
	"fmt"
	"time"
)

type OperationKind string

const (
	OpUpsert    OperationKind = "upsert"
	OpRemove    OperationKind = "remove"
	OpRenew     OperationKind = "renew"
	OpStatsAll  OperationKind = "stats_all"
	OpStatsUser OperationKind = "stats_user"
	OpHello     OperationKind = "hello"
)

type Operation struct {
	AgentID   string
	RequestID string
	Seq       uint64
	Kind      OperationKind

	Upsert    *AgentUpsertPayload
	Remove    *AgentRemovePayload
	Renew     *AgentRenewPayload
	StatsUser *StatsUserPayload
}
type AgentUpsertPayload struct {
	UserID     string    `json:"user_id"`
	DriverType string    `json:"driver"`
	ExpiresAt  time.Time `json:"expires_at"`
}
type AgentRemovePayload struct {
	UserID     string `json:"user_id"`
	DriverType string `json:"driver"`
}
type AgentRenewPayload struct {
	UserID     string    `json:"user_id"`
	DriverType string    `json:"driver"`
	ExpiresAt  time.Time `json:"expires_at"`
}
type StatsUserPayload struct {
	UserID string `json:"user_id"`
}

func (o *Operation) Validate() error {
	if o.AgentID == "" {
		return fmt.Errorf("agent_id is required")
	}
	if o.Kind == OpHello {
		return nil
	}

	if o.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	if o.Seq == 0 {
		return fmt.Errorf("seq is required")
	}

	return nil
}
