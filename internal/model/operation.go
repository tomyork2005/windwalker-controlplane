package model

import (
	"fmt"
)

type OperationKind string

const (
	OpUpsert OperationKind = "upsert"
	OpRemove OperationKind = "remove"
	OpHello  OperationKind = "hello"
)

type Operation struct {
	AgentID   string
	RequestID string
	TaskID    int64
	Kind      OperationKind

	Upsert *AgentUpsertPayload
	Remove *AgentRemovePayload
}

type AgentUpsertPayload struct {
	UserID     string `json:"user_id"`
	DriverType string `json:"driver"`
}

type AgentRemovePayload struct {
	UserID     string `json:"user_id"`
	DriverType string `json:"driver"`
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

	return nil
}
